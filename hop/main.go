// Command hop is a URL shortener that answers from pre-serialised HTTP
// responses over a bare TCP listener. Links come from a text file of
// "key=url" lines.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// The limits exist to bound hostile clients and are not worth configuring.
var limits = Config{
	HeadTimeout:  5 * time.Second,
	IdleTimeout:  10 * time.Second,
	WriteTimeout: 5 * time.Second,
	MaxHeadBytes: 8192,
	MaxConns:     4096,
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	path := flag.String("links", "links.txt", "links file, one key=url per line")
	flag.Parse()
	logger := log.New(os.Stderr, "", 0)

	load := func() (*table, int, error) {
		links, err := LoadLinks(*path)
		if err != nil {
			return nil, 0, err
		}
		return buildTable(links), len(links), nil
	}

	t, count, err := load()
	if err != nil {
		logger.Fatalf("hop: %v", err)
	}
	srv := NewServer(limits, t, logger)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		logger.Fatalf("hop: %v", err)
	}
	logger.Printf("hop: %d links from %s, listening on %s", count, *path, ln.Addr())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for s := range sig {
			if s != syscall.SIGHUP {
				logger.Printf("hop: %s, shutting down", s)
				ln.Close()
				continue
			}
			t, count, err := load()
			if err != nil {
				logger.Printf("hop: reload failed, keeping previous links: %v", err)
				continue
			}
			srv.SetTable(t)
			logger.Printf("hop: reloaded %d links", count)
		}
	}()

	if err := srv.Serve(ln); err != nil {
		logger.Fatalf("hop: %v", err)
	}
}
