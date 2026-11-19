//go:build linux && !android

package nicpin

import (
	"net/netip"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

const pinMark = egress.SplitMark

// strictReversePath is replaced by tests; production reads the kernel's rp_filter.
var strictReversePath = platform.StrictReversePathFilter

// acquire marks the socket so policy routing skips the tunnel table; the kill switch
// permits the shared addresses only for sockets carrying this mark.
func acquire(_ []netip.Addr) (*Pin, error) {
	pin := &Pin{Mark: pinMark}
	if strict, err := strictReversePath(); err == nil && strict {
		release, err := rp.acquire()
		if err != nil {
			return nil, err
		}
		pin.release = release
	}
	return pin, nil
}
