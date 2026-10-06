//go:build windows

package wg

import (
	"context"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/conn/bindtest"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	testSwitchTunnelKey  = "pangea0"
	testSwitchPrivateKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	testSwitchPeerKey    = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	rebuildMTULogLine    = "mtu changed; rebuilding the device instead of reconfiguring in place"
)

type fakeResizableTun struct {
	tun.Device
	forced []int
}

func (f *fakeResizableTun) ForceMTU(mtu int) { f.forced = append(f.forced, mtu) }

type fakeFixedTun struct{ tun.Device }

// Replays wireguard-go closing the tun between ForceMTU's closed check and its event send.
type fakeClosingTun struct{ tun.Device }

func (fakeClosingTun) ForceMTU(int) {
	events := make(chan tun.Event)
	close(events)
	events <- tun.EventMTUUpdate
}

func newChannelTun() tun.Device { return tuntest.NewChannelTUN().TUN() }

// newLiveSession runs a real wireguard-go device over tunDev on an in-memory bind.
func newLiveSession(t *testing.T, tunDev tun.Device, mtu int) *tunnelSession {
	t.Helper()
	dev := device.NewDevice(tunDev, bindtest.NewChannelBinds()[0], device.NewLogger(device.LogLevelSilent, ""))
	t.Cleanup(dev.Close)
	return &tunnelSession{interfaceName: testSwitchTunnelKey, device: dev, tunDevice: tunDev, deviceMTU: mtu, windowsLUID: 42}
}

func newSwitchManager(session *tunnelSession) *wireGuardGoManager {
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	m.storeSession(testSwitchTunnelKey, session)
	return m
}

// switchConfig has no Endpoint, so the in-place path resolves and pins no real routes.
func switchConfig(t *testing.T, mtuLine string) parsedUserlandConfig {
	t.Helper()
	parsed, err := parseUserlandConfig("[Interface]\nPrivateKey = " + testSwitchPrivateKey +
		"\nAddress = 10.7.0.2/32\n" + mtuLine + "\n\n[Peer]\nPublicKey = " + testSwitchPeerKey + "\nAllowedIPs = 0.0.0.0/0\n")
	if err != nil {
		t.Fatalf("parse switch config: %v", err)
	}
	return parsed
}

func stubConfigureWindowsInterface(t *testing.T) *[]int {
	t.Helper()
	var mtus []int
	prev := configureWindowsInterfaceFn
	configureWindowsInterfaceFn = func(_ uint64, _, _, _ []string, mtu int) error {
		mtus = append(mtus, mtu)
		return nil
	}
	t.Cleanup(func() { configureWindowsInterfaceFn = prev })
	return &mtus
}

func loggedLine(logs *state.LogStore, want string) bool {
	for _, entry := range logs.Since(0) {
		if strings.Contains(entry.Msg, want) {
			return true
		}
	}
	return false
}

// A cascade into or out of shadowsocks changes the MTU; recreating the adapter
// for it makes Windows re-identify the tunnel network.
func TestTrySwitchInPlace_ResizesTheLiveDeviceForANewMTU(t *testing.T) {
	configured := stubConfigureWindowsInterface(t)
	dev := &fakeResizableTun{Device: newChannelTun()}
	session := newLiveSession(t, dev, 1420)
	m := newSwitchManager(session)

	if !m.trySwitchInPlace(context.Background(), testSwitchTunnelKey, switchConfig(t, "MTU = 1280"), nil) {
		t.Fatalf("an MTU change must re-point the live device, not rebuild it: %+v", m.logs.Since(0))
	}
	if len(dev.forced) != 1 || dev.forced[0] != 1280 {
		t.Fatalf("ForceMTU calls = %v, want [1280]", dev.forced)
	}
	if session.deviceMTU != 1280 {
		t.Fatalf("session.deviceMTU = %d, want 1280", session.deviceMTU)
	}
	if len(*configured) != 1 || (*configured)[0] != 1280 {
		t.Fatalf("interface MTUs configured = %v, want [1280]", *configured)
	}
	if loggedLine(m.logs, rebuildMTULogLine) {
		t.Fatalf("logged a rebuild for a resizable device: %+v", m.logs.Since(0))
	}
}

