package server

import (
	"net"
	"testing"
)

func TestIsPrivate(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"192.168.1.1", true},
		{"192.168.255.255", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"172.16.0.1", false}, // intentionally excluded per spec
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"192.169.1.1", false},
		{"11.0.0.1", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		got := isPrivate(ip)
		if got != c.want {
			t.Errorf("isPrivate(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}
