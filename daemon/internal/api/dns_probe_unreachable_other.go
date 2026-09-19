//go:build !windows

package api

import (
	"errors"
	"net"
	"syscall"
)

// isNetworkUnreachable reports a dial or send that found no route out of the host yet.
func isNetworkUnreachable(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH)
}

// isConnRefused reports an ICMP port-unreachable surfaced on the socket.
func isConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED)
}

// enableUnreachableReports is Windows-only; Unix sockets report ICMP errors already.
func enableUnreachableReports(net.Conn) {}
