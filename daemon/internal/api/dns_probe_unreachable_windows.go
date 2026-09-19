//go:build windows

package api

import (
	"errors"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isNetworkUnreachable reports a dial or send that found no route out of the host yet.
func isNetworkUnreachable(err error) bool {
	return errors.Is(err, windows.WSAENETUNREACH) || errors.Is(err, windows.WSAEHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH)
}

// isConnRefused reports an ICMP port-unreachable surfaced on the socket. The
// syscall package's ECONNRESET is an invented value here and never matches.
func isConnRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNRESET) || errors.Is(err, windows.WSAECONNREFUSED)
}

// enableUnreachableReports re-arms SIO_UDP_CONNRESET, which Go clears on every
// UDP socket, so an ICMP port-unreachable surfaces as WSAECONNRESET on the next read.
func enableUnreachableReports(conn net.Conn) {
	sc, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) {
		flag := uint32(1)
		var returned uint32
		_ = syscall.WSAIoctl(syscall.Handle(fd), syscall.SIO_UDP_CONNRESET, (*byte)(unsafe.Pointer(&flag)), uint32(unsafe.Sizeof(flag)), nil, 0, &returned, nil, 0)
	})
}
