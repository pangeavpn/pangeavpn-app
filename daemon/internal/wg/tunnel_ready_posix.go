//go:build darwin || linux

package wg

import "github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"

// tunnelSessionReady: the interface is up with its address and routes by the
// time Start returns here, so there is nothing left to wait for.
func tunnelSessionReady(session *tunnelSession, _ state.WireGuardProfile) (bool, error) {
	return session.device != nil, nil
}
