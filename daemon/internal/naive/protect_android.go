//go:build android && arm64 && naive_cgo

package naive

import "C"

// ProtectFD is set by the mobile layer to VpnService.protect. Unset, every
// socket the engine opens is captured by our own TUN, so Start refuses.
var ProtectFD func(fd int) bool

//export pangeaNaiveProtectFD
func pangeaNaiveProtectFD(fd C.int) C.int {
	protect := ProtectFD
	if protect == nil || !protect(int(fd)) {
		return 0
	}
	return 1
}
