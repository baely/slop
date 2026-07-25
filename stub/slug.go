package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

// slugAlphabet deliberately omits characters that are easy to confuse when a
// short link is read aloud, copied off a screen or typed from a photo:
// 0/O, 1/l/I. What is left is 31 symbols, lowercase only.
const slugAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"

// defaultSlugLen gives 31^7 ~= 2.7e10 possibilities. Slugs are NOT
// capabilities: the redirect they guard is public by design and everything
// interesting (stats, edit, delete) sits behind APP_TOKEN. Session ids, which
// *are* capabilities, use randomID with far more entropy.
const defaultSlugLen = 7

const maxSlugLen = 64

// reservedSlugs are first path segments the app owns. A link may never claim
// one, otherwise a shortened link could shadow the UI or the API.
var reservedSlugs = map[string]bool{
	"":                 true,
	"admin":            true,
	"api":              true,
	"static":           true,
	"healthz":          true,
	"health":           true,
	"login":            true,
	"logout":           true,
	"favicon.ico":      true,
	"robots.txt":       true,
	"sitemap.xml":      true,
	".well-known":      true,
	"index":            true,
	"new":              true,
	"stats":            true,
	"link":             true,
	"links":            true,
	"metrics":          true,
	"debug":            true,
	"apple-touch-icon": true,
}

// randomID returns n characters drawn uniformly from slugAlphabet using
// crypto/rand with rejection sampling (no modulo bias).
func randomID(n int) (string, error) {
	const limit = 248 // 31 * 8, the largest multiple of 31 below 256
	out := make([]byte, 0, n)
	buf := make([]byte, n+8)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out = append(out, slugAlphabet[int(b)%len(slugAlphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

// validSlugChar reports whether c may appear in a custom slug.
func validSlugChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_':
		return true
	}
	return false
}

// normalizeSlug validates a user-supplied slug and returns it unchanged (case
// is preserved; reservation and collision checks are case-insensitive so
// "Admin" cannot sneak past "admin").
func normalizeSlug(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "/")
	if s == "" {
		return "", errors.New("Slug is empty.")
	}
	if len(s) > maxSlugLen {
		return "", fmt.Errorf("Slug is too long — %d characters, the limit is %d.", len(s), maxSlugLen)
	}
	for i := 0; i < len(s); i++ {
		if !validSlugChar(s[i]) {
			return "", fmt.Errorf("Slug contains %q — only letters, digits, hyphen and underscore are allowed.", string(s[i]))
		}
	}
	if reservedSlugs[strings.ToLower(s)] {
		return "", fmt.Errorf("Slug %q is reserved by the app and cannot be used for a link.", s)
	}
	return s, nil
}

// slugKey is the case-folded key used for collision detection and lookup.
func slugKey(s string) string { return strings.ToLower(s) }
