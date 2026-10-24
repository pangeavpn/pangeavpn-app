//go:build linux

// Package protect serves the unix-socket protocol sing-box's `protect_path`
// dialer option speaks, so transports can hand their outbound sockets to
// Android's VpnService.protect() instead of having the TUN capture them.
package protect

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// Protector reports whether the host managed to protect the socket.
type Protector func(fd int) bool

// Server accepts one fd per connection and offers it to the host.
type Server struct {
	listener *net.UnixListener
	path     string
}

// Listen starts serving at path, replacing any socket a previous run left
// behind. The caller owns Close.
func Listen(path string, protect Protector) (*Server, error) {
	if protect == nil {
		return nil, fmt.Errorf("protect: a protector is required")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("protect: clear stale socket: %w", err)
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("protect: listen %s: %w", path, err)
	}
	srv := &Server{listener: listener, path: path}
	go srv.serve(protect)
	return srv, nil
}

func (s *Server) Path() string { return s.path }

func (s *Server) Close() error {
	err := s.listener.Close()
	_ = os.Remove(s.path)
	return err
}

func (s *Server) serve(protect Protector) {
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			return
		}
		go handle(conn, protect)
	}
}

// handle answers one dialer: read the fd out of the ancillary data, protect
// it, and reply with the single byte the client blocks on.
func handle(conn *net.UnixConn, protect Protector) {
	defer conn.Close()

	buf := make([]byte, 1)
	oob := make([]byte, syscall.CmsgSpace(4))
	_, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
	if err != nil {
		return
	}
	fd, err := firstFileDescriptor(oob[:oobn])
	if err != nil {
		return
	}
	// The fd is a duplicate of the dialer's; the kernel object is shared, so
	// protecting it protects the original, and this copy must not leak.
	defer syscall.Close(fd)

	if !protect(fd) {
		return
	}
	_, _ = conn.Write([]byte{1})
}

func firstFileDescriptor(oob []byte) (int, error) {
	messages, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return 0, err
	}
	for _, message := range messages {
		fds, err := syscall.ParseUnixRights(&message)
		if err != nil || len(fds) == 0 {
			continue
		}
		for _, extra := range fds[1:] {
			_ = syscall.Close(extra)
		}
		return fds[0], nil
	}
	return 0, fmt.Errorf("protect: no file descriptor in ancillary data")
}
