// Package tectonic is the daemon's adapter for github.com/pangeavpn/tectonic: WireGuard
// over a pool of short-lived Chrome-like TLS lanes, relayed like internal/anytls.
package tectonic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	tect "github.com/pangeavpn/tectonic"
	"github.com/pangeavpn/tectonic/shape"
	"github.com/pangeavpn/tectonic/wire"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

var (
	_ transport.Manager           = (*Manager)(nil)
	_ transport.SessionWaiter     = (*Manager)(nil)
	_ transport.BoundPortReporter = (*Manager)(nil)
)

// Protect, when set, keeps each lane's socket out of the tunnel before it connects.
// Mobile points it at VpnService.protect(); desktop leaves it nil.
var Protect func(fd int) bool

// EdgePin is a CDN edge pinned to the physical NIC; see nicpin.Pin.
type EdgePin struct {
	Server  string
	Control func(network, address string, c syscall.RawConn) error
	// Dial, when set, opens the edge socket itself (the macOS broker) and wins over Control.
	Dial    func(ctx context.Context, network, address string) (net.Conn, error)
	Release func()
}

// EdgePinner pins a CDN edge's sockets to the physical NIC (desktop): the edge is a shared
// address, so it never gets a route around the tunnel that every app would follow.
type EdgePinner func(ctx context.Context, host string) (EdgePin, error)

// Manager owns a loopback UDP listener WireGuard's peer Endpoint points at,
// bridged to one Tectonic session.
type Manager struct {
	// startMu serializes Start/Stop end to end so two callers can't both pass
	// the running check and race to bind the same local port.
	startMu sync.Mutex

	mu      sync.RWMutex
	logs    *state.LogStore
	running bool
	profile state.TectonicProfile
	// gaming is gaming mode for the next Start; runningGaming is what the live session uses.
	gaming, runningGaming bool

	conn      *tect.Conn
	localConn *net.UDPConn
	cancel    context.CancelFunc

	boundLocalPort int
	done           chan struct{}
	generation     uint64

	// modes remembers the SNI mode that logged in per network for the process lifetime.
	modes      tect.ModeStore
	networkKey func() string
	edgePin    EdgePinner
}

// SetEdgePinner makes every later Start with a CDN route pin its lanes to the physical NIC.
func (m *Manager) SetEdgePinner(pin EdgePinner) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.edgePin = pin
}

// pinEdge points the CDN lanes at the pinned edge address and binds their sockets. Direct
// lanes are left to the node's own route, so a NIC change can't strand them on the old one.
func (m *Manager) pinEdge(ctx context.Context, cfg *tect.Config, profile state.TectonicProfile) (func(), error) {
	host := profile.CDNEdgeHost()
	if m.edgePin == nil || cfg.Profile.CDN == nil || host == "" || cfg.Control != nil || cfg.DialContext != nil {
		return func() {}, nil
	}
	// Without a pin the CDN lanes would dial unpinned; dropping just them keeps the direct lanes.
	dropCDN := func(why error) (func(), error) {
		cfg.Profile.CDN = nil
		m.logs.Add(state.LogWarn, state.SourceTectonic, fmt.Sprintf("tectonic: CDN lanes off for this session: %v", why))
		return func() {}, nil
	}
	pin, err := m.edgePin(ctx, host)
	if err != nil {
		return dropCDN(err)
	}
	release := pin.Release
	if release == nil {
		release = func() {}
	}
	server, perr := netip.ParseAddr(pin.Server)
	if perr != nil || (pin.Control == nil && pin.Dial == nil) {
		release()
		return dropCDN(fmt.Errorf("pin for %s binds nothing", host))
	}
	port := "443"
	if _, p, err := net.SplitHostPort(strings.TrimSpace(profile.CDN.Edge)); err == nil && p != "" {
		port = p
	}
	cfg.Profile.CDN.Edge = net.JoinHostPort(pin.Server, port)
	onEdge := func(address string) bool {
		dst, err := netip.ParseAddrPort(address)
		return err == nil && dst.Addr().Unmap() == server.Unmap()
	}
	if dial := pin.Dial; dial != nil {
		var direct net.Dialer
		cfg.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if onEdge(address) {
				return dial(ctx, network, address)
			}
			return direct.DialContext(ctx, network, address)
		}
		return release, nil
	}
	bind := pin.Control
	cfg.Control = func(network, address string, c syscall.RawConn) error {
		if onEdge(address) {
			return bind(network, address, c)
		}
		return nil
	}
	return release, nil
}

// modes is shared by every Manager so a transport rebuilt per connect (mobile) still
// goes straight to the SNI mode that worked on a network.
var modes = tect.NewMemoryModeStore()

// NewManager returns a stopped manager. networkKey names the host's network so the
// working SNI mode is remembered; nil treats every connect as a new network.
func NewManager(logs *state.LogStore, networkKey func() string) *Manager {
	return &Manager{logs: logs, modes: modes, networkKey: networkKey}
}

