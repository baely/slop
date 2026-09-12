//go:build !linux

package main

import "syscall"

func listenControl(network, address string, c syscall.RawConn) error { return nil }
