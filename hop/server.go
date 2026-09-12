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
	// HeadTimeout bounds the time from a request's first byte until its
	// terminating blank line (or, for HTTP/2, until its header block ends).
	// This is the slowloris defence.
	HeadTimeout time.Duration
	// IdleTimeout bounds how long a kept-alive connection may sit with no
	// bytes in flight.
	IdleTimeout time.Duration
	// WriteTimeout bounds each response write, defeating clients that never
	// read what they asked for.
	WriteTimeout time.Duration
	// MaxHeadBytes caps the request head. Anything larger gets a 431.
	MaxHeadBytes int
	// MaxConns caps open connections. Beyond it new connections get a baked
	// 503 and are closed at once.
	MaxConns int
	// SweepInterval is how often timeouts are checked. It is also the
	// resolution of the clock the hot path stamps connections with.
	SweepInterval time.Duration
}

// Connection phases, as the sweeper sees them.
const (
	phaseIdle  = 0 // waiting for the first byte of a request
	phaseHead  = 1 // reading a request head or HTTP/2 header block
	phaseWrite = 2 // writing a response
)

// conn is what the sweeper knows about a live connection. The request path
// does one atomic store on it per phase change and nothing else: no timers,
// no syscalls, no time.Now.
type conn struct {
	c     net.Conn
	stamp atomic.Uint64      // phase<<62 | milliseconds on the server clock
	bye   func(phase uint64) // protocol-specific goodbye before a timeout close
}

func (cn *conn) enter(s *Server, phase uint64) {
	cn.stamp.Store(phase<<62 | uint64(s.clock.Load()))
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

	start time.Time
	clock atomic.Int64 // milliseconds since start, advanced by the sweeper

	mu    sync.Mutex
	conns map[*conn]struct{}
}

// NewServer prepares a server with the given table installed.
func NewServer(cfg Config, t *table, logger *log.Logger) *Server {
	s := &Server{cfg: cfg, log: logger, sem: make(chan struct{}, cfg.MaxConns), start: time.Now(), conns: map[*conn]struct{}{}}
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
	done := make(chan struct{})
	defer close(done)
	go s.sweep(done)

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

// sweep advances the coarse clock and closes connections that have sat in a
// phase too long. One goroutine does this for every connection, so the
// request path never touches a timer.
func (s *Server) sweep(done <-chan struct{}) {
	t := time.NewTicker(s.cfg.SweepInterval)
	defer t.Stop()
	limits := [3]int64{
		phaseIdle:  s.cfg.IdleTimeout.Milliseconds(),
		phaseHead:  s.cfg.HeadTimeout.Milliseconds(),
		phaseWrite: s.cfg.WriteTimeout.Milliseconds(),
	}
	var victims []*conn
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		now := time.Since(s.start).Milliseconds()
		s.clock.Store(now)
		victims = victims[:0]
		s.mu.Lock()
		for cn := range s.conns {
			st := cn.stamp.Load()
			if now-int64(st&(1<<62-1)) > limits[st>>62] {
				victims = append(victims, cn)
			}
		}
		s.mu.Unlock()
		for _, cn := range victims {
			if phase := cn.stamp.Load() >> 62; phase != phaseWrite && cn.bye != nil {
				cn.c.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
				cn.bye(phase)
			}
			cn.c.Close()
		}
	}
}

func (s *Server) track(cn *conn) {
	s.mu.Lock()
	s.conns[cn] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) untrack(cn *conn) {
	s.mu.Lock()
	delete(s.conns, cn)
	s.mu.Unlock()
}

// reject writes a closing response to a connection we will not serve.
func (s *Server) reject(c net.Conn, r *response) {
	c.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	c.Write(r.bytes(false, false))
	c.Close()
}

// handle serves one connection: read a request head, write the baked
// answer, repeat while keep-alive holds. A connection that opens with the
// HTTP/2 preface is handed to serveH2 instead.
func (s *Server) handle(c net.Conn) {
	cn := &conn{c: c}
	cn.bye = func(phase uint64) {
		if phase == phaseHead {
			c.Write(respTimeout.close)
		}
	}
	cn.enter(s, phaseHead) // the client dialled us; it should be talking
	s.track(cn)
	defer func() {
		s.untrack(cn)
		c.Close()
		<-s.sem
	}()

	bp := s.bufs.Get().(*[]byte)
	defer s.bufs.Put(bp)
	buf := *bp

	n := 0        // bytes buffered in buf
	first := true // first request on this connection
	for {
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
				cn.enter(s, phaseWrite)
				c.Write(respTooLarge.close)
				return
			}
			scanFrom = max(n-3, 0)
			m, err := c.Read(buf[n:])
			if m > 0 && n == 0 && !first {
				cn.enter(s, phaseHead) // first byte of the next request
			}
			n += m
			if err != nil {
				return
			}
		}
		if first && n >= len(h2Preface) && bytes.Equal(buf[:len(h2Preface)], h2Preface) {
			s.serveH2(cn, buf, buf[len(h2Preface):n])
			return
		}

		req := parseRequest(buf[:end])
		keep := req.keep
		var resp *response
		switch req.status {
		case 400:
			resp, keep = respBadRequest, false
		case 405:
			resp = respMethod
		default:
			resp = s.table.Load().lookup(req.path)
		}
		cn.enter(s, phaseWrite)
		if _, err := c.Write(resp.bytes(keep, req.head)); err != nil || !keep {
			return
		}

		// Carry over anything read past this head (pipelined requests).
		n = copy(buf, buf[end:n])
		first = false
		if n == 0 {
			cn.enter(s, phaseIdle)
		} else {
			cn.enter(s, phaseHead)
		}
	}
}
