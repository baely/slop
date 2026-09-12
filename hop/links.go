package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Link is one registered short link.
type Link struct {
	Path string // request path that serves it, e.g. "/linkedin"; "/" is the root
	URL  string
}

// LoadLinks reads a links file. Each non-blank, non-comment line is
// "key=url"; the first '=' separates them so the URL may contain '='.
func LoadLinks(path string) ([]Link, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseLinks(f)
}

// ParseLinks parses "key=url" lines. Blank lines and lines starting with '#'
// are ignored. A key is matched byte-for-byte against the request path after
// the leading slash, so "linkedin" serves exactly "/linkedin". Duplicates are
// errors.
func ParseLinks(r io.Reader) ([]Link, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	seen := map[string]int{}
	var links []Link
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key=url", n)
		}
		path, err := keyPath(strings.TrimSpace(k))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		target := strings.TrimSpace(v)
		if err := validateTarget(target); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if first, dup := seen[path]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q (first defined on line %d)", n, path, first)
		}
		seen[path] = n
		links = append(links, Link{Path: path, URL: target})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

// keyPath turns a file key into the request path it serves: "/" plus the key
// exactly as written. An empty key is the root. Bytes that cannot appear in a
// request path, and a leading slash (which would double up), are rejected.
func keyPath(k string) (string, error) {
	if strings.HasPrefix(k, "/") {
		return "", fmt.Errorf("key %q: write it without the leading slash", k)
	}
	for i := 0; i < len(k); i++ {
		if c := k[i]; c <= ' ' || c == 0x7f || c == '?' || c == '#' {
			return "", fmt.Errorf("key %q: character %q can never match a request path", k, c)
		}
	}
	return "/" + k, nil
}

// validateTarget accepts absolute URIs only, and rejects anything that could
// not travel safely inside a Location header (whitespace, control bytes,
// non-ASCII).
func validateTarget(t string) error {
	if t == "" {
		return fmt.Errorf("empty url")
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c <= ' ' || c >= 0x7f {
			return fmt.Errorf("url %q contains byte %q; percent-encode it", t, c)
		}
	}
	colon := strings.IndexByte(t, ':')
	if colon <= 0 {
		return fmt.Errorf("url %q must be absolute (e.g. https://...)", t)
	}
	scheme := t[:colon]
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		alpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if !(alpha || (i > 0 && (digit || c == '+' || c == '-' || c == '.'))) {
			return fmt.Errorf("url %q must be absolute (e.g. https://...)", t)
		}
	}
	if colon == len(t)-1 {
		return fmt.Errorf("url %q has nothing after the scheme", t)
	}
	return nil
}
