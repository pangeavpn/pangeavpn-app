//go:build naive_cgo

package naive

/*
#cgo LDFLAGS: -lpangea_naive
#include <stdlib.h>
#include "pangea_naive_capi.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/net/proxy"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// bridgeAddr is the node-side bridge's fixed loopback address, reached via
// the naive-server's SOCKS5 CONNECT once the client's TLS+HTTP2 tunnel is
// up. Co-located with the naive-server in the same container (see
// PangeaHubServer's node/src/naiveBridge.js, bound 127.0.0.1:9000).
const bridgeAddr = "127.0.0.1:9000"

// Manager wraps the cgo-linked NaiveProxy engine (github.com/pangeavpn/naiveproxy,
// branch feature/pangea-static-lib) behind the transport.Manager shape,
// mirroring cloak.inProcessManager's structure: a loopback UDP listener
// WireGuard's peer Endpoint points at, owned entirely on the Go side. The
// engine itself only exposes lifecycle control (Start/Stop/Status) across
// the cgo boundary plus a local SOCKS5 listener that this manager dials
// through to reach the node-side bridge over a single persistent stream.
var _ transport.Manager = (*Manager)(nil)

type Manager struct {
	mu      sync.RWMutex
	logs    *state.LogStore
	running bool

	udpConn *net.UDPConn
	stream  net.Conn
	wgAddr  *net.UDPAddr

	boundLocalPort int

	done          chan struct{}
	session       chan struct{}
	hasSession    bool
	sessionCtx    context.Context
	sessionCancel context.CancelFunc

	// generation bumps every Start; the relay goroutines only clobber shared
	// state if their generation still matches the current one, preventing a
	// zombie goroutine from a previous Start from nuking a fresh Start's
	// state. Mirrors cloak.inProcessManager's same pattern.
	generation uint64
}

func NewManager(logs *state.LogStore) *Manager {
	return &Manager{logs: logs}
}

type startConfig struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	ServerName string `json:"serverName"`
}

type nativeStatus struct {
	Running   bool   `json:"running"`
	SocksPort int    `json:"socksPort"`
	Error     string `json:"error"`
}

func nativeQueryStatus() nativeStatus {
	cStr := C.PangeaNaiveStatus()
	if cStr == nil {
		return nativeStatus{}
	}
	defer C.free(unsafe.Pointer(cStr))
	var st nativeStatus
	_ = json.Unmarshal([]byte(C.GoString(cStr)), &st)
	return st
}

// Start binds the engine's local SOCKS5 listener (via cgo) and the
// Go-owned loopback UDP socket WireGuard's peer Endpoint points at, then
// returns. The SOCKS5 CONNECT dial to the node-side bridge (a real network
// round trip through the naive-server's TLS+HTTP2 tunnel) happens in a
// background goroutine — callers that need to know once that succeeds use
// WaitForSession, mirroring cloak.inProcessManager exactly.
func (m *Manager) Start(ctx context.Context, profile state.NaiveProfile) error {
	_ = ctx

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}

	remoteHost := strings.TrimSpace(profile.RemoteHost)
	if remoteHost == "" {
		m.mu.Unlock()
		return errors.New("naive remote host is required")
	}
	if profile.LocalPort < 0 {
		m.mu.Unlock()
		return fmt.Errorf("LocalPort must be >= 0, got %d", profile.LocalPort)
	}

	cfg := startConfig{
		RemoteHost: remoteHost,
		RemotePort: profile.RemotePort,
		Username:   profile.Username,
		Password:   profile.Password,
		ServerName: profile.ServerName,
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("naive: marshal start config: %w", err)
	}

	cPayload := C.CString(string(payload))
	startResult := C.PangeaNaiveStart(cPayload)
	C.free(unsafe.Pointer(cPayload))
	if startResult != 0 {
		st := nativeQueryStatus()
		m.mu.Unlock()
		return fmt.Errorf("naive: engine failed to start: %s", st.Error)
	}

	st := nativeQueryStatus()
	if !st.Running || st.SocksPort <= 0 {
		C.PangeaNaiveStop()
		m.mu.Unlock()
		return fmt.Errorf("naive: engine started but reported no socks port (status: %+v)", st)
	}

	localAddr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", profile.LocalPort))
	udpAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		C.PangeaNaiveStop()
		m.mu.Unlock()
		return fmt.Errorf("resolve local UDP addr %s: %w", localAddr, err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		C.PangeaNaiveStop()
		m.mu.Unlock()
		return fmt.Errorf("listen UDP %s: %w", localAddr, err)
	}

	boundPort := udpAddr.Port
	if laddr, ok := udpConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}

	done := make(chan struct{})
	session := make(chan struct{})
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	m.generation++
	generation := m.generation
	m.udpConn = udpConn
	m.running = true
	m.done = done
	m.session = session
	m.hasSession = false
	m.sessionCtx = sessionCtx
	m.sessionCancel = sessionCancel
	m.boundLocalPort = boundPort
	m.stream = nil
	m.wgAddr = nil
	m.mu.Unlock()

	pid := os.Getpid()
	m.logs.Add(state.LogInfo, state.SourceNaive, fmt.Sprintf("in-process naive started (pid=%d) listening on 127.0.0.1:%d, engine socks=127.0.0.1:%d", pid, boundPort, st.SocksPort))

	go m.runSession(sessionCtx, generation, udpConn, st.SocksPort, done, session)

	return nil
}

// runSession dials the SOCKS5 CONNECT tunnel to the node-side bridge and,
// once established, relays datagrams until either side breaks. Runs for
// the lifetime of one Start/Stop cycle.
func (m *Manager) runSession(sessionCtx context.Context, generation uint64, udpConn *net.UDPConn, socksPort int, done, session chan struct{}) {
	defer close(done)

	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{})
	if err != nil {
		m.logs.Add(state.LogError, state.SourceNaive, fmt.Sprintf("naive: build socks5 dialer: %v", err))
		m.teardown(generation)
		return
	}

	streamCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := dialer.Dial("tcp", bridgeAddr)
		if err != nil {
			errCh <- err
			return
		}
		streamCh <- conn
	}()

	var stream net.Conn
	select {
	case stream = <-streamCh:
	case err := <-errCh:
		m.logs.Add(state.LogError, state.SourceNaive, fmt.Sprintf("naive: socks5 connect to bridge failed: %v", err))
		m.teardown(generation)
		return
	case <-sessionCtx.Done():
		m.teardown(generation)
		return
	}

	m.mu.Lock()
	if m.generation != generation {
		m.mu.Unlock()
		stream.Close()
		return
	}
	m.stream = stream
	m.hasSession = true
	if m.session != nil {
		close(m.session)
		m.session = nil
	}
	m.mu.Unlock()

	m.logs.Add(state.LogInfo, state.SourceNaive, "naive: session established with node bridge")

	relayDone := make(chan struct{}, 2)
	go m.relayFromWG(udpConn, stream, relayDone)
	go m.relayToWG(udpConn, stream, relayDone)

	select {
	case <-relayDone:
	case <-sessionCtx.Done():
	}
	stream.Close()
	<-relayDone // wait for the other relay goroutine to notice the closed stream/socket

	m.teardown(generation)
}

func (m *Manager) relayFromWG(udpConn *net.UDPConn, stream net.Conn, relayDone chan<- struct{}) {
	defer func() { relayDone <- struct{}{} }()
	buf := make([]byte, 65535)
	for {
		n, addr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		m.mu.Lock()
		m.wgAddr = addr
		m.mu.Unlock()
		if err := WriteFrame(stream, buf[:n]); err != nil {
			return
		}
	}
}

func (m *Manager) relayToWG(udpConn *net.UDPConn, stream net.Conn, relayDone chan<- struct{}) {
	defer func() { relayDone <- struct{}{} }()
	for {
		payload, err := ReadFrame(stream)
		if err != nil {
			return
		}
		m.mu.RLock()
		addr := m.wgAddr
		m.mu.RUnlock()
		if addr == nil {
			continue
		}
		_, _ = udpConn.WriteToUDP(payload, addr)
	}
}

// teardown clears shared state, but only if this call still owns the
// current generation — see the generation field's doc comment.
func (m *Manager) teardown(generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generation != generation {
		return
	}
	if m.udpConn != nil {
		m.udpConn.Close()
	}
	C.PangeaNaiveStop()
	m.running = false
	m.udpConn = nil
	m.stream = nil
	m.wgAddr = nil
	m.session = nil
	m.boundLocalPort = 0
	if m.sessionCancel != nil {
		m.sessionCancel()
	}
	m.sessionCtx = nil
	m.sessionCancel = nil
}

func (m *Manager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	m.mu.RLock()
	if !m.running {
		m.mu.RUnlock()
		return errors.New("naive is not running")
	}
	if m.hasSession {
		m.mu.RUnlock()
		return nil
	}
	session := m.session
	m.mu.RUnlock()

	if session == nil {
		return errors.New("naive session waiter unavailable")
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
		return fmt.Errorf("naive session not established within %s", timeout)
	}
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}

	udpConn := m.udpConn
	stream := m.stream
	done := m.done
	sessionCancel := m.sessionCancel
	m.mu.Unlock()

	if sessionCancel != nil {
		sessionCancel()
	}
	if stream != nil {
		stream.Close()
	}
	if udpConn != nil {
		udpConn.Close()
	}

	if done == nil {
		return nil
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		m.mu.Lock()
		generation := m.generation
		m.mu.Unlock()
		m.teardown(generation)
		m.logs.Add(state.LogWarn, state.SourceNaive, "naive stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
		m.mu.Lock()
		generation := m.generation
		m.mu.Unlock()
		m.teardown(generation)
		return nil
	}
}

func (m *Manager) Status() state.TransportStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return state.TransportStatus{Running: false}
	}
	pid := os.Getpid()
	return state.TransportStatus{Running: true, PID: &pid}
}

// BoundLocalPort reports the loopback UDP port WireGuard's peer Endpoint
// should target, or 0 when not running. See cloak.inProcessManager's same
// method for why LocalPort=0 (dynamic allocation) needs this.
func (m *Manager) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return 0
	}
	return m.boundLocalPort
}
