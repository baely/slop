package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// h2Client speaks HTTP/2 over plain TCP (prior knowledge), which is what
// hop sees after Traefik strips TLS.
func h2Client(t *testing.T) *http.Client {
	tr := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func TestH2Redirect(t *testing.T) {
	_, addr := start(t, testConfig())
	c := h2Client(t)
	for _, path := range []string{"/linkedin", "/linkedin?utm_source=x&y=1"} {
		resp, err := c.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.ProtoMajor != 2 {
			t.Fatalf("%s: proto %s", path, resp.Proto)
		}
		if resp.StatusCode != 302 || resp.Header.Get("Location") != "https://linkedin.com/in/baileybutler1" || len(body) != 0 {
			t.Errorf("%s: %d %q body=%d", path, resp.StatusCode, resp.Header.Get("Location"), len(body))
		}
	}
}

func TestH2Pages(t *testing.T) {
	_, addr := start(t, testConfig())
	c := h2Client(t)
	check := func(method, path string, status int, contains string, wantLen int64) {
		t.Helper()
		req, _ := http.NewRequest(method, "http://"+addr+path, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != status || !strings.Contains(string(body), contains) {
			t.Errorf("%s %s: %d, body missing %q", method, path, resp.StatusCode, contains)
		}
		if resp.Header.Get("Content-Type") != "text/html; charset=utf-8" || resp.ContentLength != wantLen {
			t.Errorf("%s %s: content-type %q length %d", method, path, resp.Header.Get("Content-Type"), resp.ContentLength)
		}
	}
	check("GET", "/", 200, "<h1>hop.</h1>", int64(len(respIndex.body)))
	check("GET", "/nope", 404, "HTTP 404", int64(len(respNotFound.body)))
	check("HEAD", "/nope", 404, "", int64(len(respNotFound.body))) // headers only, but the length is advertised
}

func TestH2MethodNotAllowed(t *testing.T) {
	_, addr := start(t, testConfig())
	c := h2Client(t)
	resp, err := c.Post("http://"+addr+"/linkedin", "text/plain", strings.NewReader("abc"))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("got %d allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	// The connection must still be usable afterwards.
	resp, err = c.Get("http://" + addr + "/linkedin")
	if err != nil || resp.StatusCode != 302 {
		t.Fatalf("after POST: %v %v", err, resp)
	}
	resp.Body.Close()
}

func TestH2ManyStreamsOneConnection(t *testing.T) {
	s, addr := start(t, testConfig())
	c := h2Client(t)
	var wg sync.WaitGroup
	errs := make(chan error, 500)
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				resp, err := c.Get("http://" + addr + "/docs/api")
				if err != nil {
					errs <- err
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 302 {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n := len(s.sem); n != 1 {
		t.Fatalf("%d connections open, want 1 multiplexed", n)
	}
}

func TestH2FlowControlPendingBody(t *testing.T) {
	// Exhaust the 64 KiB connection window with 404 pages, then confirm the
	// stalled responses are released by a WINDOW_UPDATE. The Go transport
	// does exactly this, so a burst of pages larger than the window must
	// all complete.
	_, addr := start(t, testConfig())
	c := h2Client(t)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ { // 64 * ~2 KiB > 65535
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := c.Get("http://" + addr + "/missing")
			if err != nil {
				t.Error(err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 404 || len(body) != len(respNotFound.body) {
				t.Errorf("got %d, %d bytes", resp.StatusCode, len(body))
			}
		}()
	}
	wg.Wait()
}

func TestH2HeaderBlockDecodes(t *testing.T) {
	var got []hpack.HeaderField
	dec := hpack.NewDecoder(4096, func(f hpack.HeaderField) { got = append(got, f) })
	r := bake(302, []header{{"Location", "https://example.com/" + strings.Repeat("x", 200)}}, nil)
	if _, err := dec.Write(r.h2); err != nil {
		t.Fatal(err)
	}
	want := []hpack.HeaderField{
		{Name: ":status", Value: "302"},
		{Name: "location", Value: "https://example.com/" + strings.Repeat("x", 200)},
		{Name: "content-length", Value: "0"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Value != want[i].Value {
			t.Errorf("field %d = %v, want %v", i, got[i], want[i])
		}
	}
	got = nil
	dec.Write(respMethod.h2)
	if got[0].Value != "405" || got[1].Name != "content-type" || got[2].Name != "allow" || got[2].Value != "GET, HEAD" {
		t.Errorf("405 block: %v", got)
	}
}

// Raw-frame helpers for the misbehaving-client tests.

func h2Frame(typ, flags byte, stream uint32, payload []byte) []byte {
	b := make([]byte, 9+len(payload))
	h2FrameHeader(b, typ, flags, stream, len(payload))
	copy(b[9:], payload)
	return b
}

func readH2Frame(r io.Reader) (typ, flags byte, stream uint32, payload []byte, err error) {
	var hdr [9]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	n := int(hdr[0])<<16 | int(hdr[1])<<8 | int(hdr[2])
	payload = make([]byte, n)
	_, err = io.ReadFull(r, payload)
	return hdr[3], hdr[4], binary.BigEndian.Uint32(hdr[5:]) & 0x7fffffff, payload, err
}

func TestH2SlowlorisPartialFrame(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	started := time.Now()
	c.Write(h2Preface)
	c.Write([]byte{0, 0, 30, 1}) // start of a HEADERS frame header, never finished
	io.ReadAll(c)
	if el := time.Since(started); el < cfg.HeadTimeout || el > cfg.HeadTimeout*4 {
		t.Fatalf("closed after %v, want about %v", el, cfg.HeadTimeout)
	}
}

func TestH2TrickledContinuationSharesDeadline(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	c.Write(h2Preface)
	c.Write(h2Frame(h2Settings, 0, 0, nil))
	started := time.Now()
	// HEADERS without END_HEADERS, then a CONTINUATION every 100ms forever.
	c.Write(h2Frame(h2Headers, 0, 1, []byte{0x82}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, err := c.Write(h2Frame(h2Continuation, 0, 1, []byte{0x86})); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	io.ReadAll(c)
	el := time.Since(started)
	<-done
	if el < cfg.HeadTimeout || el > cfg.HeadTimeout*4 {
		t.Fatalf("closed after %v, want about %v", el, cfg.HeadTimeout)
	}
}

func TestH2IdleGoAway(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	c.Write(h2Preface)
	c.Write(h2Frame(h2Settings, 0, 0, nil))
	started := time.Now()
	var types []byte
	for {
		typ, _, _, payload, err := readH2Frame(c)
		if err != nil {
			break
		}
		types = append(types, typ)
		if typ == h2GoAway && binary.BigEndian.Uint32(payload[4:]) != h2ErrNo {
			t.Fatalf("GOAWAY code %d", binary.BigEndian.Uint32(payload[4:]))
		}
	}
	if !bytes.Equal(types, []byte{h2Settings, h2Settings, h2GoAway}) {
		t.Fatalf("frames: %v", types)
	}
	if el := time.Since(started); el < cfg.IdleTimeout || el > cfg.IdleTimeout*3 {
		t.Fatalf("closed after %v, want about %v", el, cfg.IdleTimeout)
	}
}

func TestH2HeadersTooLarge(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	c.Write(h2Preface)
	c.Write(h2Frame(h2Settings, 0, 0, nil))
	c.Write(h2Frame(h2Headers, h2FlagEndHeaders|h2FlagEndStream, 1, bytes.Repeat([]byte{0x82}, cfg.MaxHeadBytes+1)))
	var status string
	dec := hpack.NewDecoder(4096, func(f hpack.HeaderField) {
		if f.Name == ":status" {
			status = f.Value
		}
	})
	sawGoAway := false
	for {
		typ, _, stream, payload, err := readH2Frame(c)
		if err != nil {
			break
		}
		if typ == h2Headers && stream == 1 {
			dec.Write(payload)
		}
		if typ == h2GoAway {
			sawGoAway = true
		}
	}
	if status != "431" || !sawGoAway {
		t.Fatalf("status %q goaway=%v", status, sawGoAway)
	}
}

func TestH2PingAndUnknownFrames(t *testing.T) {
	_, addr := start(t, testConfig())
	c := dial(t, addr)
	c.Write(h2Preface)
	c.Write(h2Frame(h2Settings, 0, 0, nil))
	c.Write(h2Frame(0x42, 0, 0, []byte("whatever"))) // unknown type: ignored
	c.Write(h2Frame(h2Ping, 0, 0, []byte("12345678")))
	for {
		typ, flags, _, payload, err := readH2Frame(c)
		if err != nil {
			t.Fatal(err)
		}
		if typ == h2Ping {
			if flags&h2FlagAck == 0 || string(payload) != "12345678" {
				t.Fatalf("bad ping ack: flags=%x %q", flags, payload)
			}
			return
		}
	}
}

func TestH2PrefaceSplitAcrossReads(t *testing.T) {
	_, addr := start(t, testConfig())
	c := dial(t, addr)
	c.Write(h2Preface[:18]) // exactly the part that looks like an HTTP/1 head
	time.Sleep(50 * time.Millisecond)
	c.Write(h2Preface[18:])
	c.Write(h2Frame(h2Settings, 0, 0, nil))
	typ, _, _, _, err := readH2Frame(c)
	if err != nil || typ != h2Settings {
		t.Fatalf("expected server SETTINGS, got type %d err %v", typ, err)
	}
}
