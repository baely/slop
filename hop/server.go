package main

import (
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
	// MaxConnsPerIP caps connections per remote IP. Zero disables it, which
	// is what you want behind a reverse proxy where every connection shares
	// the proxy's address.
	MaxConnsPerIP int
	// RedirectStatus is the 3xx code used for every link.
	RedirectStatus int
}

// Server is a bare TCP HTTP/1.x server that answers each request with a
// pre-serialised response chosen by path.
type Server struct {
	cfg   Config
	table atomic.Pointer[table]
	sem   chan struct{}
	bufs  sync.Pool
	log   *log.Logger

	perIP struct {
		sync.Mutex
		m map[string]int
	}
}

// NewServer prepares a server with the given table installed.
func NewServer(cfg Config, t *table, logger *log.Logger) *Server {
	s := &Server{cfg: cfg, log: logger, sem: make(chan struct{}, cfg.MaxConns)}
	s.bufs.New = func() any {
		b := make([]byte, cfg.MaxHeadBytes)
		return &b
	}
	s.perIP.m = map[string]int{}
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
		ip := ""
		if s.cfg.MaxConnsPerIP > 0 {
			ip = remoteIP(c)
			if !s.acquireIP(ip) {
				<-s.sem
				go s.reject(c, respBusy)
				continue
			}
		}
		go s.handle(c, ip)
	}
}

// reject writes a closing response to a connection we will not serve.
func (s *Server) reject(c net.Conn, r *response) {
	c.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	c.Write(r.bytes(false, false))
	c.Close()
}

func remoteIP(c net.Conn) string {
	if a, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		return a.IP.String()
	}
	return c.RemoteAddr().String()
}

func (s *Server) acquireIP(ip string) bool {
	s.perIP.Lock()
	defer s.perIP.Unlock()
	if s.perIP.m[ip] >= s.cfg.MaxConnsPerIP {
		return false
	}
	s.perIP.m[ip]++
	return true
}

func (s *Server) releaseIP(ip string) {
	s.perIP.Lock()
	if s.perIP.m[ip] <= 1 {
		delete(s.perIP.m, ip)
	} else {
		s.perIP.m[ip]--
	}
	s.perIP.Unlock()
}

// handle serves one connection: read a request head within the deadline,
// write the baked answer, repeat while keep-alive holds.
func (s *Server) handle(c net.Conn, ip string) {
	defer func() {
		c.Close()
		if ip != "" {
			s.releaseIP(ip)
		}
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
			if end = findHeadEnd(buf[:n], scanFrom); end >= 0 {
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
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() && n > 0 {
					s.reply(c, respTimeout, false, false)
				}
				return
			}
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
