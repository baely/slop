package main

import "testing"

func TestParseRequest(t *testing.T) {
	type want struct {
		status  int
		head    bool
		path    string
		keep    bool
		hasBody bool
	}
	cases := []struct {
		name string
		raw  string
		want want
	}{
		{"plain", "GET /linkedin HTTP/1.1\r\nHost: h\r\n\r\n", want{path: "/linkedin", keep: true}},
		{"query stripped", "GET /linkedin?utm_source=x&y=1 HTTP/1.1\r\nHost: h\r\n\r\n", want{path: "/linkedin", keep: true}},
		{"exact bytes, no normalisation", "GET //LinkedIn/ HTTP/1.1\r\n\r\n", want{path: "//LinkedIn/", keep: true}},
		{"root", "GET / HTTP/1.1\r\n\r\n", want{path: "/", keep: true}},
		{"root with query", "GET /?x=1 HTTP/1.1\r\n\r\n", want{path: "/", keep: true}},
		{"nested", "GET /docs/api HTTP/1.1\r\n\r\n", want{path: "/docs/api", keep: true}},
		{"head", "HEAD /x HTTP/1.1\r\n\r\n", want{path: "/x", head: true, keep: true}},
		{"absolute form rejected", "GET http://hop.example/linkedin HTTP/1.1\r\n\r\n", want{status: 400}},
		{"connection close", "GET /x HTTP/1.1\r\nConnection: Close\r\n\r\n", want{path: "/x"}},
		{"connection list", "GET /x HTTP/1.1\r\nconnection: keep-alive, close\r\n\r\n", want{path: "/x"}},
		{"http/1.0 default close", "GET /x HTTP/1.0\r\n\r\n", want{path: "/x"}},
		{"http/1.0 keep-alive still closes", "GET /x HTTP/1.0\r\nConnection: Keep-Alive\r\n\r\n", want{path: "/x"}},
		{"bare lf", "GET /x HTTP/1.1\nHost: h\n\n", want{path: "/x", keep: true}},
		{"body forces close", "GET /x HTTP/1.1\r\nContent-Length: 5\r\n\r\n", want{path: "/x", keep: true, hasBody: true}},
		{"zero body ok", "GET /x HTTP/1.1\r\nContent-Length: 0\r\n\r\n", want{path: "/x", keep: true}},
		{"chunked forces close", "GET /x HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n", want{path: "/x", keep: true, hasBody: true}},
		{"post is 405", "POST /x HTTP/1.1\r\nContent-Length: 2\r\n\r\n", want{status: 405, path: "/x", keep: true, hasBody: true}},
		{"options is 405", "OPTIONS /x HTTP/1.1\r\n\r\n", want{status: 405, path: "/x", keep: true}},
		{"http/0.9", "GET /x\r\n\r\n", want{status: 400}},
		{"garbage", "\x16\x03\x01\x02\x00\x01\x00\x01\xfc\r\n\r\n", want{status: 400}},
		{"http/2 preface", "PRI * HTTP/2.0\r\n\r\n", want{status: 400}},
		{"asterisk", "OPTIONS * HTTP/1.1\r\n\r\n", want{status: 400}},
		{"authority form", "GET hop.example:80 HTTP/1.1\r\n\r\n", want{status: 400}},
	}
	for _, c := range cases {
		got := parseRequest([]byte(c.raw))
		g := want{got.status, got.head, string(got.path), got.keep, got.hasBody}
		if got.status == 400 {
			g.path, g.keep, g.hasBody, g.head = "", false, false, false
		}
		if g != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, g, c.want)
		}
	}
}

func TestFindHeadEnd(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"GET / HTTP/1.1\r\n\r\n", 18},
		{"GET / HTTP/1.1\n\n", 16},
		{"GET / HTTP/1.1\r\nA: b\r\n\r\nGET /", 24},
		{"GET / HTTP/1.1\r\nA: b\r\n", -1},
		{"", -1},
		{"\r\n", -1},
	}
	for _, c := range cases {
		if got := findHeadEnd([]byte(c.in), 0); got != c.want {
			t.Errorf("findHeadEnd(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	// Resuming a scan across a split terminator must still find it.
	b := []byte("GET / HTTP/1.1\r\n\r\n")
	for split := 1; split < len(b); split++ {
		if got := findHeadEnd(b, max(split-3, 0)); got != len(b) {
			t.Errorf("split at %d: got %d", split, got)
		}
	}
}

var benchReq = []byte("GET /linkedin?utm_source=share&utm_medium=member_desktop HTTP/1.1\r\nHost: hop.baileys.dev\r\nUser-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36\r\nAccept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\nAccept-Language: en-AU,en;q=0.9\r\nAccept-Encoding: gzip, deflate, br\r\nConnection: keep-alive\r\nUpgrade-Insecure-Requests: 1\r\n\r\n")

func BenchmarkParseAndLookup(b *testing.B) {
	tbl := buildTable([]Link{{"/linkedin", "https://linkedin.com/in/baileybutler1"}})
	buf := make([]byte, len(benchReq))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		copy(buf, benchReq)
		end := findHeadEnd(buf, 0)
		r := parseRequest(buf[:end])
		if tbl.lookup(r.path) == respNotFound {
			b.Fatal("miss")
		}
	}
}
