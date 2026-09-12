package main

import (
	"bytes"
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Config tunes the listener. Every limit exists to bound what one slow or
// hostile client can hold open.
type Config struct {
	Addr string

	// HeadTimeout bounds the time from a request's first byte until its
	// terminating blank line. This is the slowloris defence: a client that
	// trickles headers is cut off regardless of how many bytes it has sent.
	HeadTimeout time.Duration
	// IdleTimeout bounds how long a keep-alive connection may sit with no
	// bytes in flight before it is closed.
	IdleTimeout time.Duration
	// WriteTimeout bounds each response write, defeating clients that never
	// read what they asked for.
	WriteTimeout time.Duration
	// MaxHeadBytes caps the request line plus headers. Anything larger gets
	// a 431 and is closed.
	MaxHeadBytes int
	// MaxConns caps concurrently open connections. Beyond it new
	// connections are answered with a baked 503 and closed at once.
	MaxConns int
}

// Server is a bare TCP server that answers each HTTP/1.x or HTTP/2 request
// with a pre-serialised response chosen by path.
type Server struct {
	cfg    Config
	table  atomic.Pointer[table]
	sem    chan struct{}
	bufs   sync.Pool // MaxHeadBytes buffers for request heads
	h2bufs sync.Pool // h2MaxFrameSize buffers for frame payloads
	log    *log.Logger
}

// NewServer prepares a server with the given table installed.
func NewServer(cfg Config, t *table, logger *log.Logger) *Server {
	s := &Server{cfg: cfg, log: logger, sem: make(chan struct{}, cfg.MaxConns)}
	s.bufs.New = func() any {
		b := make([]byte, cfg.MaxHeadBytes)
		return &b
	}
	s.h2bufs.New = func() any {
		b := make([]byte, h2MaxFrameSize)
		return &b
	}
	s.table.Store(t)
	return s
}

// SetTable atomically swaps the routing table. In-flight requests finish
// against whichever table they started with.
func (s *Server) SetTable(t *table) { s.table.Store(t) }

// Serve accepts connections until the listener is closed.
func (s *Server) Serve(ln net.Listener) error {
	var backoff time.Duration
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			// Typically EMFILE. Back off rather than spin.
			if backoff == 0 {
				backoff = 5 * time.Millisecond
			} else if backoff *= 2; backoff > time.Second {
				backoff = time.Second
			}
			s.log.Printf("accept: %v (retrying in %v)", err, backoff)
			time.Sleep(backoff)
			continue
		}
		backoff = 0

		select {
		case s.sem <- struct{}{}:
		default:
			go s.reject(c, respBusy)
			continue
		}
		go s.handle(c)
	}
}

// reject writes a closing response to a connection we will not serve.
func (s *Server) reject(c net.Conn, r *response) {
	c.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	c.Write(r.bytes(false, false))
	c.Close()
}

// handle serves one connection: read a request head within the deadline,
// write the baked answer, repeat while keep-alive holds. A connection that
// opens with the HTTP/2 preface is handed to serveH2 instead.
func (s *Server) handle(c net.Conn) {
	defer func() {
		c.Close()
		<-s.sem
	}()

	bp := s.bufs.Get().(*[]byte)
	defer s.bufs.Put(bp)
	buf := *bp

	n := 0        // bytes buffered in buf
	first := true // first request on this connection
	for {
		// A fresh connection gets the head timeout straight away: the client
		// dialled us, it should be talking. A kept-alive connection may idle,
		// but the moment a byte lands the head timeout takes over.
		if n == 0 && !first {
			c.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		} else {
			c.SetReadDeadline(time.Now().Add(s.cfg.HeadTimeout))
		}

		end := -1
		scanFrom := 0
		for {
			end = findHeadEnd(buf[:n], scanFrom)
			// "PRI * HTTP/2.0\r\n\r\n" looks like a complete head but may be
			// the first 18 bytes of the 24-byte HTTP/2 preface: keep reading.
			if end >= 0 && !(first && n < len(h2Preface) && bytes.Equal(buf[:end], h2PrefaceLine)) {
				break
			}
			if n == len(buf) {
				s.reply(c, respTooLarge, false, false)
				return
			}
			scanFrom = max(n-3, 0)
			m, err := c.Read(buf[n:])
			if m > 0 && n == 0 && !first {
				c.SetReadDeadline(time.Now().Add(s.cfg.HeadTimeout))
			}
			n += m
			if err != nil {
				if isTimeout(err) && n > 0 {
					s.reply(c, respTimeout, false, false)
				}
				return
			}
		}
		if first && n >= len(h2Preface) && bytes.Equal(buf[:len(h2Preface)], h2Preface) {
			s.serveH2(c, buf, buf[len(h2Preface):n])
			return
		}

		req := parseRequest(buf[:end])
		keep := req.keep && !req.hasBody
		var resp *response
		switch req.status {
		case 400:
			resp, keep = respBadRequest, false
		case 405:
			resp = respMethod
		default:
			resp = s.table.Load().lookup(req.path)
		}
		if !s.reply(c, resp, keep, req.head) || !keep {
			return
		}

		// Carry over anything read past this head (pipelined requests).
		n = copy(buf, buf[end:n])
		first = false
	}
}

// reply writes one baked response under the write deadline.
func (s *Server) reply(c net.Conn, r *response, keep, headOnly bool) bool {
	c.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	_, err := c.Write(r.bytes(keep, headOnly))
	return err == nil
}

// findHeadEnd returns the index just past the blank line that ends a request
// head ("\r\n\r\n", or a bare "\n\n" for hand-typed clients), scanning from
// index from. It returns -1 if the head is not yet complete.
func findHeadEnd(b []byte, from int) int {
	for i := max(from, 1); i < len(b); i++ {
		if b[i] != '\n' {
			continue
		}
		if b[i-1] == '\n' {
			return i + 1
		}
		if i >= 3 && b[i-1] == '\r' && b[i-2] == '\n' && b[i-3] == '\r' {
			return i + 1
		}
	}
	return -1
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
