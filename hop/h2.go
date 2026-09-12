package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/net/http2/hpack"
)

// The HTTP/2 client connection preface (RFC 9113 §3.4). Traefik terminates
// TLS and pipes the plaintext through, so a browser that negotiated h2 via
// ALPN shows up here with this instead of a request line.
var (
	h2Preface     = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	h2PrefaceLine = h2Preface[:18] // where findHeadEnd stops
)

const (
	h2Data         = 0x0
	h2Headers      = 0x1
	h2RSTStream    = 0x3
	h2Settings     = 0x4
	h2PushPromise  = 0x5
	h2Ping         = 0x6
	h2GoAway       = 0x7
	h2WindowUpdate = 0x8
	h2Continuation = 0x9

	h2FlagEndStream  = 0x1
	h2FlagAck        = 0x1
	h2FlagEndHeaders = 0x4
	h2FlagPadded     = 0x8
	h2FlagPriority   = 0x20

	h2ErrNo          = 0x0
	h2ErrProtocol    = 0x1
	h2ErrFlowControl = 0x3
	h2ErrFrameSize   = 0x6
	h2ErrCompression = 0x9
	h2ErrCalm        = 0xb

	h2SettingInitialWindowSize = 0x4
	h2SettingMaxFrameSize      = 0x5

	h2MaxFrameSize  = 16384 // the protocol default, which we never raise
	h2InitialWindow = 65535
	h2MaxWindow     = 1<<31 - 1
)

// h2Conn serves one HTTP/2 connection. Frames are handled in order and each
// request is answered the moment its header block is complete, so the only
// per-stream state is a response waiting on flow-control credit.
type h2Conn struct {
	s   *Server
	c   net.Conn
	br  *bufio.Reader
	buf []byte // frame payload scratch, h2MaxFrameSize long
	dec *hpack.Decoder

	lastStream   uint32
	sendWindow   int64 // connection-level credit for DATA
	streamWindow int64 // client's SETTINGS_INITIAL_WINDOW_SIZE
	pending      []h2Pending

	// Header block under assembly (HEADERS then CONTINUATIONs).
	block         []byte
	blockOpen     bool
	blockStream   uint32
	blockEnd      bool // END_STREAM was on the HEADERS frame
	blockOver     bool // exceeded MaxHeadBytes
	blockDeadline time.Time

	// Captured while the decoder emits the current block.
	method, path string
	badHeaders   bool

	hdr, hdr2 [9]byte
	small     [8]byte
}

type h2Pending struct {
	stream uint32
	r      *response
}

// serveH2 takes over a connection whose client preface has been consumed.
// block is the connection's 8 KiB head buffer, reused for header blocks;
// leftover is whatever arrived after the preface and may alias it.
func (s *Server) serveH2(c net.Conn, block, leftover []byte) {
	rest := bytes.Clone(leftover)
	bp := s.h2bufs.Get().(*[]byte)
	defer s.h2bufs.Put(bp)
	h := &h2Conn{
		s: s, c: c, buf: *bp, block: block[:0],
		sendWindow: h2InitialWindow, streamWindow: h2InitialWindow,
	}
	h.br = bufio.NewReaderSize(io.MultiReader(bytes.NewReader(rest), c), 4096)
	h.dec = hpack.NewDecoder(4096, h.onHeader)

	// Server preface: a SETTINGS frame; empty means all defaults.
	if !h.writeFrame(h2Settings, 0, 0, nil) {
		return
	}
	for h.readFrame() {
	}
}

// readFrame reads and handles one frame. It returns false when the
// connection is finished; any GOAWAY has already been written.
func (h *h2Conn) readFrame() bool {
	if h.blockOpen {
		// Mid header block: the whole block shares one deadline, so trickled
		// CONTINUATION frames cannot stretch it.
		h.c.SetReadDeadline(h.blockDeadline)
	} else {
		h.c.SetReadDeadline(time.Now().Add(h.s.cfg.IdleTimeout))
		if _, err := h.br.Peek(1); err != nil {
			if isTimeout(err) {
				h.goAway(h2ErrNo)
			}
			return false
		}
		h.c.SetReadDeadline(time.Now().Add(h.s.cfg.HeadTimeout))
	}
	if _, err := io.ReadFull(h.br, h.hdr[:]); err != nil {
		return false
	}
	length := int(h.hdr[0])<<16 | int(h.hdr[1])<<8 | int(h.hdr[2])
	typ, flags := h.hdr[3], h.hdr[4]
	stream := binary.BigEndian.Uint32(h.hdr[5:]) & 0x7fffffff
	if length > len(h.buf) {
		return h.goAway(h2ErrFrameSize)
	}
	payload := h.buf[:length]
	if _, err := io.ReadFull(h.br, payload); err != nil {
		return false
	}
	return h.handle(typ, flags, stream, payload)
}

