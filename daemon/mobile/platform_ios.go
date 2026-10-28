//go:build ios

package mobile

import (
	"os"
	"runtime/debug"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	platformName         = "iOS"
	anytlsClientMetadata = "pangeavpn-ios"

	// The packet tunnel extension is jetsammed at 50 MiB resident.
	iosMemoryLimit = 32 << 20
)

func init() { debug.SetMemoryLimit(iosMemoryLimit) }

// The extension keeps owning the original fd, so Go works on a dup.
func adoptTunFD(fd int) (int, error) {
	dup, err := unix.Dup(fd)
	if err != nil {
		return -1, err
	}
	if err := unix.SetNonblock(dup, true); err != nil {
		_ = unix.Close(dup)
		return -1, err
	}
	return dup, nil
}

func openTUN(fd int) (tun.Device, error) {
	return tun.CreateTUNFromFile(os.NewFile(uintptr(fd), "/dev/tun"), 0)
}

// Extension sockets bypass the tunnel, so nothing needs protecting.
func singBoxProtected() bool { return true }

func startProtectServer(string, *state.LogStore) error { return nil }
