package reach

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/sagernet/sing/common/control"
)

// DirectDialer dials ip on HubPort from the named interface, so the probe leaves
// by that NIC even while a VPN tunnel owns the default route.
func DirectDialer(ip, iface string) DialFunc {
	return func(ctx context.Context) (net.Conn, error) {
		ifc, err := net.InterfaceByName(iface)
		if err != nil {
			return nil, fmt.Errorf("reach: interface %q: %w", iface, err)
		}
		dialer := net.Dialer{Control: control.BindToInterface(nil, ifc.Name, ifc.Index)}
		return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(ip, strconv.Itoa(HubPort)))
	}
}