func (h *h2Conn) handle(typ, flags byte, stream uint32, p []byte) bool {
	switch typ {
	case h2Headers:
		if h.blockOpen || stream == 0 || stream%2 == 0 {
			return h.goAway(h2ErrProtocol)
		}
		frag, ok := h2StripHeaders(p, flags)
		if !ok {
			return h.goAway(h2ErrProtocol)
		}
		h.blockOpen, h.blockStream = true, stream
		h.blockEnd = flags&h2FlagEndStream != 0
		h.blockOver = false
		h.block = h.block[:0]
		h.blockDeadline = time.Now().Add(h.s.cfg.HeadTimeout)
		h.appendBlock(frag)
		if flags&h2FlagEndHeaders != 0 {
			return h.endHeaders()
		}
	case h2Continuation:
		if !h.blockOpen || stream != h.blockStream {
			return h.goAway(h2ErrProtocol)
		}
		h.appendBlock(p)
		if flags&h2FlagEndHeaders != 0 {
			return h.endHeaders()
		}
	case h2Settings:
		if stream != 0 {
			return h.goAway(h2ErrProtocol)
		}
		if flags&h2FlagAck != 0 {
			return true
		}
		if len(p)%6 != 0 {
			return h.goAway(h2ErrFrameSize)
		}
		for ; len(p) > 0; p = p[6:] {
			id, v := binary.BigEndian.Uint16(p), binary.BigEndian.Uint32(p[2:])
			switch id {
			case h2SettingInitialWindowSize:
				if v > h2MaxWindow {
					return h.goAway(h2ErrFlowControl)
				}
				h.streamWindow = int64(v)
			case h2SettingMaxFrameSize:
				if v < h2MaxFrameSize || v > 1<<24-1 {
					return h.goAway(h2ErrProtocol)
				}
			}
		}
		return h.writeFrame(h2Settings, h2FlagAck, 0, nil)
	case h2Ping:
		if stream != 0 {
			return h.goAway(h2ErrProtocol)
		}
		if len(p) != 8 {
			return h.goAway(h2ErrFrameSize)
		}
		if flags&h2FlagAck == 0 {
			return h.writeFrame(h2Ping, h2FlagAck, 0, p)
		}
	case h2WindowUpdate:
		if len(p) != 4 {
			return h.goAway(h2ErrFrameSize)
		}
		if stream != 0 {
			return true // per-stream credit is not tracked; see writeResponse
		}
		inc := int64(binary.BigEndian.Uint32(p) & 0x7fffffff)
		if inc == 0 {
			return h.goAway(h2ErrProtocol)
		}
		if h.sendWindow += inc; h.sendWindow > h2MaxWindow {
			return h.goAway(h2ErrFlowControl)
		}
		return h.flushPending()
	case h2RSTStream:
		for i, pnd := range h.pending {
			if pnd.stream == stream {
				h.pending = append(h.pending[:i], h.pending[i+1:]...)
				break
			}
		}
	case h2GoAway:
		return false
	case h2PushPromise:
		return h.goAway(h2ErrProtocol)
		// DATA is a request body we never read: GET and HEAD have none, and
		// anything else was already answered 405. Since we never grant more
		// window, a client can send at most 64 KiB of it per connection.
		// PRIORITY and unknown frame types are ignored as the RFC requires.
	}
	return true
}

// h2StripHeaders removes the padding and priority fields from a HEADERS
// payload, leaving the header block fragment.
func h2StripHeaders(p []byte, flags byte) ([]byte, bool) {
	if flags&h2FlagPadded != 0 {
		if len(p) < 1 {
			return nil, false
		}
		pad := int(p[0])
		p = p[1:]
		if pad > len(p) {
			return nil, false
		}
		p = p[:len(p)-pad]
	}
	if flags&h2FlagPriority != 0 {
		if len(p) < 5 {
			return nil, false
		}
		p = p[5:]
	}
	return p, true
}

func (h *h2Conn) appendBlock(frag []byte) {
	if h.blockOver || len(h.block)+len(frag) > cap(h.block) {
		h.blockOver = true
		return
	}
	h.block = append(h.block, frag...)
}

