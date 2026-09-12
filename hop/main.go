// Command hop is a URL shortener that answers from pre-serialised HTTP
// responses over a bare TCP listener. Links come from a text file of
// "key=url" lines.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/baely/slop/hop/links"
)

// The limits exist to bound hostile clients and are not worth configuring.
var limits = Config{
	HeadTimeout:   5 * time.Second,
	IdleTimeout:   10 * time.Second,
	WriteTimeout:  5 * time.Second,
	MaxHeadBytes:  8192,
	MaxConns:      4096,
	SweepInterval: 250 * time.Millisecond,
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	path := flag.String("links", "links.txt", "links file, one key=url per line")
	flag.Parse()
	logger := log.New(os.Stderr, "", 0)

	load := func() (*table, int, error) {
		ls, err := links.Load(*path)
		if err != nil {
			return nil, 0, err
		}
		return buildTable(ls), len(ls), nil
	}

	t, count, err := load()
	if err != nil {
		logger.Fatalf("hop: %v", err)
	}
	srv := NewServer(limits, t, logger)

	// KeepAlive -1: no per-connection keepalive setsockopts; the sweeper's
	// idle timeout already bounds dead connections.
	lc := net.ListenConfig{KeepAlive: -1, Control: listenControl}
	ln, err := lc.Listen(context.Background(), "tcp", *addr)
	if err != nil {
		logger.Fatalf("hop: %v", err)
	}
	logger.Printf("hop: %d links from %s, listening on %s", count, *path, ln.Addr())

	reload := func(why string) {
		t, count, err := load()
		if err != nil {
			logger.Printf("hop: %s: reload failed, keeping previous links: %v", why, err)
			return
		}
		srv.SetTable(t)
		logger.Printf("hop: %s: reloaded %d links", why, count)
	}
	// Edits to the file (hop-writer, or vi) take effect within two seconds.
	go watchFile(*path, 2*time.Second, func() { reload("links file changed") })

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for s := range sig {
			if s == syscall.SIGHUP {
				reload("SIGHUP")
				continue
			}
			logger.Printf("hop: %s, shutting down", s)
			ln.Close()
		}
	}()

	if err := srv.Serve(ln); err != nil {
		logger.Fatalf("hop: %v", err)
	}
}
