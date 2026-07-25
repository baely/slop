package main

import (
	"fmt"
	"net/url"
	"strings"
)

const maxTargetLen = 2048

// validateTarget is the single gate between "a link shortener" and "an open
// redirect that launders hostile URLs through a trusted domain". Only absolute
// http and https URLs with a host survive. Every rejection names the actual
// problem so the caller does not have to guess.
func validateTarget(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("Target is required.")
	}
	if len(s) > maxTargetLen {
		return "", fmt.Errorf("Target is too long — %d characters, the limit is %d.", len(s), maxTargetLen)
	}
	// Strip characters that never belong in a URL but are routinely used to
	// smuggle a scheme past naive parsers ("java\tscript:").
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("Target contains a control character. Only printable URL characters are accepted.")
		}
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("Target is not a valid URL.")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		if strings.HasPrefix(s, "//") {
			return "", fmt.Errorf("Target %q is scheme-relative. Write it out in full, starting with http:// or https://.", s)
		}
		return "", fmt.Errorf("Target %q has no scheme. Only absolute http:// and https:// URLs are accepted.", s)
	}
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("Scheme %q is not allowed. Only http and https targets are accepted — %s: URLs are refused because a short link that runs code or embeds a payload is an attack, not a link.", scheme, scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("Target %q has no host. An http/https target needs a hostname, e.g. https://example.com/path.", s)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("Target %q has a port but no hostname.", s)
	}
	if strings.ContainsAny(u.Host, " \t") {
		return "", fmt.Errorf("Target host %q contains whitespace.", u.Host)
	}
	return u.String(), nil
}

// referrerOrigin reduces a Referer header to scheme://host so the stats table
// does not turn into a list of one-off deep links (and does not retain query
// strings, which routinely carry tokens).
func referrerOrigin(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return directReferrer
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return unknownReferrer
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return unknownReferrer
	}
	return scheme + "://" + strings.ToLower(u.Host)
}

const (
	directReferrer  = "(direct)"
	unknownReferrer = "(unknown)"
	otherReferrer   = "(other)"
)
