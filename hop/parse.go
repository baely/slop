package main

import "bytes"

// request is the little we need from a request head: enough to pick a
// response and decide whether the connection can be reused.
type request struct {
	status  int    // 0 when routable, else 400 or 405
	head    bool   // HEAD method: send headers only
	path    []byte // request target with the query cut off, exactly as sent
	keep    bool   // client allows connection reuse
	hasBody bool   // request declares a body we will not read; forces close
}

// parseRequest parses a complete request head (including its final blank
// line). It never allocates: the path is a view into b.
func parseRequest(b []byte) request {
	var r request

	line, rest := cutLine(b)
	method, line := cutSpace(line)
	target, version := cutSpace(line)
	if version == nil || len(version) < 8 || string(version[:7]) != "HTTP/1." || version[7] < '0' || version[7] > '9' {
		r.status = 400
		return r
	}
	if len(target) == 0 || target[0] != '/' {
		r.status = 400
		return r
	}
	// HTTP/1.0 connections always close. Supporting 1.0 keep-alive would
	// mean advertising "Connection: keep-alive" in every reply, and it is
	// not worth a third baked variant for clients that old.
	r.keep = !(len(version) == 8 && version[7] == '0')

	switch string(method) {
	case "GET":
	case "HEAD":
		r.head = true
	default:
		r.status = 405
	}

	if i := bytes.IndexByte(target, '?'); i >= 0 {
		target = target[:i]
	}
	r.path = target

	// Headers: only the connection-management ones matter to us.
	for len(rest) > 0 {
		var h []byte
		h, rest = cutLine(rest)
		if len(h) == 0 {
			break
		}
		colon := bytes.IndexByte(h, ':')
		if colon < 0 {
			continue
		}
		name, value := h[:colon], bytes.TrimSpace(h[colon+1:])
		switch {
		case equalFold(name, "connection"):
			if containsFold(value, "close") {
				r.keep = false
			}
		case equalFold(name, "content-length"):
			if !(len(value) == 1 && value[0] == '0') {
				r.hasBody = true
			}
		case equalFold(name, "transfer-encoding"), equalFold(name, "expect"):
			r.hasBody = true
		}
	}
	return r
}

// cutLine splits at the first LF, dropping the line terminator (LF or CRLF).
func cutLine(b []byte) (line, rest []byte) {
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		return b, nil
	}
	line, rest = b[:i], b[i+1:]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, rest
}

// cutSpace splits at the first space. rest is nil when there is none.
func cutSpace(b []byte) (tok, rest []byte) {
	i := bytes.IndexByte(b, ' ')
	if i < 0 {
		return b, nil
	}
	return b[:i], b[i+1:]
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// equalFold reports whether b equals the lowercase ASCII string s, ignoring
// ASCII case in b.
func equalFold(b []byte, s string) bool {
	if len(b) != len(s) {
		return false
	}
	for i := range b {
		if lower(b[i]) != s[i] {
			return false
		}
	}
	return true
}

// containsFold reports whether lowercase ASCII s occurs in b, ignoring case.
func containsFold(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if equalFold(b[i:i+len(s)], s) {
			return true
		}
	}
	return false
}
