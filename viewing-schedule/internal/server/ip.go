package server

import (
	"net"
	"net/http"
	"strings"
)

// privateNets are the CIDR ranges considered "private" for admin access.
// Per spec: 10.0.0.0/8 and 192.168.0.0/16. Loopback is also allowed for local dev.
var privateNets = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// clientIP extracts the client IP, optionally trusting X-Forwarded-For.
func clientIP(r *http.Request, trustProxy bool) net.IP {
	if trustProxy {
		if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
			// First IP in the list is the original client.
			parts := strings.Split(xf, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[0])); ip != nil {
				return ip
			}
		}
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			if ip := net.ParseIP(strings.TrimSpace(xr)); ip != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// isPrivate reports whether the given IP is in an allowed private range.
func isPrivate(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range privateNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// requirePrivateIP wraps a handler so it returns 403 unless the request originates
// from a private IP range (10.0.0.0/8, 192.168.0.0/16) or loopback.
func (s *Server) requirePrivateIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, s.opts.TrustProxy)
		if !isPrivate(ip) {
			http.Error(w, "Admin panel is restricted to the local network.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