// endHeaders decodes the finished header block and answers the stream.
func (h *h2Conn) endHeaders() bool {
	stream, endStream, over := h.blockStream, h.blockEnd, h.blockOver
	h.blockOpen = false
	if over {
		// The block was never decoded, so the HPACK state is lost. Answer,
		// then end the connection, as HTTP/1.1 does after a 431.
		h.writeResponse(stream, respTooLarge, false)
		return h.goAway(h2ErrNo)
	}
	h.method, h.path, h.badHeaders = "", "", false
	if _, err := h.dec.Write(h.block); err != nil {
		return h.goAway(h2ErrCompression)
	}
	if err := h.dec.Close(); err != nil {
		return h.goAway(h2ErrCompression)
	}
	if stream <= h.lastStream {
		return true // trailers or a reset stream: decoded to stay in sync, nothing to answer
	}
	h.lastStream = stream

	var resp *response
	switch {
	case h.badHeaders || h.method == "" || h.path == "" || h.path[0] != '/':
		resp = respBadRequest
	case h.method != "GET" && h.method != "HEAD":
		resp = respMethod
	default:
		p := h.path
		if i := strings.IndexByte(p, '?'); i >= 0 {
			p = p[:i]
		}
		resp = h.s.table.Load().lookupString(p)
	}
	ok, sent := h.writeResponse(stream, resp, h.method == "HEAD")
	if ok && sent && !endStream {
		// A body is on its way that we will not read. RST_STREAM(NO_ERROR)
		// after a complete response is the sanctioned way to say so.
		binary.BigEndian.PutUint32(h.small[:4], h2ErrNo)
		return h.writeFrame(h2RSTStream, 0, stream, h.small[:4])
	}
	return ok
}

// writeResponse answers a stream: one HEADERS frame, plus one DATA frame if
// there is a body to send. Bodies spend connection-level flow-control
// credit; a response that does not fit waits for a WINDOW_UPDATE.
func (h *h2Conn) writeResponse(stream uint32, r *response, head bool) (ok, sent bool) {
	body := r.body
	if head {
		body = nil
	}
	if len(body) > 0 {
		if int64(len(body)) > h.streamWindow {
			// A client window too small for a 2 KiB page is not worth serving.
			binary.BigEndian.PutUint32(h.small[:4], h2ErrCalm)
			return h.writeFrame(h2RSTStream, 0, stream, h.small[:4]), true
		}
		if int64(len(body)) > h.sendWindow {
			h.pending = append(h.pending, h2Pending{stream, r})
			return true, false
		}
		h.sendWindow -= int64(len(body))
	}
	flags := byte(h2FlagEndHeaders)
	if len(body) == 0 {
		flags |= h2FlagEndStream
	}
	h2FrameHeader(h.hdr[:], h2Headers, flags, stream, len(r.h2))
	bufs := net.Buffers{h.hdr[:], r.h2}
	if len(body) > 0 {
		h2FrameHeader(h.hdr2[:], h2Data, h2FlagEndStream, stream, len(body))
		bufs = append(bufs, h.hdr2[:], body)
	}
	return h.write(bufs), true
}

func (h *h2Conn) flushPending() bool {
	for len(h.pending) > 0 {
		p := h.pending[0]
		if int64(len(p.r.body)) > h.sendWindow {
			return true
		}
		h.pending = h.pending[1:]
		if ok, _ := h.writeResponse(p.stream, p.r, false); !ok {
			return false
		}
	}
	return true
}

func (h *h2Conn) onHeader(f hpack.HeaderField) {
	switch f.Name {
	case ":method":
		if h.method != "" {
			h.badHeaders = true
		}
		h.method = f.Value
	case ":path":
		if h.path != "" {
			h.badHeaders = true
		}
		h.path = f.Value
	case ":scheme", ":authority":
	default:
		if len(f.Name) > 0 && f.Name[0] == ':' {
			h.badHeaders = true
		}
	}
}

func h2FrameHeader(dst []byte, typ, flags byte, stream uint32, length int) {
	dst[0], dst[1], dst[2] = byte(length>>16), byte(length>>8), byte(length)
	dst[3], dst[4] = typ, flags
	binary.BigEndian.PutUint32(dst[5:], stream)
}

func (h *h2Conn) writeFrame(typ, flags byte, stream uint32, payload []byte) bool {
	h2FrameHeader(h.hdr[:], typ, flags, stream, len(payload))
	return h.write(net.Buffers{h.hdr[:], payload})
}

func (h *h2Conn) write(b net.Buffers) bool {
	h.c.SetWriteDeadline(time.Now().Add(h.s.cfg.WriteTimeout))
	_, err := b.WriteTo(h.c)
	return err == nil
}

// goAway tells the client why the connection is ending. It always returns
// false so callers can `return h.goAway(code)`.
func (h *h2Conn) goAway(code uint32) bool {
	binary.BigEndian.PutUint32(h.small[:4], h.lastStream)
	binary.BigEndian.PutUint32(h.small[4:], code)
	h.writeFrame(h2GoAway, 0, 0, h.small[:8])
	return false
}
