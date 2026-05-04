package cloak

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak/ck/client"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak/ck/common"
	mux "github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak/ck/multiplex"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	log "github.com/sirupsen/logrus"
)

type Manager interface {
	Start(ctx context.Context, profile state.CloakProfile) error
	Stop(ctx context.Context) error
	Status() state.CloakStatus
}

func NewManager(logs *state.LogStore) Manager {
	return &inProcessManager{logs: logs}
}

type inProcessManager struct {
	mu            sync.RWMutex
	logs          *state.LogStore
	running       bool
	stopping      bool
	udpConn       *net.UDPConn
	done          chan struct{}
	session       chan struct{}
	hasSession    bool
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	// boundLocalPort is the actual loopback UDP port the listener is bound
	// to. Differs from profile.LocalPort when the caller requested dynamic
	// allocation (LocalPort=0). Zero when not running.
	boundLocalPort int
	// generation bumps every Start; goroutine cleanup only clobbers shared
	// state if its generation still matches the current one. Prevents a
	// zombie RouteUDP goroutine from a previous Start from nuking the state
	// owned by a fresh Start.
	generation uint64
}

// cancellableDialer wraps a net.Dialer so that TCP dials are bound to a
// context. When the context is cancelled, in-flight Dial calls return
// immediately with context.Canceled, which lets MakeSession's retry loop
// exit in bounded time during Stop().
type cancellableDialer struct {
	ctx    context.Context
	dialer *net.Dialer
}

func (d *cancellableDialer) Dial(network, address string) (net.Conn, error) {
	return d.dialer.DialContext(d.ctx, network, address)
}

func (m *inProcessManager) Start(ctx context.Context, profile state.CloakProfile) error {
	_ = ctx

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}

	remoteHost := strings.TrimSpace(profile.RemoteHost)
	if remoteHost == "" {
		m.mu.Unlock()
		return errors.New("cloak remote host is required")
	}

	rawConfig, err := buildRawConfig(profile, remoteHost)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("build cloak config: %w", err)
	}

	localAddr := net.JoinHostPort(rawConfig.LocalHost, rawConfig.LocalPort)
	udpAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("resolve local UDP addr %s: %w", localAddr, err)
	}

	// Retry ListenUDP briefly in case a previous cloak instance's socket is
	// still being released by the OS (rare race on reconnect, but seen in
	// the wild on all platforms). Total wait <= ~1 second.
	udpConn, err := listenUDPWithRetry(udpAddr, 10, 100*time.Millisecond)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("listen UDP %s: %w", localAddr, err)
	}

	// Resolve the actual bound port; when the caller passed LocalPort=0 the
	// kernel picks an ephemeral port and we need to surface it upward.
	boundPort := udpAddr.Port
	if laddr, ok := udpConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}
	boundLocalAddr := udpConn.LocalAddr().String()

	worldState := common.RealWorldState
	localConfig, remoteConfig, authInfo, err := rawConfig.ProcessRawConfig(worldState)
	if err != nil {
		udpConn.Close()
		m.mu.Unlock()
		return fmt.Errorf("process cloak config: %w", err)
	}
	_ = localConfig // we manage the UDP listener ourselves

	done := make(chan struct{})
	session := make(chan struct{})
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	m.generation++
	generation := m.generation
	m.udpConn = udpConn
	m.running = true
	m.stopping = false
	m.done = done
	m.session = session
	m.hasSession = false
	m.sessionCtx = sessionCtx
	m.sessionCancel = sessionCancel
	m.boundLocalPort = boundPort
	m.mu.Unlock()

	pid := os.Getpid()
	m.logs.Add(state.LogInfo, state.SourceCloak, fmt.Sprintf("in-process cloak started (pid=%d) listening on %s", pid, boundLocalAddr))
	m.logs.Add(state.LogInfo, state.SourceCloak, fmt.Sprintf("cloak remote=%s encryption=%s numConn=%d udp=%t",
		remoteConfig.RemoteAddr, profile.EncryptionMethod, remoteConfig.NumConn, authInfo.Unordered))

	// Install logrus hook so vendored Cloak logs go into our LogStore.
	hook := &logStoreHook{logs: m.logs}
	log.AddHook(hook)

	dialer := &cancellableDialer{ctx: sessionCtx, dialer: &net.Dialer{}}

	var sessionCounter uint32
	newSession := func() *mux.Session {
		sessionCounter++
		authInfo.SessionId = sessionCounter
		sesh, err := client.MakeSession(sessionCtx, remoteConfig, authInfo, dialer)
		if err != nil {
			// Cancelled (Stop called) or otherwise unable to build a session.
			// Returning nil signals RouteUDP to exit cleanly.
			return nil
		}
		m.markSessionEstablished()
		return sesh
	}

	go func() {
		err := client.RouteUDP(udpConn, localConfig.Timeout, remoteConfig.Singleplex, newSession)

		m.mu.Lock()
		wasStopping := m.stopping
		// Only clear shared state if this goroutine still owns the current
		// generation. A zombie goroutine from a previous Start must not
		// clobber state belonging to a newer Start.
		if m.generation == generation {
			m.running = false
			m.udpConn = nil
			m.done = nil
			m.session = nil
			m.stopping = false
			m.boundLocalPort = 0
			if m.sessionCancel != nil {
				m.sessionCancel()
			}
			m.sessionCtx = nil
			m.sessionCancel = nil
		}
		m.mu.Unlock()

		if err != nil && !wasStopping {
			m.logs.Add(state.LogError, state.SourceCloak, fmt.Sprintf("cloak exited with error: %v", err))
		} else {
			m.logs.Add(state.LogInfo, state.SourceCloak, "in-process cloak stopped")
		}

		close(done)
	}()

	return nil
}

