package main

import (
	"strconv"
	"strings"
)

// response is a fully serialised HTTP/1.1 reply, baked once at load time so
// that serving it is a single write. Two variants exist: one for keep-alive
// connections and one carrying "Connection: close".
type response struct {
	keep      []byte
	keepHead  int // bytes up to and including the blank line
	close     []byte
	closeHead int
}

// bytes picks the variant to write. HEAD requests get only the header block.
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
	301: "Moved Permanently",
	302: "Found",
	307: "Temporary Redirect",
	308: "Permanent Redirect",
	400: "Bad Request",
	404: "Not Found",
	405: "Method Not Allowed",
	408: "Request Timeout",
	431: "Request Header Fields Too Large",
	503: "Service Unavailable",
}

// bake serialises a response. headers are "Name: value" lines without CRLF.
func bake(status int, headers []string, body []byte) *response {
	build := func(closing bool) ([]byte, int) {
		var sb strings.Builder
		sb.Grow(256 + len(body))
		sb.WriteString("HTTP/1.1 ")
		sb.WriteString(strconv.Itoa(status))
		sb.WriteByte(' ')
		sb.WriteString(reasons[status])
		sb.WriteString("\r\n")
		for _, h := range headers {
			sb.WriteString(h)
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
	r := &response{}
	r.keep, r.keepHead = build(false)
	r.close, r.closeHead = build(true)
	return r
}

// redirect bakes a redirect to target with the given 3xx status.
func redirect(status int, target string) *response {
	return bake(status, []string{"Location: " + target}, nil)
}

// htmlPage bakes a small house-style HTML page.
func htmlPage(status int, extra []string, heading, detail string) *response {
	headers := append([]string{"Content-Type: text/html; charset=utf-8"}, extra...)
	return bake(status, headers, []byte(renderPage(status, heading, detail)))
}

// Static responses shared by every table.
var (
	respIndex      = htmlPage(200, nil, "hop.", "A short link redirector.")
	respNotFound   = htmlPage(404, nil, "Not found.", "Nothing is registered at this address.")
	respBadRequest = htmlPage(400, nil, "Bad request.", "That did not parse as HTTP/1.x.")
	respMethod     = htmlPage(405, []string{"Allow: GET, HEAD"}, "Method not allowed.", "Only GET and HEAD are served here.")
	respTimeout    = htmlPage(408, nil, "Request timeout.", "The request headers did not arrive in time.")
	respTooLarge   = htmlPage(431, nil, "Headers too large.", "The request headers exceeded the limit.")
	respBusy       = htmlPage(503, []string{"Retry-After: 1"}, "Busy.", "Too many open connections. Try again shortly.")
)

// table maps normalised paths ("" for root) to baked responses.
type table struct {
	routes map[string]*response
}

// buildTable bakes one redirect per link. A link keyed "/" replaces the
// index page at the root path.
func buildTable(links []Link, status int) *table {
	routes := make(map[string]*response, len(links)+1)
	for _, l := range links {
		routes[l.Key] = redirect(status, l.URL)
	}
	if _, ok := routes[""]; !ok {
		routes[""] = respIndex
	}
	return &table{routes: routes}
}

// lookup finds the response for a normalised path.
func (t *table) lookup(path []byte) *response {
	if r, ok := t.routes[string(path)]; ok { // no allocation: compiler-optimised map lookup
		return r
	}
	return respNotFound
}
