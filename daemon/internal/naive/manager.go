//go:build naive_cgo

package naive

// #include <stdlib.h>
// #include "pangea_naive_capi.h"
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/net/proxy"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// The node-side bridge's fixed loopback address, reached via the
// naive-server's SOCKS5 CONNECT once the TLS+HTTP2 tunnel is up.
// bridgeAddrFor is the node-side framed-UDP bridge this tunnel dials through
// the CONNECT stream: one instance per exit under multihop, the default
// otherwise. The node only listens on ports it generated, so an unconfigured
// exit is refused there.
func bridgeAddrFor(port int) string {
	if port <= 0 {
		port = state.DefaultNaiveBridgePort
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// Manager wraps the cgo-linked NaiveProxy engine behind transport.Manager,
// owning the loopback UDP listener WireGuard's peer Endpoint points at.
var _ transport.Manager = (*Manager)(nil)
var _ transport.SessionWaiter = (*Manager)(nil)
var _ transport.BoundPortReporter = (*Manager)(nil)

type Manager struct {
	mu      sync.RWMutex
	logs    *state.LogStore
	running bool

	// startMu serializes the blocking cgo Start/Stop calls so they never
	// run concurrently, without forcing Status/WaitForSession to wait on them.
	startMu sync.Mutex

	udpConn *net.UDPConn
	stream  net.Conn
	wgAddr  *net.UDPAddr

	boundLocalPort int
	activeProfile  state.NaiveProfile

	done          chan struct{}
	session       chan struct{}
	hasSession    bool
	sessionCtx    context.Context
	sessionCancel context.CancelFunc

	// generation bumps every Start, so a zombie relay goroutine from a previous
	// Start cannot clobber a fresh Start's state.
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

func nativeQueryStatus() (nativeStatus, error) {
	cStr := C.PangeaNaiveStatus()
	if cStr == nil {
		return nativeStatus{}, errors.New("naive: status query returned null")
	}
	defer C.free(unsafe.Pointer(cStr))
	var st nativeStatus
	if err := json.Unmarshal([]byte(C.GoString(cStr)), &st); err != nil {
		return nativeStatus{}, fmt.Errorf("naive: parse status json: %w", err)
	}
	return st, nil
}

// Start binds the engine's SOCKS5 listener and the Go-owned loopback UDP
// socket; the CONNECT dial happens in the background, see WaitForSession.
func (m *Manager) Start(ctx context.Context, profile state.NaiveProfile) error {
	m.mu.RLock()
	running := m.running
	current := m.activeProfile
	m.mu.RUnlock()

	if running {
		if current == profile {
			return nil
		}
		if err := m.Stop(ctx); err != nil {
			return fmt.Errorf("naive: stop previous session before switching profile: %w", err)
		}
	}

	remoteHost := strings.TrimSpace(profile.RemoteHost)
	if remoteHost == "" {
		return errors.New("naive remote host is required")
	}
	if profile.LocalPort < 0 {
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
		return fmt.Errorf("naive: marshal start config: %w", err)
	}

	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.RLock()
	alreadyRunning := m.running
	m.mu.RUnlock()
	if alreadyRunning {
		return nil
	}

	// Bracket the cgo call: the engine can die without unwinding to Go, so the
	// last line on disk is what localises a hard crash to the C side.
	m.logs.Add(state.LogInfo, state.SourceNaive, fmt.Sprintf(
		"naive: calling engine start (host=%s port=%d sni=%s)", cfg.RemoteHost, cfg.RemotePort, cfg.ServerName))

	cPayload := C.CString(string(payload))
	startResult := C.PangeaNaiveStart(cPayload)
	C.free(unsafe.Pointer(cPayload))

	m.logs.Add(state.LogInfo, state.SourceNaive, fmt.Sprintf("naive: engine start returned %d", int(startResult)))
	if startResult != 0 {
		st, statusErr := nativeQueryStatus()
		C.PangeaNaiveStop()
		if statusErr != nil {
			return fmt.Errorf("naive: engine failed to start (status unavailable: %v)", statusErr)
		}
		return fmt.Errorf("naive: engine failed to start: %s", st.Error)
	}

	st, statusErr := nativeQueryStatus()
	if statusErr != nil {
		C.PangeaNaiveStop()
		return fmt.Errorf("naive: engine started but status query failed: %w", statusErr)
	}
	if !st.Running || st.SocksPort <= 0 {
		C.PangeaNaiveStop()
		return fmt.Errorf("naive: engine started but reported no socks port (status: %+v)", st)
	}

	localAddr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", profile.LocalPort))
	udpAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		C.PangeaNaiveStop()
		return fmt.Errorf("resolve local UDP addr %s: %w", localAddr, err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		C.PangeaNaiveStop()
		return fmt.Errorf("listen UDP %s: %w", localAddr, err)
	}

	boundPort := udpAddr.Port
	if laddr, ok := udpConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}

	done := make(chan struct{})
	session := make(chan struct{})
	sessionCtx, sessionCancel := context.WithCancel(context.Background())

	m.mu.Lock()
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
	m.activeProfile = profile
	m.mu.Unlock()

	pid := os.Getpid()
	m.logs.Add(state.LogInfo, state.SourceNaive, fmt.Sprintf("in-process naive started (pid=%d) listening on 127.0.0.1:%d, engine socks=127.0.0.1:%d", pid, boundPort, st.SocksPort))

	go m.runSession(sessionCtx, generation, udpConn, st.SocksPort, bridgeAddrFor(profile.BridgePort), done, session)

	return nil
}

// runSession dials the SOCKS5 CONNECT tunnel, then relays datagrams until
// either side breaks. Runs for one Start/Stop cycle.
func (m *Manager) runSession(sessionCtx context.Context, generation uint64, udpConn *net.UDPConn, socksPort int, bridgeAddr string, done, session chan struct{}) {
	defer close(done)

	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{})
	if err != nil {
		m.logs.Add(state.LogError, state.SourceNaive, fmt.Sprintf("naive: build socks5 dialer: %v", err))
		m.teardown(generation)
		return
	}

	m.logs.Add(state.LogInfo, state.SourceNaive, fmt.Sprintf("naive: dialing %s via engine socks %s", bridgeAddr, socksAddr))

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
		// The dial may still land after Stop; drain it so a late-arriving
		// conn doesn't leak with no owner to close it.
		go func() {
			select {
			case conn := <-streamCh:
				conn.Close()
			case <-errCh:
			}
		}()
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

	established := time.Now()
	m.logs.Add(state.LogInfo, state.SourceNaive, "naive: engine SOCKS tunnel opened (upstream CONNECT is async)")

	relayDone := make(chan relayStat, 2)
	go m.relayFromWG(udpConn, stream, relayDone)
	go m.relayToWG(udpConn, stream, relayDone)

	// Relay exit = dead upstream tunnel (report it); ctx cancel = deliberate Stop.
	var stats []relayStat
	abnormal := true
	select {
	case s := <-relayDone:
		stats = append(stats, s)
	case <-sessionCtx.Done():
		abnormal = false
	}
	stream.Close()
	// Also close udpConn: whichever relay hasn't exited yet may be parked in
	// ReadFromUDP, which only stream.Close() would never wake.
	udpConn.Close()
	for len(stats) < 2 {
		stats = append(stats, <-relayDone)
	}

	if abnormal {
		m.logSessionEnded(established, stats)
	}

	m.teardown(generation)
}

// relayStat carries a relay's direction, throughput, and exit error.
type relayStat struct {
	dir    string
	frames uint64
	bytes  uint64
	err    error
}

// logSessionEnded warns why a session ended; 0 inbound frames = dead upstream.
func (m *Manager) logSessionEnded(established time.Time, stats []relayStat) {
	var out, in relayStat
	for _, s := range stats {
		switch s.dir {
		case "wg->bridge":
			out = s
		case "bridge->wg":
			in = s
		}
	}
	detail := ""
	if in.frames == 0 {
		detail = "; 0 received means the upstream proxy CONNECT tunnel never delivered a reply"
	}
	m.logs.Add(state.LogWarn, state.SourceNaive, fmt.Sprintf(
		"naive session ended after %s: sent %d frame(s)/%d B to bridge (err=%v), received %d frame(s)/%d B back (err=%v)%s",
		time.Since(established).Round(time.Millisecond),
		out.frames, out.bytes, out.err, in.frames, in.bytes, in.err, detail))
}

func (m *Manager) relayFromWG(udpConn *net.UDPConn, stream net.Conn, relayDone chan<- relayStat) {
	stat := relayStat{dir: "wg->bridge"}
	defer func() { relayDone <- stat }()
	buf := make([]byte, 65535)
	for {
		n, addr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			stat.err = err
			return
		}
		m.mu.Lock()
		m.wgAddr = addr
		m.mu.Unlock()
		if err := WriteFrame(stream, buf[:n]); err != nil {
			stat.err = err
			return
		}
		stat.frames++
		stat.bytes += uint64(n)
	}
}

