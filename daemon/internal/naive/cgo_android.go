//go:build android && arm64 && naive_cgo

// comment-length-check: ignore-file — the block below is a cgo preamble,
// i.e. C source that Go requires to be written as a comment.

package naive

// #cgo CFLAGS: -I${SRCDIR}/android/arm64-v8a
// #cgo LDFLAGS: -L${SRCDIR}/android/arm64-v8a -lpangea_naive -llog -lm -ldl -latomic
//
// #include "pangea_naive_capi.h"
//
// extern int pangeaNaiveProtectFD(int fd);
//
// static void pangeaNaiveInstallProtector(void) {
//   PangeaNaiveSetSocketProtector(pangeaNaiveProtectFD);
// }
import "C"

// installSocketProtector routes every socket the engine opens through
// ProtectFD. Without it naive dials its server through our own TUN.
func installSocketProtector() {
	C.pangeaNaiveInstallProtector()
}
