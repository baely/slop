package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.txt")
	os.WriteFile(path, []byte("a=https://a.example\n"), 0o644)
	changes := make(chan struct{}, 8)
	go watchFile(path, 10*time.Millisecond, func() { changes <- struct{}{} })

	time.Sleep(50 * time.Millisecond)
	select {
	case <-changes:
		t.Fatal("change reported before anything changed")
	default:
	}

	// In-place edit with a different size.
	os.WriteFile(path, []byte("a=https://a.example\nb=https://b.example\n"), 0o644)
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("in-place edit not noticed")
	}

	// Atomic replace (what hop-writer does): same size, new inode, new mtime.
	time.Sleep(20 * time.Millisecond)
	tmp := path + ".tmp"
	os.WriteFile(tmp, []byte("a=https://a.example\nc=https://c.example\n"), 0o644)
	os.Rename(tmp, path)
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("atomic replace not noticed")
	}
}
