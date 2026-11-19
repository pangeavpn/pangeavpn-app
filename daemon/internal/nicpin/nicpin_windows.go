//go:build windows

package nicpin

import "net/netip"

// acquire binds by IP_UNICAST_IF, which confines the route lookup to the NIC's own
// routes; no route is added, so no other process can follow one.
func acquire(_ []netip.Addr) (*Pin, error) {
	name, index, _, err := physicalInterface()
	if err != nil {
		return nil, err
	}
	return &Pin{Interface: name, index: index}, nil
}
