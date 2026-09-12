// Package links reads and validates hop's links file: one "key=url" per
// line, matched byte-for-byte against the request path after the leading
// slash. hop loads it; hop-writer edits it. Both go through this package so
// nothing hop-writer accepts can fail to load in hop.
package links

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

// Key is the file key for a path: "/linkedin" is written as "linkedin".
func (l Link) Key() string { return strings.TrimPrefix(l.Path, "/") }

// Load reads and parses the file at path.
func Load(path string) ([]Link, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads "key=url" lines. Blank lines and lines starting with '#' are
// ignored. Duplicate paths are errors.
func Parse(r io.Reader) ([]Link, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	seen := map[string]int{}
	var links []Link
	for n := 1; sc.Scan(); n++ {
		l, ok, err := ParseLine(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if !ok {
			continue
		}
		if first, dup := seen[l.Path]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q (first defined on line %d)", n, l.Key(), first)
		}
		seen[l.Path] = n
		links = append(links, l)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

// ParseLine classifies one line of the file. ok is false for blank and
// comment lines, which carry no link.
func ParseLine(line string) (l Link, ok bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return Link{}, false, nil
	}
	k, v, found := strings.Cut(line, "=")
	if !found {
		return Link{}, false, fmt.Errorf("expected key=url")
	}
	path, err := KeyPath(strings.TrimSpace(k))
	if err != nil {
		return Link{}, false, err
	}
	target := strings.TrimSpace(v)
	if err := ValidateTarget(target); err != nil {
		return Link{}, false, err
	}
	return Link{Path: path, URL: target}, true, nil
}

// KeyPath turns a file key into the request path it serves: "/" plus the key
// exactly as written. An empty key is the root. Bytes that cannot appear in
// a request path, and a leading slash (which would double up), are rejected.
func KeyPath(k string) (string, error) {
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

// ValidateTarget accepts absolute URIs only, and rejects anything that could
// not travel safely inside a Location header (whitespace, control bytes,
// non-ASCII).
func ValidateTarget(t string) error {
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
