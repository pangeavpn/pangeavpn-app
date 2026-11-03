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
	// cancel ends the context the transports were started under.
	cancel context.CancelFunc
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
	attempt, err := claimStart()
	if err != nil {
		discardTunFD(tunFd)
		return err
	}
	defer finishStart(attempt)

	mu.Lock()
	logs := wgLogs
	preferred := preferredTransport
	memory := transportMem
	netKey := networkKey()
	mu.Unlock()

	p := attempt.prepared
	label := tunnelLabel(p)
	reportError := func(detail string) { pushAttemptStatus(attempt, label.with(string(state.StateError), detail)) }

	tunFd, err = adoptTunFD(tunFd)
	if err != nil {
		err = fmt.Errorf("adopt tun fd: %w", err)
		reportError(err.Error())
		return err
	}

	// Synthesized per start from the prepared profile, so a new network gets its own answer.
	profile, synthesized := synthesizedProfile(p.profile, nat64Synthesize)
	if synthesized {
		logs.Add(state.LogInfo, state.SourceDaemon, "this network reaches the server over NAT64; dialing its synthesized IPv6 address")
	}

	st := newStarter(logs)
	candidates, err := transport.Select(profile, preferred, st)
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
	return walkCascade(attempt, profile, dev, st, candidates, logs, netKey)
}

// walkCascade tries each candidate on profile until one carries a handshake. It
// owns dev and closes it when every rung fails or a stop cancels the attempt.
func walkCascade(attempt *startAttempt, profile *state.Profile, dev *device.Device, st *starter,
	candidates []transport.Candidate, logs *state.LogStore, netKey string) error {
	p := attempt.prepared
	label := tunnelLabel(p)
	ctx := attempt.ctx
	var failures []string
	for i, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}
		pushAttemptStatus(attempt, label.with(string(state.StateConnecting), "starting "+candidate.Kind))
		if err := bringUpOver(ctx, dev, p, profile, st, candidate); err != nil {
			failures = append(failures, err.Error())
			logAdd(logs, state.LogWarn, fmt.Sprintf("%s transport did not establish a tunnel: %v", candidate.Kind, err))
			st.stopActive(context.Background())
			continue
		}
		if i > 0 {
			logAdd(logs, state.LogInfo, "fell back to "+candidate.Kind+" transport")
		}
		if !goLive(attempt, dev, st, label, len(p.presharedKeyRaw) == pq.PresharedKeySize, candidate.Kind) {
			break
		}
		rememberTransport(netKey, candidate.Kind)
		return nil
	}

	st.stopActive(context.Background())
	dev.Close()
	if ctx.Err() != nil {
		return errStartCancelled
	}
	detail := "no transport could establish a tunnel: " + strings.Join(failures, "; ")
	pushAttemptStatus(attempt, label.with(string(state.StateError), detail))
	return errors.New("mobile: " + detail)
}

// goLive publishes the runtime, then a connected status carrying the handshake
// just proven. It refuses once a stop has cancelled the attempt.
func goLive(attempt *startAttempt, dev *device.Device, st *starter, label statusJSON, postQuantum bool, kind string) bool {
	mu.Lock()
	if attempt.ctx.Err() != nil {
		mu.Unlock()
		return false
	}
	activeTunnel = &tunnelRuntime{
		dev: dev, starter: st, serverID: label.ServerID, serverName: label.ServerName,
		activeTransport: kind, entryServerID: label.EntryServerID, entryServerName: label.EntryServerName,
		postQuantum: postQuantum, cancel: attempt.cancel,
	}
	attempt.live = true
	mu.Unlock()

	connected := label.with(string(state.StateConnected), "")
	connected.ActiveTransport = kind
	connected.PostQuantum = postQuantum
	_, _, connected.LastHandshake = tunnelStats(dev)
	pushAttemptStatus(attempt, connected)
	return true
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
func bringUpOver(ctx context.Context, dev *device.Device, p *preparedTunnel, profile *state.Profile, st *starter, candidate transport.Candidate) error {
	if err := candidate.Start(ctx, profile, nil); err != nil {
		return err
	}
	if err := dev.IpcSet(buildUAPI(p.wgPrivateKeyRaw, p.serverPubKeyRaw, p.presharedKeyRaw, st.boundPort)); err != nil {
		return fmt.Errorf("%s: wg configure: %w", candidate.Kind, err)
	}
	if err := waitForHandshake(ctx, dev, handshakeTimeout); err != nil {
		return fmt.Errorf("%s: %w", candidate.Kind, err)
	}
	return nil
}

// waitForHandshake blocks until the peer completes a handshake or ctx ends. A
// fresh device has none, so any non-zero value belongs to this attempt.
func waitForHandshake(ctx context.Context, dev *device.Device, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	poll := time.NewTicker(handshakePollInterval)
	defer poll.Stop()
	for {
		if lastHandshakeUnix(dev) > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("no wireguard handshake before timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-poll.C:
		}
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

// detachTunnel hands the running tunnel to exactly one stopper and cancels a
// start still in its cascade, under mu so that start can no longer go live.
func detachTunnel() (*tunnelRuntime, *startAttempt) {
	mu.Lock()
	defer mu.Unlock()
	t, pending := activeTunnel, starting
	activeTunnel, starting = nil, nil
	if pending != nil {
		pending.cancel()
	}
	return t, pending
}

func closeTunnel(t *tunnelRuntime) {
	if t.dev != nil {
		t.dev.Close()
	}
	if t.starter != nil {
		t.starter.stopActive(context.Background())
	}
	if t.cancel != nil {
		t.cancel()
	}
}

func stopTunnel() {
	t, pending := detachTunnel()
	awaitUnwind(pending)
	if t == nil {
		if pending != nil {
			pushStatusJSON(statusJSON{State: string(state.StateDisconnected)})
		}
		return
	}
	pushStatusJSON(t.label().with(string(state.StateDisconnecting), ""))
	closeTunnel(t)
	pushStatusJSON(statusJSON{State: string(state.StateDisconnected)})
}

// stopTunnelForSwitch reports connecting to the prepared server, so the host
// never sees a disconnect between two servers.
func stopTunnelForSwitch() {
	t, pending := detachTunnel()
	awaitUnwind(pending)
	if t == nil && pending == nil {
		return
	}
	mu.Lock()
	var next statusJSON
	if t != nil {
		next = t.label()
	} else {
		next = tunnelLabel(pending.prepared)
	}
	if p := prepared; p != nil {
		next = tunnelLabel(p)
	}
	mu.Unlock()
	pushStatusJSON(next.with(string(state.StateConnecting), "switching server"))
	if t != nil {
		closeTunnel(t)
	}
}

// pushStatusJSON publishes a whole status, so no field is dropped on the way.
func pushStatusJSON(s statusJSON) {
	mu.Lock()
	currentStatus = s
	sk := sink
	mu.Unlock()
	emitStatus(sk, s)
}

// pushAttemptStatus publishes only while the start is live, so the status a
// stop pushed is never followed by one from the start it cancelled.
func pushAttemptStatus(attempt *startAttempt, s statusJSON) {
	mu.Lock()
	if attempt.ctx.Err() != nil {
		mu.Unlock()
		return
	}
	currentStatus = s
	sk := sink
	mu.Unlock()
	emitStatus(sk, s)
}

func emitStatus(sk StatusSink, s statusJSON) {
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