// NLMTU, not the device MTU, sizes what Windows sends into the tunnel, so the two must agree.
func TestTrySwitchInPlace_GivesTheInterfaceTheDeviceMTU(t *testing.T) {
	tests := []struct {
		name    string
		mtuLine string
		fromMTU int
		wantMTU int
	}{
		{name: "no MTU line leaves no stale shadowsocks NLMTU", mtuLine: "", fromMTU: 1280, wantMTU: 1420},
		{name: "oversized MTU is clamped like the device", mtuLine: "MTU = 9000", fromMTU: 1420, wantMTU: 1500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configured := stubConfigureWindowsInterface(t)
			session := newLiveSession(t, &fakeResizableTun{Device: newChannelTun()}, tc.fromMTU)
			m := newSwitchManager(session)

			if !m.trySwitchInPlace(context.Background(), testSwitchTunnelKey, switchConfig(t, tc.mtuLine), nil) {
				t.Fatalf("in-place switch failed: %+v", m.logs.Since(0))
			}
			if session.deviceMTU != tc.wantMTU {
				t.Fatalf("session.deviceMTU = %d, want %d", session.deviceMTU, tc.wantMTU)
			}
			if len(*configured) != 1 || (*configured)[0] != tc.wantMTU {
				t.Fatalf("interface MTUs configured = %v, want [%d]", *configured, tc.wantMTU)
			}
		})
	}
}

func TestMatchDeviceMTU_ResizesALiveDeviceInPlace(t *testing.T) {
	dev := &fakeResizableTun{Device: newChannelTun()}
	session := newLiveSession(t, dev, 1420)
	m := newSwitchManager(session)

	if !m.matchDeviceMTU(session, 1280) {
		t.Fatal("a device that can ForceMTU must be resized in place, not rebuilt")
	}
	if len(dev.forced) != 1 || dev.forced[0] != 1280 {
		t.Fatalf("ForceMTU calls = %v, want [1280]", dev.forced)
	}
	if session.deviceMTU != 1280 {
		t.Fatalf("session.deviceMTU = %d, want 1280", session.deviceMTU)
	}
	if !loggedLine(m.logs, "mtu changed to 1280; resized the device in place") {
		t.Fatalf("missing resize log line: %+v", m.logs.Since(0))
	}
}

func TestMatchDeviceMTU_SameMTULeavesTheDeviceAlone(t *testing.T) {
	dev := &fakeResizableTun{Device: newChannelTun()}
	session := newLiveSession(t, dev, 1420)
	m := newSwitchManager(session)

	if !m.matchDeviceMTU(session, 1420) {
		t.Fatal("an unchanged MTU must not force a rebuild")
	}
	if len(dev.forced) != 0 {
		t.Fatalf("ForceMTU called %v for an unchanged MTU", dev.forced)
	}
}

func TestMatchDeviceMTU_FallsBackToRebuildWithoutForceMTU(t *testing.T) {
	tests := []struct {
		name    string
		session func(t *testing.T) *tunnelSession
	}{
		{name: "no ForceMTU", session: func(t *testing.T) *tunnelSession {
			return newLiveSession(t, &fakeFixedTun{Device: newChannelTun()}, 1420)
		}},
		{name: "no device", session: func(*testing.T) *tunnelSession {
			return &tunnelSession{deviceMTU: 1420}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session := tc.session(t)
			m := newSwitchManager(session)

			if m.matchDeviceMTU(session, 1280) {
				t.Fatal("a device that cannot resize must fall back to a rebuild")
			}
			if session.deviceMTU != 1420 {
				t.Fatalf("session.deviceMTU = %d, want the unchanged 1420", session.deviceMTU)
			}
			if !loggedLine(m.logs, rebuildMTULogLine) {
				t.Fatalf("missing rebuild log line: %+v", m.logs.Since(0))
			}
		})
	}
}

// ForceMTU on a closed tun is a silent no-op, so claiming a resize would mislead the logs.
func TestMatchDeviceMTU_RebuildsAClosedDevice(t *testing.T) {
	dev := &fakeResizableTun{Device: newChannelTun()}
	session := newLiveSession(t, dev, 1420)
	m := newSwitchManager(session)
	session.device.Close()

	if m.matchDeviceMTU(session, 1280) {
		t.Fatal("a closed device must be rebuilt, not resized")
	}
	if len(dev.forced) != 0 {
		t.Fatalf("ForceMTU called %v on a closed device", dev.forced)
	}
	if session.deviceMTU != 1420 {
		t.Fatalf("session.deviceMTU = %d, want the unchanged 1420", session.deviceMTU)
	}
	if loggedLine(m.logs, "resized the device in place") {
		t.Fatalf("claimed a resize on a closed device: %+v", m.logs.Since(0))
	}
}

// The resize can run on the health loop's rebuild goroutine, where a panic takes the daemon down.
func TestMatchDeviceMTU_RebuildsWhenTheResizeRacesAClose(t *testing.T) {
	session := newLiveSession(t, fakeClosingTun{Device: newChannelTun()}, 1420)
	m := newSwitchManager(session)

	if m.matchDeviceMTU(session, 1280) {
		t.Fatal("a resize that raced the tun closing must fall back to a rebuild")
	}
	if session.deviceMTU != 1420 {
		t.Fatalf("session.deviceMTU = %d, want the unchanged 1420", session.deviceMTU)
	}
	if loggedLine(m.logs, "resized the device in place") {
		t.Fatalf("claimed a resize that never happened: %+v", m.logs.Since(0))
	}
}
