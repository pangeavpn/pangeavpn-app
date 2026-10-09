//go:build linux

package protect

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/sagernet/sing/common/control"
)

func TestServerProtectsDialedSocket(t *testing.T) {
	var got []int
	path := filepath.Join(t.TempDir(), "protect.sock")

	srv, err := Listen(path, func(fd int) bool {
		got = append(got, fd)
		return true
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	defer syscall.Close(fd)

	if err := sendFor(srv.Path(), fd); err != nil {
		t.Fatalf("ProtectPath control: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("protector calls = %d, want 1", len(got))
	}
}

func TestServerReportsRefusalToClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "protect.sock")
	srv, err := Listen(path, func(int) bool { return false })
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	defer syscall.Close(fd)

	if err := sendFor(srv.Path(), fd); err == nil {
		t.Fatal("expected an error when the host refuses to protect the fd")
	}
}

// sendFor drives sing's own ProtectPath client against the server, so the test
// fails if the wire protocol ever drifts from the one the dialers speak.
func sendFor(path string, fd int) error {
	return control.ProtectPath(path)("udp", "", rawFD(fd))
}

type rawFD int

func (r rawFD) Control(f func(fd uintptr)) error { f(uintptr(r)); return nil }
func (r rawFD) Read(func(uintptr) bool) error    { return nil }
func (r rawFD) Write(func(uintptr) bool) error   { return nil }
