package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Target is one downstream service that receives a copy of every request.
type Target struct {
	Name string
	URL  *url.URL // base URL; its query params are merged over the incoming ones
}

// Config is read entirely from the environment.
type Config struct {
	Addr     string
	DataDir  string
	Token    string // optional secret path prefix the client must use
	Targets  []Target
	Timeout  time.Duration // per-delivery HTTP timeout
	MaxQueue int           // per-target cap; oldest entries are dropped beyond it
}

func loadConfig() (Config, error) {
	c := Config{
		Addr:     envOr("ADDR", ":8080"),
		DataDir:  envOr("DATA_DIR", "./data"),
		Token:    strings.Trim(os.Getenv("TOKEN"), "/"),
		Timeout:  15 * time.Second,
		MaxQueue: 20000,
	}
	if v := os.Getenv("TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("TIMEOUT: %w", err)
		}
		c.Timeout = d
	}
	if v := os.Getenv("MAX_QUEUE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("MAX_QUEUE must be a positive integer")
		}
		c.MaxQueue = n
	}

	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(k, "TARGET_") || v == "" {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(k, "TARGET_"))
		if name == "" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(v))
		if err != nil {
			return c, fmt.Errorf("%s: %w", k, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
			return c, fmt.Errorf("%s: must be an absolute http(s) URL", k)
		}
		c.Targets = append(c.Targets, Target{Name: name, URL: u})
	}
	sort.Slice(c.Targets, func(i, j int) bool { return c.Targets[i].Name < c.Targets[j].Name })
	if len(c.Targets) == 0 {
		return c, errors.New("no targets configured: set at least one TARGET_<NAME>=<url>")
	}
	return c, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Redacted returns the target URL safe for display: no userinfo, no query values.
func (t Target) Redacted() string {
	u := *t.URL
	u.User = nil
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			q.Set(k, "…")
		}
		u.RawQuery = strings.ReplaceAll(q.Encode(), "%E2%80%A6", "…")
	}
	return u.String()
}