func (m *Manager) relayToWG(udpConn *net.UDPConn, stream net.Conn, relayDone chan<- relayStat) {
	stat := relayStat{dir: "bridge->wg"}
	defer func() { relayDone <- stat }()
	for {
		payload, err := ReadFrame(stream)
		if err != nil {
			stat.err = err
			return
		}
		m.mu.RLock()
		addr := m.wgAddr
		m.mu.RUnlock()
		if addr == nil {
			continue
		}
		_, _ = udpConn.WriteToUDP(payload, addr)
		stat.frames++
		stat.bytes += uint64(len(payload))
	}
}

// teardown clears shared state, but only if this call still owns the
// current generation — see the generation field's doc comment.
func (m *Manager) teardown(generation uint64) {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.Lock()
	if m.generation != generation {
		m.mu.Unlock()
		return
	}
	udpConn := m.udpConn
	sessionCancel := m.sessionCancel
	waiter := m.session
	m.running = false
	m.udpConn = nil
	m.stream = nil
	m.wgAddr = nil
	m.hasSession = false
	m.session = nil
	m.boundLocalPort = 0
	m.sessionCtx = nil
	m.sessionCancel = nil
	// Retire the generation here, not just in Start: Stop's timeout path and
	// runSession's own exit both tear the same one down, and PangeaNaiveStop
	// is a C entry point with no promise of being idempotent.
	m.generation++
	m.mu.Unlock()

	if sessionCancel != nil {
		sessionCancel()
	}
	if udpConn != nil {
		udpConn.Close()
	}
	if waiter != nil {
		close(waiter)
	}
	C.PangeaNaiveStop()
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
		// teardown also closes this channel to wake us on a failed session;
		// recheck state instead of assuming the wake means success.
		m.mu.RLock()
		ok := m.running && m.hasSession
		m.mu.RUnlock()
		if !ok {
			return errors.New("naive session ended before it was established")
		}
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

	generation := m.generation
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
		m.teardown(generation)
		m.logs.Add(state.LogWarn, state.SourceNaive, "naive stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
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
// should target, or 0 when not running. Needed when LocalPort=0.
func (m *Manager) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return 0
	}
	return m.boundLocalPort
}
