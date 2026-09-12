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
	Key string // normalised path with no surrounding slashes; "" is the root
	URL string
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
// are ignored. Keys are normalised with NormaliseKey; duplicates are errors.
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
		key, err := NormaliseKey(strings.TrimSpace(k))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		target := strings.TrimSpace(v)
		if err := validateTarget(target); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q (first defined on line %d)", n, displayKey(key), first)
		}
		seen[key] = n
		links = append(links, Link{Key: key, URL: target})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

// NormaliseKey lowercases a key and strips surrounding slashes so that
// "LinkedIn", "/linkedin" and "linkedin/" all address the same link. "/" (or
// an empty key) addresses the root path.
func NormaliseKey(k string) (string, error) {
	k = strings.Trim(k, "/")
	if strings.Contains(k, "//") {
		return "", fmt.Errorf("key %q contains an empty path segment", k)
	}
	b := []byte(k)
	for i, c := range b {
		switch {
		case c >= 'A' && c <= 'Z':
			b[i] = c + ('a' - 'A')
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == '~', c == '/', c == '+', c == '@', c == ':':
		default:
			return "", fmt.Errorf("key %q: character %q is not allowed (use letters, digits, - _ . ~ / + @ :)", k, c)
		}
	}
	return string(b), nil
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

func displayKey(k string) string {
	if k == "" {
		return "/"
	}
	return k
}
