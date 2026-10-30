//go:build android || ios

package mobile

// Start/Stop bring up the data plane on the host's TUN fd, walking the transport cascade.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

type tunnelRuntime struct {
	dev             *device.Device
	starter         *starter
	serverID        string
	serverName      string
	activeTransport string
	// Set only for a multihop route; serverID stays the exit.
	entryServerID   string
	entryServerName string
	// postQuantum: the live peer carries an ML-KEM-derived pre-shared key.
	postQuantum bool
}

// label is the runtime's server and entry, for a status about it.
func (t *tunnelRuntime) label() statusJSON {
	return statusJSON{
		ServerID: t.serverID, ServerName: t.serverName,
		EntryServerID: t.entryServerID, EntryServerName: t.entryServerName,
	}
}

// tunnelLabel is the prepared server and entry, for a status about them.
func tunnelLabel(p *preparedTunnel) statusJSON {
	return statusJSON{
		ServerID: p.serverID, ServerName: p.serverName,
		EntryServerID: p.entryServerID, EntryServerName: p.entryServerName,
	}
}

// with returns a copy of s in state st.
func (s statusJSON) with(st, detail string) statusJSON {
	s.State = st
	s.Detail = detail
	return s
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
		discardTunFD(tunFd)
		return errors.New("mobile: no prepared tunnel; call Prepare first")
	}
	if active != nil {
		discardTunFD(tunFd)
		return errors.New("mobile: tunnel already running")
	}
	label := tunnelLabel(p)
	reportError := func(detail string) { pushStatusJSON(label.with(string(state.StateError), detail)) }

	tunFd, err := adoptTunFD(tunFd)
	if err != nil {
		err = fmt.Errorf("adopt tun fd: %w", err)
		reportError(err.Error())
		return err
	}

	st := newStarter(logs)
	candidates, err := transport.Select(p.profile, preferred, st)
	if err != nil {
		_ = syscall.Close(tunFd)
		reportError(err.Error())
		return err
	}
	if len(candidates) == 0 {
		_ = syscall.Close(tunFd)
		detail := "no transport is configured for this server"
		reportError(detail)
		return errors.New("mobile: " + detail)
	}
	candidates, promoted := transport.ReorderByMemory(candidates, preferred, netKey, memory)
	if promoted != "" {
		logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("trying %s first (last worked on this network)", promoted))
	}

	// openTUN has already closed the fd when it fails.
	tunDev, err := openTUN(tunFd)
	if err != nil {
		reportError(err.Error())
		return fmt.Errorf("create tun: %w", err)
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), newWGLogger())
	if err := dev.Up(); err != nil {
		dev.Close()
		reportError(err.Error())
		return fmt.Errorf("wg up: %w", err)
	}
	return walkCascade(dev, p, st, candidates, logs, netKey)
}

// walkCascade tries each candidate until one carries a handshake. It owns dev
// and closes it when every rung fails.
func walkCascade(dev *device.Device, p *preparedTunnel, st *starter,
	candidates []transport.Candidate, logs *state.LogStore, netKey string) error {
	label := tunnelLabel(p)
	ctx := context.Background()
	var failures []string
	for i, candidate := range candidates {
		pushStatusJSON(label.with(string(state.StateConnecting), "starting "+candidate.Kind))
		if err := bringUpOver(ctx, dev, p, p.presharedKeyRaw, st, candidate); err != nil {
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
		goLive(dev, st, label, len(p.presharedKeyRaw) == pq.PresharedKeySize, candidate.Kind)
		return nil
	}

	dev.Close()
	detail := "no transport could establish a tunnel: " + strings.Join(failures, "; ")
	pushStatusJSON(label.with(string(state.StateError), detail))
	return errors.New("mobile: " + detail)
}

// goLive publishes the runtime, then a connected status carrying the
// handshake just proven.
func goLive(dev *device.Device, st *starter, label statusJSON, postQuantum bool, kind string) {
	mu.Lock()
	activeTunnel = &tunnelRuntime{
		dev: dev, starter: st, serverID: label.ServerID, serverName: label.ServerName,
		activeTransport: kind, entryServerID: label.EntryServerID, entryServerName: label.EntryServerName,
		postQuantum: postQuantum,
	}
	mu.Unlock()

	connected := label.with(string(state.StateConnected), "")
	connected.ActiveTransport = kind
	connected.PostQuantum = postQuantum
	_, _, connected.LastHandshake = tunnelStats(dev)
	pushStatusJSON(connected)
}

// discardTunFD releases a refused Start's fd: Android hands ownership to Go, so
// it closes there; on iOS it only closes the dup, leaving the host's fd open.
func discardTunFD(fd int) {
	if owned, err := adoptTunFD(fd); err == nil {
		_ = syscall.Close(owned)
	}
}

// bringUpOver starts one transport and proves it carries a real WireGuard
// handshake. A started-but-blocked transport must not count as connected.
func bringUpOver(ctx context.Context, dev *device.Device, p *preparedTunnel, psk []byte, st *starter, candidate transport.Candidate) error {
	if err := candidate.Start(ctx, p.profile, nil); err != nil {
		return err
	}
	if err := dev.IpcSet(buildUAPI(p.wgPrivateKeyRaw, p.serverPubKeyRaw, psk, st.boundPort)); err != nil {
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

// tunnelStats reads the byte counters and newest handshake in one UAPI pass.
func tunnelStats(dev *device.Device) (rx, tx, lastHandshake int64) {
	if dev == nil {
		return 0, 0, 0
	}
	uapi, err := dev.IpcGet()
	if err != nil {
		return 0, 0, 0
	}
	return parseTunnelStats(uapi)
}

func lastHandshakeUnix(dev *device.Device) int64 {
	_, _, last := tunnelStats(dev)
	return last
}

// detachTunnel hands the running tunnel to exactly one stopper.
func detachTunnel() *tunnelRuntime {
	mu.Lock()
	defer mu.Unlock()
	t := activeTunnel
	activeTunnel = nil
	return t
}

func closeTunnel(t *tunnelRuntime) {
	if t.dev != nil {
		t.dev.Close()
	}
	if t.starter != nil {
		t.starter.stopActive(context.Background())
	}
}

func stopTunnel() {
	t := detachTunnel()
	if t == nil {
		return
	}
	pushStatusJSON(t.label().with(string(state.StateDisconnecting), ""))
	closeTunnel(t)
	pushStatusJSON(statusJSON{State: string(state.StateDisconnected)})
}

// stopTunnelForSwitch reports connecting to the prepared server, so the host
// never sees a disconnect between two servers.
func stopTunnelForSwitch() {
	t := detachTunnel()
	if t == nil {
		return
	}
	mu.Lock()
	next := t.label()
	if p := prepared; p != nil {
		next = tunnelLabel(p)
	}
	mu.Unlock()
	pushStatusJSON(next.with(string(state.StateConnecting), "switching server"))
	closeTunnel(t)
}

// pushStatusJSON publishes a whole status, so no field is dropped on the way.
func pushStatusJSON(s statusJSON) {
	mu.Lock()
	currentStatus = s
	sk := sink
	mu.Unlock()

	if sk == nil {
		return
	}
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	sk.OnStatus(string(b))
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
