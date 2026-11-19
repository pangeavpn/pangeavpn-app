//go:build linux && !android

package nicpin

import (
	"errors"
	"net/netip"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

// strictReversePath is replaced by tests; production reads the kernel's rp_filter.
var strictReversePath = platform.StrictReversePathFilter

var errStrictReversePath = errors.New("strict reverse-path filtering (rp_filter=1) drops the replies to a pinned socket, so the edge can't be reached without a route every app would follow")

// acquire marks the socket so policy routing skips the tunnel table; the kill switch
// permits the shared addresses only for sockets carrying this mark.
func acquire(_ []netip.Addr) (*Pin, error) {
	if strict, err := strictReversePath(); err == nil && strict {
		return nil, errStrictReversePath
	}
	return &Pin{Mark: egress.SplitMark}, nil
}
