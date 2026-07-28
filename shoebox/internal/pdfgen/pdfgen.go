// Package pdfgen renders HTML to PDF via headless chromium. If no chromium
// binary is found the feature quietly disables itself (local dev).
package pdfgen

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	once sync.Once
	bin  string
)

func binary() string {
	once.Do(func() {
		if p := os.Getenv("CHROMIUM_PATH"); p != "" {
			bin = p
			return
		}
		for _, c := range []string{"chromium", "chromium-browser", "google-chrome", "chrome"} {
			if p, err := exec.LookPath(c); err == nil {
				bin = p
				return
			}
		}
		log.Printf("pdfgen: no chromium binary found, email to pdf disabled")
	})
	return bin
}

func Available() bool { return binary() != "" }

// HTMLToPDF prints an HTML document to PDF. Each call gets its own profile
// dir so concurrent conversions don't fight over chromium's singleton lock.
func HTMLToPDF(html []byte) ([]byte, error) {
	if binary() == "" {
		return nil, errors.New("no chromium binary")
	}
	dir, err := os.MkdirTemp("", "shoebox-pdf-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in.html")
	out := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(in, html, 0o600); err != nil {
		return nil, err
	}

	cmd := exec.Command(binary(),
		"--headless", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
		"--disable-extensions", "--mute-audio", "--no-first-run",
		"--user-data-dir="+filepath.Join(dir, "profile"),
		"--crash-dumps-dir="+filepath.Join(dir, "crash"),
		"--virtual-time-budget=4000",
		"--print-to-pdf-no-header",
		"--print-to-pdf="+out,
		"file://"+in)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Chromium sometimes finishes the PDF but never exits (seen on macOS).
	// Trust the file, not the exit: poll for a stable size ending in %%EOF,
	// then reap the process.
	finished := func() []byte {
		data, err := os.ReadFile(out)
		if err != nil || len(data) == 0 {
			return nil
		}
		tail := data
		if len(tail) > 32 {
			tail = tail[len(tail)-32:]
		}
		if !bytes.Contains(tail, []byte("%%EOF")) {
			return nil
		}
		return data
	}

	deadline := time.After(45 * time.Second)
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	var lastSize int64 = -1
	stable := 0
	for {
		select {
		case err := <-done:
			if data := finished(); data != nil {
				return data, nil
			}
			return nil, fmt.Errorf("chromium: %v: %s", err, bytes.TrimSpace(buf.Bytes()))
		case <-deadline:
			cmd.Process.Kill()
			<-done
			if data := finished(); data != nil {
				return data, nil
			}
			return nil, fmt.Errorf("chromium: timed out: %s", bytes.TrimSpace(buf.Bytes()))
		case <-tick.C:
			fi, err := os.Stat(out)
			if err != nil || fi.Size() == 0 {
				continue
			}
			if fi.Size() == lastSize {
				stable++
			} else {
				lastSize, stable = fi.Size(), 0
			}
			if stable >= 2 {
				if data := finished(); data != nil {
					cmd.Process.Kill()
					<-done
					return data, nil
				}
			}
		}
	}
}
