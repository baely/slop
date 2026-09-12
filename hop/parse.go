package main

import "bytes"

var crlfcrlf = []byte("\r\n\r\n")

// request is all we take from a request head: method, path, version.
// Headers are never read. Keep-alive follows the HTTP version, and a request
// that carries a body is not something a redirector serves anyway.
type request struct {
	status int    // 0 when routable, else 400 or 405
	head   bool   // HEAD method: send headers only
	path   []byte // request target with the query cut off, exactly as sent
	keep   bool   // HTTP/1.1
}

// parseRequest parses the request line of a complete head. It never
// allocates: the path is a view into b.
func parseRequest(b []byte) request {
	var r request
	line, _ := cutLine(b)
	method, line := cutSpace(line)
	target, version := cutSpace(line)
	if len(version) != 8 || string(version[:7]) != "HTTP/1." || len(target) == 0 || target[0] != '/' {
		r.status = 400
		return r
	}
	// HTTP/1.0 connections always close: honouring 1.0 keep-alive would mean
	// advertising it in every reply.
	r.keep = version[7] != '0'
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
	return r
}

// findHeadEnd returns the index just past the CRLFCRLF that ends a request
// head, scanning from index from, or -1 if the head is not yet complete.
func findHeadEnd(b []byte, from int) int {
	if i := bytes.Index(b[from:], crlfcrlf); i >= 0 {
		return from + i + 4
	}
	return -1
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