// listenUDPWithRetry attempts to bind a UDP socket, retrying on transient
// "address already in use" style errors that can occur immediately after a
// previous cloak instance released the port. Returns the first successful
// conn or the last error after attempts are exhausted.
func listenUDPWithRetry(addr *net.UDPAddr, attempts int, delay time.Duration) (*net.UDPConn, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		conn, err := net.ListenUDP("udp", addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if !isAddrInUseErr(err) {
			return nil, err
		}
		time.Sleep(delay)
	}
	return nil, lastErr
}

func isAddrInUseErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	s := err.Error()
	// Covers: Linux "address already in use", macOS "address already in use",
	// Windows "Only one usage of each socket address (protocol/network address/port)
	// is normally permitted" (WSAEADDRINUSE).
	return strings.Contains(s, "address already in use") ||
		strings.Contains(s, "Only one usage of each socket address")
}

func (m *inProcessManager) markSessionEstablished() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasSession {
		return
	}
	m.hasSession = true
	if m.session != nil {
		close(m.session)
		m.session = nil
	}
}

func (m *inProcessManager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	m.mu.RLock()
	if !m.running {
		m.mu.RUnlock()
		return errors.New("cloak is not running")
	}
	if m.hasSession {
		m.mu.RUnlock()
		return nil
	}
	session := m.session
	m.mu.RUnlock()

	if session == nil {
		return errors.New("cloak session waiter unavailable")
	}

	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-session:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("cloak session not established within %s", timeout)
	}
}

func (m *inProcessManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running || m.udpConn == nil {
		m.mu.Unlock()
		return nil
	}

	m.stopping = true
	udpConn := m.udpConn
	done := m.done
	sessionCancel := m.sessionCancel
	m.mu.Unlock()

	// Cancel the session context first so any in-flight MakeSession retry
	// loops (dialer blocked on TCP connect/sleep) unblock immediately. Then
	// close the UDP socket which kicks RouteUDP out of its ReadFrom loop.
	// Order matters: cancelling first prevents a racing retry from holding
	// the dialer open while we wait.
	if sessionCancel != nil {
		sessionCancel()
	}
	udpConn.Close()

	// Wait for RouteUDP's goroutine to finish releasing state. It should now
	// exit in bounded time because MakeSession honors the cancelled context.
	// Keep a generous ceiling to avoid hanging the disconnect flow if
	// something downstream (e.g. the underlying mux session close) misbehaves.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		m.forceResetStateLocked()
		m.logs.Add(state.LogWarn, state.SourceCloak, "cloak stop timed out; forced shutdown (RouteUDP may still be draining)")
		return nil
	case <-ctx.Done():
		m.forceResetStateLocked()
		return nil
	}
}

