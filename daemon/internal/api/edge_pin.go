package api

import (
	"context"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/nicpin"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/sbrelay"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/tectonic"
)

// acquireEdgePin is replaced by tests; production pins through the OS.
var acquireEdgePin = nicpin.Acquire

// pinCDNEdge pins the CDN transport's edge sockets to the physical NIC: a shared edge
// gets no bypass route (see sharedEdgeHosts), so an unpinned dial would loop into the tunnel.
func pinCDNEdge(ctx context.Context, host string) (sbrelay.Pin, error) {
	pin, err := acquireEdgePin(ctx, host)
	if err != nil {
		return sbrelay.Pin{}, err
	}
	return sbrelay.Pin{Server: pin.Server, Interface: pin.Interface, Mark: pin.Mark, Release: pin.Release}, nil
}

func pinTectonicEdge(ctx context.Context, host string) (tectonic.EdgePin, error) {
	pin, err := acquireEdgePin(ctx, host)
	if err != nil {
		return tectonic.EdgePin{}, err
	}
	return tectonic.EdgePin{Server: pin.Server, Control: pin.Control(), Release: pin.Release}, nil
}
