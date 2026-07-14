//go:build android

package mobile

// Start/Stop bring up the data plane on the TUN fd handed over by
// VpnService.Builder.establish(): cloak's in-process client proxies
// WireGuard's UDP over a multiplexed TLS session to the cloak server, and
// wireguard-go runs the WireGuard protocol on top of that loopback peer.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

type tunnelRuntime struct {
	dev        *device.Device
	serverID   string
	serverName string
}

// cloakSessionWaiter and cloakBoundPortReporter mirror the optional
// interfaces daemon/internal/api/service.go type-asserts the cloak.Manager
// against; the Manager interface itself only exposes Start/Stop/Status.
type cloakSessionWaiter interface {
	WaitForSession(ctx context.Context, timeout time.Duration) error
}

type cloakBoundPortReporter interface {
	BoundLocalPort() int
}

func startTunnel(tunFd int) error {
	mu.Lock()
	p := prepared
	active := activeTunnel
	mgr := cloakMgr
	mu.Unlock()

	if p == nil {
		return errors.New("mobile: no prepared tunnel; call Prepare first")
	}
	if active != nil {
		return errors.New("mobile: tunnel already running")
	}
	if mgr == nil {
		return errors.New("mobile: not initialized")
	}

	pushStatus(string(state.StateConnecting), "starting cloak", p.serverID, p.serverName)

	if err := mgr.Start(context.Background(), p.cloakProfile); err != nil {
		pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
		return fmt.Errorf("cloak start: %w", err)
	}

	if waiter, ok := mgr.(cloakSessionWaiter); ok {
		if err := waiter.WaitForSession(context.Background(), 10*time.Second); err != nil {
			_ = mgr.Stop(context.Background())
			pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
			return fmt.Errorf("cloak session: %w", err)
		}
	}

	boundPort := p.cloakProfile.LocalPort
	if reporter, ok := mgr.(cloakBoundPortReporter); ok {
		if bp := reporter.BoundLocalPort(); bp > 0 {
			boundPort = bp
		}
	}

	tunDev, _, err := tun.CreateUnmonitoredTUNFromFD(tunFd)
	if err != nil {
		_ = syscall.Close(tunFd)
		_ = mgr.Stop(context.Background())
		pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
		return fmt.Errorf("create tun: %w", err)
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), newWGLogger())

	if err := dev.IpcSet(buildUAPI(p.wgPrivateKeyRaw, p.serverPubKeyRaw, boundPort)); err != nil {
		dev.Close()
		_ = mgr.Stop(context.Background())
		pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
		return fmt.Errorf("wg configure: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		_ = mgr.Stop(context.Background())
		pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
		return fmt.Errorf("wg up: %w", err)
	}

	mu.Lock()
	activeTunnel = &tunnelRuntime{dev: dev, serverID: p.serverID, serverName: p.serverName}
	mu.Unlock()

	pushStatus(string(state.StateConnected), "", p.serverID, p.serverName)
	return nil
}

func stopTunnel() {
	mu.Lock()
	t := activeTunnel
	mgr := cloakMgr
	activeTunnel = nil
	mu.Unlock()

	if t == nil {
		return
	}

	pushStatus(string(state.StateDisconnecting), "", t.serverID, t.serverName)
	t.dev.Close()
	if mgr != nil {
		_ = mgr.Stop(context.Background())
	}
	pushStatus(string(state.StateDisconnected), "", "", "")
}

// tunnelBytes sums rx_bytes/tx_bytes across all peers reported by the
// running WireGuard device's UAPI get operation.
func tunnelBytes(dev *device.Device) (rx int64, tx int64) {
	uapi, err := dev.IpcGet()
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(uapi, "\n") {
		switch {
		case strings.HasPrefix(line, "rx_bytes="):
			v, _ := strconv.ParseInt(strings.TrimPrefix(line, "rx_bytes="), 10, 64)
			rx += v
		case strings.HasPrefix(line, "tx_bytes="):
			v, _ := strconv.ParseInt(strings.TrimPrefix(line, "tx_bytes="), 10, 64)
			tx += v
		}
	}
	return rx, tx
}

// buildUAPI renders the fixed single-peer WireGuard config: UAPI keys are
// hex, not base64, so the raw (already-decoded) key bytes are hex-encoded.
func buildUAPI(privRaw, pubRaw []byte, endpointPort int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", hex.EncodeToString(privRaw))
	fmt.Fprintf(&b, "public_key=%s\n", hex.EncodeToString(pubRaw))
	fmt.Fprintf(&b, "endpoint=127.0.0.1:%d\n", endpointPort)
	fmt.Fprintf(&b, "persistent_keepalive_interval=25\n")
	fmt.Fprintf(&b, "allowed_ip=0.0.0.0/0\n")
	return b.String()
}

func newWGLogger() *device.Logger {
	return &device.Logger{
		Verbosef: func(format string, args ...any) {
			wgLogs.Add(state.LogDebug, state.SourceWireGuard, fmt.Sprintf(format, args...))
		},
		Errorf: func(format string, args ...any) {
			wgLogs.Add(state.LogError, state.SourceWireGuard, fmt.Sprintf(format, args...))
		},
	}
}