// Start begins the session and returns once the loopback relay is up. Lanes
// open in the background; WaitForSession blocks until the first logs in.
func (m *Manager) Start(ctx context.Context, profile state.TectonicProfile) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.RLock()
	running, current := m.running, m.profile
	gaming, runningGaming := m.gaming, m.runningGaming
	m.mu.RUnlock()

	if running && reflect.DeepEqual(current, profile) && gaming == runningGaming {
		return nil
	}

	// Built first, so a malformed profile never tears down a live session.
	cfg, err := m.config(profile)
	if err != nil {
		return err
	}
	cfg.Redundant = gaming

	if running {
		if err := m.Stop(ctx); err != nil {
			return fmt.Errorf("tectonic: stop previous session: %w", err)
		}
	}

	releasePin, err := m.pinEdge(ctx, &cfg, profile)
	if err != nil {
		return err
	}
	conn, err := tect.Dial(ctx, cfg)
	if err != nil {
		releasePin()
		return fmt.Errorf("tectonic: %w", annotate(err))
	}

	localConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: profile.LocalPort})
	if err != nil {
		conn.Close()
		releasePin()
		return fmt.Errorf("tectonic: listen local udp: %w", err)
	}
	boundPort := profile.LocalPort
	if laddr, ok := localConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}

	// Rooted independently of ctx: the relay outlives this call.
	bridgeCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	m.mu.Lock()
	m.generation++
	generation := m.generation
	m.conn = conn
	m.localConn = localConn
	m.cancel = cancel
	m.boundLocalPort = boundPort
	m.running = true
	m.runningGaming = gaming
	m.profile = state.TectonicProfile{}
	if cloned := state.CloneTectonicProfile(&profile); cloned != nil {
		m.profile = *cloned
	}
	m.done = done
	m.mu.Unlock()

	m.logs.Add(state.LogInfo, state.SourceTectonic, fmt.Sprintf(
		"tectonic started, listening on 127.0.0.1:%d, relaying to %s", boundPort, conn.RemoteAddr()))

	go func() {
		bridgeErr := transport.BridgeUDP(bridgeCtx, localConn, conn, conn.RemoteAddr())
		cancel()
		localConn.Close()

		m.mu.Lock()
		if m.generation == generation {
			m.clearLocked()
		}
		m.mu.Unlock()

		if bridgeErr != nil && !transport.IsBridgeShutdown(bridgeErr) {
			m.logs.Add(state.LogWarn, state.SourceTectonic, fmt.Sprintf("tectonic bridge exited: %v", annotate(bridgeErr)))
		} else {
			m.logs.Add(state.LogInfo, state.SourceTectonic, "tectonic stopped")
		}
		close(done)
		// Last, as it waits up to a second for CLOSE to reach the node, and nothing needs that.
		conn.Close()
		releasePin()
	}()

	return nil
}

// SetGamingMode makes the next Start copy light traffic onto a second lane, and asks the
// node to do the same; a running session with the other setting is replaced on that Start.
func (m *Manager) SetGamingMode(on bool) {
	m.mu.Lock()
	m.gaming = on
	m.mu.Unlock()
}

// config turns the daemon's profile into the client's, validating it without dialing.
func (m *Manager) config(profile state.TectonicProfile) (tect.Config, error) {
	if profile.NodeHost() == "" {
		return tect.Config{}, errors.New("tectonic: profile has no node address")
	}
	if net.ParseIP(profile.NodeHost()) == nil {
		// A hostname would be resolved outside the tunnel, which Lockdown
		// blocks and which names the node to the network's resolver.
		return tect.Config{}, fmt.Errorf("tectonic: node %q is not an IP address", profile.Node)
	}
	if len(profile.KeyID) != wire.KeyIDLen || len(profile.DeviceKey) != wire.KeyLen {
		return tect.Config{}, fmt.Errorf("tectonic: key_id must be %d bytes and device_key %d", wire.KeyIDLen, wire.KeyLen)
	}
	var id wire.KeyID
	copy(id[:], profile.KeyID)
	if id.Expired(time.Now()) {
		return tect.Config{}, annotate(fmt.Errorf("tectonic: %w: key_id expired", tect.ErrCredentialRejected))
	}
	mode, err := parseSNIMode(profile.SNIMode)
	if err != nil {
		return tect.Config{}, err
	}
	var shaping []*shape.Profile
	if len(profile.Shaping) > 0 && string(profile.Shaping) != "null" {
		if err := json.Unmarshal(profile.Shaping, &shaping); err != nil {
			return tect.Config{}, fmt.Errorf("tectonic: shaping profiles: %w", err)
		}
	}
	p := tect.Profile{
		Node:        strings.TrimSpace(profile.Node),
		Nodes:       m.extraNodes(profile.Nodes),
		CoverDomain: profile.CoverDomain,
		KeyID:       append([]byte(nil), profile.KeyID...),
		DeviceKey:   append([]byte(nil), profile.DeviceKey...),
		Pins:        append([]string(nil), profile.Pins...),
		BorrowedSNI: append([]string(nil), profile.BorrowedSNI...),
		Shaping:     shaping,
	}
	if profile.CDN != nil {
		p.CDN = &tect.CDNRoute{Host: profile.CDN.Host, Path: profile.CDN.Path, Edge: profile.CDN.Edge}
	}
	network := ""
	if m.networkKey != nil {
		network = m.networkKey()
	}
	cfg := tect.Config{
		Profile: p,
		SNIMode: mode,
		SNI:     strings.TrimSpace(profile.SNI),
		Network: network,
		Modes:   m.modes,
		Logger:  newLogger(m.logs),
	}
	if protect := Protect; protect != nil {
		cfg.Control = protectControl(protect)
	}
	return cfg, nil
}

