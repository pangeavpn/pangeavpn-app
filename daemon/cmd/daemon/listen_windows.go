package main

import (
	"errors"
	"syscall"
)

// Winsock reports a taken port as WSAEADDRINUSE, which syscall does not name.
const wsaEAddrInUse = syscall.Errno(10048)

func isAddrInUse(err error) bool {
	return errors.Is(err, wsaEAddrInUse) || errors.Is(err, syscall.EADDRINUSE)
}
