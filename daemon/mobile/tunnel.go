//go:build android

package mobile

// Start/Stop bring up the data plane on the TUN fd handed over by
// VpnService.Builder.establish(), walking the transport cascade.

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
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

type tunnelRuntime struct {
	dev             *device.Device
	starter         *starter
	serverID        string
	serverName      string
	activeTransport string
}

// handshakeTimeout bounds how long one transport is given to carry a first
// WireGuard handshake before the cascade moves on.
const handshakeTimeout = 10 * time.Second

const handshakePollInterval = 200 * time.Millisecond

// startTunnel walks the cascade over a single TUN fd. The fd is established
// once and reused: tearing it down between rungs would flash the VPN key icon.
func startTunnel(tunFd int) error {
	mu.Lock()
	p := prepared
	active := activeTunnel
	logs := wgLogs
	preferred := preferredTransport
	memory := transportMem
	netKey := networkKey()
	mu.Unlock()

	if p == nil {
		return errors.New("mobile: no prepared tunnel; call Prepare first")
	}
	if active != nil {
		return errors.New("mobile: tunnel already running")
	}

	st := newStarter(logs)
	candidates, err := transport.Select(p.profile, preferred, st)
	if err != nil {
		_ = syscall.Close(tunFd)
		pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
		return err
	}
	if len(candidates) == 0 {
		_ = syscall.Close(tunFd)
		detail := "no transport is configured for this server"
		pushStatus(string(state.StateError), detail, p.serverID, p.serverName)
		return errors.New("mobile: " + detail)
	}
	candidates, promoted := transport.ReorderByMemory(candidates, preferred, netKey, memory)
	if promoted != "" {
		logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("trying %s first (last worked on this network)", promoted))
	}

	tunDev, _, err := tun.CreateUnmonitoredTUNFromFD(tunFd)
	if err != nil {
		_ = syscall.Close(tunFd)
		pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
		return fmt.Errorf("create tun: %w", err)
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), newWGLogger())
	if err := dev.Up(); err != nil {
		dev.Close()
		pushStatus(string(state.StateError), err.Error(), p.serverID, p.serverName)
		return fmt.Errorf("wg up: %w", err)
	}

	ctx := context.Background()
	var failures []string
	for i, candidate := range candidates {
		pushStatus(string(state.StateConnecting), "starting "+candidate.Kind, p.serverID, p.serverName)
		if err := bringUpOver(ctx, dev, p, st, candidate); err != nil {
			failures = append(failures, err.Error())
			logs.Add(state.LogWarn, state.SourceDaemon,
				fmt.Sprintf("%s transport did not establish a tunnel: %v", candidate.Kind, err))
			st.stopActive(ctx)
			continue
		}
		if i > 0 {
			logs.Add(state.LogInfo, state.SourceDaemon, "fell back to "+candidate.Kind+" transport")
		}
		rememberTransport(netKey, candidate.Kind)

		mu.Lock()
		activeTunnel = &tunnelRuntime{
			dev: dev, starter: st, serverID: p.serverID,
			serverName: p.serverName, activeTransport: candidate.Kind,
		}
		mu.Unlock()

		pushStatusTransport(string(state.StateConnected), "", p.serverID, p.serverName, candidate.Kind)
		return nil
	}

	dev.Close()
	detail := "no transport could establish a tunnel: " + strings.Join(failures, "; ")
	pushStatus(string(state.StateError), detail, p.serverID, p.serverName)
	return errors.New("mobile: " + detail)
}

// bringUpOver starts one transport and proves it carries a real WireGuard
// handshake. A started-but-blocked transport must not count as connected.
func bringUpOver(ctx context.Context, dev *device.Device, p *preparedTunnel, st *starter, candidate transport.Candidate) error {
	if err := candidate.Start(ctx, p.profile, nil); err != nil {
		return err
	}
	if err := dev.IpcSet(buildUAPI(p.wgPrivateKeyRaw, p.serverPubKeyRaw, st.boundPort)); err != nil {
		return fmt.Errorf("%s: wg configure: %w", candidate.Kind, err)
	}
	if err := waitForHandshake(dev, handshakeTimeout); err != nil {
		return fmt.Errorf("%s: %w", candidate.Kind, err)
	}
	return nil
}

// waitForHandshake blocks until the peer completes a handshake. A fresh device
// has none, so any non-zero value belongs to this attempt.
func waitForHandshake(dev *device.Device, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if lastHandshakeUnix(dev) > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("no wireguard handshake before timeout")
		}
		time.Sleep(handshakePollInterval)
	}
}

func lastHandshakeUnix(dev *device.Device) int64 {
	uapi, err := dev.IpcGet()
	if err != nil {
		return 0
	}
	var latest int64
	for _, line := range strings.Split(uapi, "\n") {
		if !strings.HasPrefix(line, "last_handshake_time_sec=") {
			continue
		}
		v, _ := strconv.ParseInt(strings.TrimPrefix(line, "last_handshake_time_sec="), 10, 64)
		if v > latest {
			latest = v
		}
	}
	return latest
}

func stopTunnel() {
	mu.Lock()
	t := activeTunnel
	activeTunnel = nil
	mu.Unlock()

	if t == nil {
		return
	}

	pushStatus(string(state.StateDisconnecting), "", t.serverID, t.serverName)
	t.dev.Close()
	if t.starter != nil {
		t.starter.stopActive(context.Background())
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
