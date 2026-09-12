package main

import "syscall"

// listenControl asks the kernel to hold a new connection until its first
// bytes arrive (TCP_DEFER_ACCEPT), so accept returns a connection whose
// first read succeeds at once and a client that connects and says nothing
// never costs a goroutine.
func listenControl(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_DEFER_ACCEPT, int(limits.HeadTimeout.Seconds()))
	})
}
