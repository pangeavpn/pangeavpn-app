//go:build android

package mobile

import "golang.zx2c4.com/wireguard/tun"

const (
	platformName         = "Android"
	anytlsClientMetadata = "pangeavpn-android"
)

func adoptTunFD(fd int) (int, error) { return fd, nil }

func openTUN(fd int) (tun.Device, error) {
	dev, _, err := tun.CreateUnmonitoredTUNFromFD(fd)
	return dev, err
}

func singBoxProtected() bool { return protectServer != nil }
