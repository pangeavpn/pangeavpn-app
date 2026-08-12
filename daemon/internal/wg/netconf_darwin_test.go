//go:build darwin

package wg

import "testing"

// gatewayRouteOutput is what `route -n get` reports when the bypass is intact:
// an exact host route out of the physical interface.
const gatewayRouteOutput = `   route to: 203.0.113.9
destination: 203.0.113.9
    gateway: 192.168.1.1
  interface: en0
      flags: <UP,GATEWAY,HOST,DONE,STATIC>
 recvpipe  sendpipe  ssthresh  rtt,msec    rttvar  hopcount      mtu     expire
       0         0         0         0         0         0      1500         0
`

// tunnelRouteOutput is what it reports once the host route is gone and the
// tunnel's own 0.0.0.0/1 covers the endpoint — no gateway line, because an
// interface route has none. This is the state that black-holes the session.
const tunnelRouteOutput = `   route to: 203.0.113.9
destination: 0.0.0.0
       mask: 128.0.0.0
  interface: utun4
      flags: <UP,DONE,CLONING,STATIC>
`

func TestParseDarwinRouteGet(t *testing.T) {
	intact := parseDarwinRouteGet(gatewayRouteOutput)
	if intact.destination != "203.0.113.9" || intact.gateway != "192.168.1.1" || intact.iface != "en0" {
		t.Errorf("parsed %+v, want destination 203.0.113.9 via 192.168.1.1 on en0", intact)
	}

	covering := parseDarwinRouteGet(tunnelRouteOutput)
	if covering.destination != "0.0.0.0" || covering.iface != "utun4" {
		t.Errorf("parsed %+v, want destination 0.0.0.0 on utun4", covering)
	}
	if covering.gateway != "" {
		t.Errorf("gateway = %q, want empty — an interface route carries no gateway", covering.gateway)
	}
}

func TestEndpointRouteNeedsRepair(t *testing.T) {
	const (
		destination = "203.0.113.9"
		tunnel      = "utun4"
		gateway     = "192.168.1.1"
	)

	tests := []struct {
		name    string
		current darwinRoute
		repair  bool
	}{
		{
			name:    "intact bypass is left alone",
			current: darwinRoute{destination: destination, gateway: gateway, iface: "en0"},
			repair:  false,
		},
		{
			// The link flapped and took the host route with it. The endpoint now
			// matches the tunnel's own 0.0.0.0/1, so WireGuard is routed into the
			// tunnel it is trying to establish.
			name:    "host route dropped, covered by the tunnel",
			current: parseDarwinRouteGet(tunnelRouteOutput),
			repair:  true,
		},
		{
			// Roam or DHCP renewal onto a new gateway.
			name:    "gateway moved",
			current: darwinRoute{destination: destination, gateway: "10.20.0.1", iface: "en0"},
			repair:  true,
		},
		{
			// Belt and braces: an exact host route that somehow points at the
			// tunnel is the loop written down explicitly.
			name:    "host route points into the tunnel",
			current: darwinRoute{destination: destination, gateway: gateway, iface: tunnel},
			repair:  true,
		},
		{
			name:    "covered by a different route entirely",
			current: darwinRoute{destination: "0.0.0.0", gateway: gateway, iface: "en0"},
			repair:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := endpointRouteNeedsRepair(destination, tc.current, tunnel, gateway); got != tc.repair {
				t.Errorf("endpointRouteNeedsRepair(%+v) = %t, want %t", tc.current, got, tc.repair)
			}
		})
	}
}

// TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp proves a session with nothing
// recorded does not shell out at all — the guard runs on every health tick.
func TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp(t *testing.T) {
	repaired, err := ensureSessionEndpointRoutes(&tunnelSession{interfaceName: "utun4"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired {
		t.Error("repaired = true with no recorded routes")
	}
}
