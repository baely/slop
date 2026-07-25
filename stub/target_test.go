package main

import (
	"strings"
	"testing"
)

func TestValidateTargetAccepts(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com/a/b?c=d#e", "http://example.com/a/b?c=d#e"},
		{"  https://example.com/x  ", "https://example.com/x"},
		{"HTTPS://Example.com/Path", "https://Example.com/Path"},
		{"https://example.com:8443/x", "https://example.com:8443/x"},
		{"https://user@example.com/x", "https://user@example.com/x"},
		{"http://localhost:3000/dev", "http://localhost:3000/dev"},
		{"https://xn--80ak6aa92e.com/", "https://xn--80ak6aa92e.com/"},
	}
	for _, tc := range tests {
		got, err := validateTarget(tc.in)
		if err != nil {
			t.Errorf("validateTarget(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("validateTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// This is the table that matters. A shortener that accepts any of these turns
// a trusted domain into a delivery vehicle.
func TestValidateTargetRejects(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantSub string
	}{
		{"javascript", "javascript:alert(1)", `Scheme "javascript" is not allowed`},
		{"javascript uppercase", "JavaScript:alert(1)", `Scheme "javascript" is not allowed`},
		{"javascript with spaces", "  javascript:alert(document.domain)  ", `Scheme "javascript" is not allowed`},
		{"data", "data:text/html;base64,PHNjcmlwdD4=", `Scheme "data" is not allowed`},
		{"data plain", "data:text/html,<script>alert(1)</script>", `Scheme "data" is not allowed`},
		{"file", "file:///etc/passwd", `Scheme "file" is not allowed`},
		{"file uppercase", "FILE:///etc/passwd", `Scheme "file" is not allowed`},
		{"vbscript", "vbscript:msgbox(1)", `Scheme "vbscript" is not allowed`},
		{"mailto", "mailto:someone@example.com", `Scheme "mailto" is not allowed`},
		{"ftp", "ftp://example.com/x", `Scheme "ftp" is not allowed`},
		{"intent", "intent://scan/#Intent;scheme=zxing;end", `Scheme "intent" is not allowed`},
		{"blob", "blob:https://example.com/uuid", `Scheme "blob" is not allowed`},
		{"no scheme", "example.com/path", "has no scheme"},
		{"scheme relative", "//evil.example.com/x", "scheme-relative"},
		{"root relative", "/admin", "has no scheme"},
		{"http no host", "http:///path", "has no host"},
		{"https no host", "https://", "has no host"},
		{"empty", "", "Target is required"},
		{"only spaces", "   ", "Target is required"},
		{"tab smuggling", "java\tscript:alert(1)", "control character"},
		{"newline smuggling", "java\nscript:alert(1)", "control character"},
		{"null byte", "https://example.com/\x00", "control character"},
		{"too long", "https://example.com/" + strings.Repeat("a", maxTargetLen), "too long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateTarget(tc.in)
			if err == nil {
				t.Fatalf("validateTarget(%q) = %q, want rejection", tc.in, got)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("validateTarget(%q) error = %q, want it to name the problem (%q)", tc.in, err, tc.wantSub)
			}
		})
	}
}

func TestReferrerOrigin(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", directReferrer},
		{"   ", directReferrer},
		{"https://news.ycombinator.com/item?id=1&token=secret", "https://news.ycombinator.com"},
		{"http://Example.COM/a/b", "http://example.com"},
		{"https://t.co/abc", "https://t.co"},
		{"android-app://com.example", unknownReferrer},
		{"not a url at all", unknownReferrer},
		{"/relative", unknownReferrer},
	}
	for _, tc := range tests {
		if got := referrerOrigin(tc.in); got != tc.want {
			t.Errorf("referrerOrigin(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReferrerOriginDropsQueryStrings(t *testing.T) {
	got := referrerOrigin("https://mail.example.com/inbox?auth=abc123token")
	if strings.Contains(got, "abc123token") {
		t.Fatalf("referrerOrigin retained a query string: %q", got)
	}
}