// extraNodes are the node's other addresses, which lanes spread across so
// that each address carries one at a time. Only IP literals: a hostname
// would be resolved outside the tunnel, which Lockdown blocks and which
// names the node to the network's resolver. One that is not a literal is
// dropped with a line in the log rather than refusing the profile, since
// these are an optimisation and the first address carries the session
// without them.
func (m *Manager) extraNodes(addrs []string) []string {
	var out []string
	for _, a := range addrs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		host := a
		if h, _, err := net.SplitHostPort(a); err == nil {
			host = h
		}
		if net.ParseIP(host) == nil {
			m.logs.Add(state.LogWarn, state.SourceTectonic, fmt.Sprintf(
				"tectonic: ignoring node address %q: not an IP address", a))
			continue
		}
		out = append(out, a)
	}
	return out
}

func parseSNIMode(s string) (tect.SNIMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return tect.SNIAuto, nil
	case "own":
		return tect.SNIOwn, nil
	case "borrowed":
		return tect.SNIBorrowed, nil
	case "none":
		return tect.SNINone, nil
	default:
		return 0, fmt.Errorf("tectonic: unknown SNI mode %q", s)
	}
}

// protectControl hands a lane's socket to protect before it connects; a socket left
// unprotected would route into the tunnel it is carrying.
func protectControl(protect func(fd int) bool) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		ok := false
		if err := c.Control(func(fd uintptr) { ok = protect(int(fd)) }); err != nil {
			return err
		}
		if !ok {
			return errors.New("tectonic: could not protect lane socket")
		}
		return nil
	}
}

// annotate says what to do about a rejected credential, which on its own
// reads like a node fault.
func annotate(err error) error {
	if errors.Is(err, tect.ErrCredentialRejected) {
		return fmt.Errorf("%w (the node no longer accepts this device's Tectonic credential; reconnect to fetch a new one)", err)
	}
	return err
}

// WaitForSession blocks until the first lane has logged in to the node, so a
// node that is unreachable, or that turns the credential away, fails here.
func (m *Manager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	m.mu.RLock()
	conn, running := m.conn, m.running
	m.mu.RUnlock()
	if !running || conn == nil {
		return errors.New("tectonic is not running")
	}
	if err := conn.WaitForSession(ctx, timeout); err != nil {
		return annotate(err)
	}
	stats := conn.Stats()
	m.logs.Add(state.LogInfo, state.SourceTectonic, fmt.Sprintf(
		"tectonic session up over %s (SNI mode %s)", stats.Carrier, stats.Mode.Mode))
	return nil
}

func (m *Manager) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return 0
	}
	return m.boundLocalPort
}

func (m *Manager) Status() state.TransportStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return state.TransportStatus{}
	}
	pid := os.Getpid()
	return state.TransportStatus{Running: true, PID: &pid}
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	conn := m.conn
	localConn := m.localConn
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Closing the session sends CLOSE on a lane, so the node drops it now rather than holding
	// it for the resume window; a switch has already cut the old node off, so nothing waits on it.
	if conn != nil {
		go conn.Close()
	}
	if localConn != nil {
		localConn.Close()
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		m.forceReset()
		m.logs.Add(state.LogWarn, state.SourceTectonic, "tectonic stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
		m.forceReset()
		m.logs.Add(state.LogWarn, state.SourceTectonic, "tectonic stop cancelled; forced shutdown")
		return ctx.Err()
	}
}

// forceReset drops shared state to stopped, so a later Start does not race
// a relay that has not yet noticed it was closed.
func (m *Manager) forceReset() {
	m.mu.Lock()
	m.clearLocked()
	m.generation++
	m.mu.Unlock()
}

func (m *Manager) clearLocked() {
	m.running = false
	m.conn = nil
	m.localConn = nil
	m.cancel = nil
	m.boundLocalPort = 0
	m.done = nil
	m.profile = state.TectonicProfile{}
}
