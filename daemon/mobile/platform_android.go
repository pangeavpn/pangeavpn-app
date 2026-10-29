//go:build android

package mobile

import (
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun"
)

const (
	platformName         = "Android"
	anytlsClientMetadata = "pangeavpn-android"
)

func adoptTunFD(fd int) (int, error) { return fd, nil }

// openTUN owns the fd from here on, so it closes it when the device fails,
// matching iOS where CreateTUNFromFile closes on every error.
func openTUN(fd int) (tun.Device, error) {
	dev, _, err := tun.CreateUnmonitoredTUNFromFD(fd)
	if err != nil {
		_ = unix.Close(fd)
	}
	return dev, err
}

func singBoxProtected() bool { return protectServer != nil }
