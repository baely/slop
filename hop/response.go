package main

import (
	"strconv"
	"strings"

	"github.com/baely/slop/hop/links"
)

// redirectStatus is used for every link. 302 keeps edits to the links file
// effective immediately; browsers would cache a 301 indefinitely.
const redirectStatus = 302

// header is one response header. Names are canonical case for HTTP/1.1 and
// lowercased for the HTTP/2 block.
type header struct{ name, value string }

// response is a reply serialised once at load time so that serving it costs
// a single write. HTTP/1.1 gets two variants, keep-alive and closing;
// HTTP/2 gets a ready HPACK header block plus the body.
type response struct {
	keep      []byte
	keepHead  int // bytes up to and including the blank line
	close     []byte
	closeHead int

	h2   []byte // HPACK block: :status, headers, content-length
	body []byte
}

// bytes picks the HTTP/1.1 variant to write. HEAD gets only the header block.
func (r *response) bytes(keepAlive, headOnly bool) []byte {
	if keepAlive {
		if headOnly {
			return r.keep[:r.keepHead]
		}
		return r.keep
	}
	if headOnly {
		return r.close[:r.closeHead]
	}
	return r.close
}

var reasons = map[int]string{
	200: "OK",
	302: "Found",
	400: "Bad Request",
	404: "Not Found",
	405: "Method Not Allowed",
	408: "Request Timeout",
	431: "Request Header Fields Too Large",
	503: "Service Unavailable",
}

// bake serialises a response in every form it will ever be sent.
func bake(status int, headers []header, body []byte) *response {
	build := func(closing bool) ([]byte, int) {
		var sb strings.Builder
		sb.Grow(256 + len(body))
		sb.WriteString("HTTP/1.1 ")
		sb.WriteString(strconv.Itoa(status))
		sb.WriteByte(' ')
		sb.WriteString(reasons[status])
		sb.WriteString("\r\n")
		for _, h := range headers {
			sb.WriteString(h.name)
			sb.WriteString(": ")
			sb.WriteString(h.value)
			sb.WriteString("\r\n")
		}
		sb.WriteString("Content-Length: ")
		sb.WriteString(strconv.Itoa(len(body)))
		sb.WriteString("\r\n")
		if closing {
			sb.WriteString("Connection: close\r\n")
		}
		sb.WriteString("\r\n")
		head := sb.Len()
		sb.Write(body)
		return []byte(sb.String()), head
	}
	r := &response{body: body}
	r.keep, r.keepHead = build(false)
	r.close, r.closeHead = build(true)

	r.h2 = hpackStatus(nil, status)
	for _, h := range headers {
		r.h2 = hpackField(r.h2, strings.ToLower(h.name), h.value)
	}
	r.h2 = hpackField(r.h2, "content-length", strconv.Itoa(len(body)))
	return r
}

// HPACK static table entries we use (RFC 7541 appendix A).
var (
	hpackStaticName   = map[string]int{":status": 8, "allow": 22, "content-length": 28, "content-type": 31, "location": 46, "retry-after": 53}
	hpackStaticStatus = map[int]byte{200: 0x88, 204: 0x89, 206: 0x8a, 304: 0x8b, 400: 0x8c, 404: 0x8d, 500: 0x8e}
)

func hpackStatus(dst []byte, status int) []byte {
	if b, ok := hpackStaticStatus[status]; ok {
		return append(dst, b)
	}
	return hpackField(dst, ":status", strconv.Itoa(status))
}

// hpackField appends a literal header field without indexing, so the block
// is valid in any decoder state and never touches the dynamic table.
func hpackField(dst []byte, name, value string) []byte {
	if idx, ok := hpackStaticName[name]; ok {
		dst = hpackInt(dst, 0x00, 4, idx)
	} else {
		dst = append(dst, 0x00)
		dst = hpackString(dst, name)
	}
	return hpackString(dst, value)
}

// hpackString appends a raw (non-Huffman) string literal.
func hpackString(dst []byte, s string) []byte {
	dst = hpackInt(dst, 0x00, 7, len(s))
	return append(dst, s...)
}

// hpackInt appends an integer with an n-bit prefix (RFC 7541 §5.1).
func hpackInt(dst []byte, first byte, n uint, v int) []byte {
	limit := 1<<n - 1
	if v < limit {
		return append(dst, first|byte(v))
	}
	dst = append(dst, first|byte(limit))
	v -= limit
	for v >= 128 {
		dst = append(dst, byte(v%128|0x80))
		v /= 128
	}
	return append(dst, byte(v))
}

// redirect bakes the redirect for one link.
func redirect(target string) *response {
	return bake(redirectStatus, []header{{"Location", target}}, nil)
}

// htmlPage bakes a small house-style HTML page.
func htmlPage(status int, extra []header, heading, detail string) *response {
	headers := append([]header{{"Content-Type", "text/html; charset=utf-8"}}, extra...)
	return bake(status, headers, []byte(renderPage(status, heading, detail)))
}

// Static responses shared by every table.
var (
	respIndex      = htmlPage(200, nil, "hop.", "A short link redirector.")
	respNotFound   = htmlPage(404, nil, "Not found.", "Nothing is registered at this address.")
	respBadRequest = htmlPage(400, nil, "Bad request.", "That did not parse as HTTP.")
	respMethod     = htmlPage(405, []header{{"Allow", "GET, HEAD"}}, "Method not allowed.", "Only GET and HEAD are served here.")
	respTimeout    = htmlPage(408, nil, "Request timeout.", "The request headers did not arrive in time.")
	respTooLarge   = htmlPage(431, nil, "Headers too large.", "The request headers exceeded the limit.")
	respBusy       = htmlPage(503, []header{{"Retry-After", "1"}}, "Busy.", "Too many open connections. Try again shortly.")
)

// table maps request paths to baked responses.
type table struct {
	routes map[string]*response
}

// buildTable bakes one redirect per link. A link for "/" replaces the index
// page at the root.
func buildTable(ls []links.Link) *table {
	routes := make(map[string]*response, len(ls)+1)
	for _, l := range ls {
		routes[l.Path] = redirect(l.URL)
	}
	if _, ok := routes["/"]; !ok {
		routes["/"] = respIndex
	}
	return &table{routes: routes}
}

// lookup finds the response for an exact path.
func (t *table) lookup(path []byte) *response {
	if r, ok := t.routes[string(path)]; ok { // no allocation: compiler-optimised map lookup
		return r
	}
	return respNotFound
}

func (t *table) lookupString(path string) *response {
	if r, ok := t.routes[path]; ok {
		return r
	}
	return respNotFound
}
