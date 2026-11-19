//go:build !windows && !(linux && !android) && !(darwin && !ios)

package nicpin

import "net/netip"

func acquire(_ []netip.Addr) (*Pin, error) {
	return nil, ErrUnsupported
}
