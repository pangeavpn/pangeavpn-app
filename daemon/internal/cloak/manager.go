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
	mu         sync.RWMutex
	logs       *state.LogStore
	running    bool
	stopping   bool
	udpConn    *net.UDPConn
	done       chan struct{}
	session    chan struct{}
	hasSession bool
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

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("listen UDP %s: %w", localAddr, err)
	}

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
	m.udpConn = udpConn
	m.running = true
	m.stopping = false
	m.done = done
	m.session = session
	m.hasSession = false
	m.mu.Unlock()

	pid := os.Getpid()
	m.logs.Add(state.LogInfo, state.SourceCloak, fmt.Sprintf("in-process cloak started (pid=%d) listening on %s", pid, localAddr))
	m.logs.Add(state.LogInfo, state.SourceCloak, fmt.Sprintf("cloak remote=%s encryption=%s numConn=%d udp=%t",
		remoteConfig.RemoteAddr, profile.EncryptionMethod, remoteConfig.NumConn, authInfo.Unordered))

	// Install logrus hook so vendored Cloak logs go into our LogStore.
	hook := &logStoreHook{logs: m.logs}
	log.AddHook(hook)

	dialer := &net.Dialer{}

	var sessionCounter uint32
	newSession := func() *mux.Session {
		sessionCounter++
		authInfo.SessionId = sessionCounter
		sesh := client.MakeSession(remoteConfig, authInfo, dialer)
		m.markSessionEstablished()
		return sesh
	}

	go func() {
		err := client.RouteUDP(udpConn, localConfig.Timeout, remoteConfig.Singleplex, newSession)

		m.mu.Lock()
		wasStopping := m.stopping
		m.running = false
		m.udpConn = nil
		m.done = nil
		m.session = nil
		m.stopping = false
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
	m.mu.Unlock()

	// Closing the UDP conn causes RouteUDP to return — unless it's
	// blocked inside MakeSession retrying TCP connections with no internet.
	udpConn.Close()

	// Wait briefly for a clean shutdown, but don't block disconnect on it.
	// If RouteUDP is stuck (e.g. MakeSession retrying with no internet),
	// force-mark as stopped so disconnect can proceed.
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		m.mu.Lock()
		m.running = false
		m.udpConn = nil
		m.done = nil
		m.session = nil
		m.stopping = false
		m.mu.Unlock()
		m.logs.Add(state.LogWarn, state.SourceCloak, "cloak stop timed out; forced shutdown (RouteUDP may still be draining)")
		return nil
	case <-ctx.Done():
		m.mu.Lock()
		m.running = false
		m.udpConn = nil
		m.done = nil
		m.session = nil
		m.stopping = false
		m.mu.Unlock()
		return nil
	}
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

	localPort := strconv.Itoa(profile.LocalPort)
	if profile.LocalPort <= 0 {
		localPort = "51820"
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