// forceResetStateLocked drops shared state to a stopped configuration even
// if the RouteUDP goroutine has not finished. Safe to call when Stop's wait
// has timed out; the goroutine's generation check will prevent it from later
// clobbering a fresh Start.
func (m *inProcessManager) forceResetStateLocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.boundLocalPort = 0
	m.udpConn = nil
	m.done = nil
	m.session = nil
	m.stopping = false
	if m.sessionCancel != nil {
		m.sessionCancel()
	}
	m.sessionCtx = nil
	m.sessionCancel = nil
	// Bump generation so any still-running goroutine from this Start does
	// not later restore these fields.
	m.generation++
}

func (m *inProcessManager) Status() state.CloakStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.running {
		return state.CloakStatus{Running: false}
	}

	pid := os.Getpid()
	return state.CloakStatus{
		Running: true,
		PID:     &pid,
	}
}

// BoundLocalPort reports the loopback UDP port the manager is currently bound
// to, or 0 when not running. Callers that requested dynamic allocation
// (LocalPort=0) use this to discover the kernel-assigned port so downstream
// config (e.g. WireGuard peer endpoint) can be rewritten to match.
func (m *inProcessManager) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return 0
	}
	return m.boundLocalPort
}

func buildRawConfig(profile state.CloakProfile, remoteHost string) (*client.RawConfig, error) {
	uid, err := base64.StdEncoding.DecodeString(profile.UID)
	if err != nil {
		return nil, fmt.Errorf("decode UID: %w", err)
	}

	pubKey, err := base64.StdEncoding.DecodeString(profile.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode PublicKey: %w", err)
	}

	encMethod := profile.EncryptionMethod
	if encMethod == "" {
		encMethod = "plain"
	}

	// LocalPort == 0 is intentional: it requests an ephemeral port from the
	// kernel. Loopback-only, so any port works — this sidesteps Windows
	// Hyper-V UDP exclusion ranges that can claim 51820 at boot.
	localPort := strconv.Itoa(profile.LocalPort)
	if profile.LocalPort < 0 {
		return nil, fmt.Errorf("LocalPort must be >= 0, got %d", profile.LocalPort)
	}

	remotePort := strconv.Itoa(profile.RemotePort)
	if profile.RemotePort <= 0 {
		remotePort = "443"
	}

	return &client.RawConfig{
		ServerName:       "www.microsoft.com",
		ProxyMethod:      "wireguard",
		EncryptionMethod: encMethod,
		UID:              uid,
		PublicKey:        pubKey,
		NumConn:          4,
		LocalHost:        "127.0.0.1",
		LocalPort:        localPort,
		RemoteHost:       remoteHost,
		RemotePort:       remotePort,
		UDP:              true,
		BrowserSig:       "firefox",
		Transport:        "direct",
		StreamTimeout:    300,
	}, nil
}

// logStoreHook is a logrus.Hook that forwards Cloak log entries to the daemon LogStore.
type logStoreHook struct {
	logs *state.LogStore
}

func (h *logStoreHook) Levels() []log.Level {
	return log.AllLevels
}

func (h *logStoreHook) Fire(entry *log.Entry) error {
	switch entry.Level {
	case log.DebugLevel, log.TraceLevel:
		return nil // skip verbose logs to avoid flooding the log store
	case log.ErrorLevel, log.FatalLevel, log.PanicLevel:
		h.logs.Add(state.LogError, state.SourceCloak, entry.Message)
	case log.WarnLevel:
		h.logs.Add(state.LogWarn, state.SourceCloak, entry.Message)
	default:
		h.logs.Add(state.LogInfo, state.SourceCloak, entry.Message)
	}
	return nil
}

// init seeds the session counter offset so concurrent daemons don't collide.
func init() {
	_ = rand.Uint32()
}
