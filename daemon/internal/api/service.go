package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

// ErrTransportExhausted is returned only after auto mode tried every configured
// transport for one server. Desktop may then safely try another server.
var ErrTransportExhausted = errors.New("all configured transports failed")

// cloakManager is transport.Manager (Stop) plus Start/Status with Cloak's
// concrete types. cloak.Manager's real signature — Start(ctx,
// state.CloakProfile) error; Stop(ctx) error; Status() state.CloakStatus
// (daemon/internal/cloak/manager.go:24-28) — already satisfies this exactly,
// zero changes needed to the cloak package.
type cloakManager interface {
	transport.Manager
	Start(ctx context.Context, profile state.CloakProfile) error
	Status() state.CloakStatus
}

// naiveManager is transport.Manager (Stop) plus Start/Status with
// NaiveProxy's concrete types. naive.Manager (Task 5) is built to satisfy
// this directly.
type naiveManager interface {
	transport.Manager
	Start(ctx context.Context, profile state.NaiveProfile) error
	Status() state.TransportStatus
}

// realityManager is transport.Manager (Stop) plus Start/Status with
// VLESS+REALITY's concrete types. reality.Manager satisfies this directly.
type realityManager interface {
	transport.Manager
	Start(ctx context.Context, profile state.RealityProfile) error
	Status() state.TransportStatus
}

// hysteria2Manager is transport.Manager (Stop) plus Start/Status with
// Hysteria2's concrete types. hysteria2.Manager satisfies this directly.
type hysteria2Manager interface {
	transport.Manager
	Start(ctx context.Context, profile state.Hysteria2Profile) error
	Status() state.TransportStatus
}

// snowflakeManager is transport.Manager (Stop) plus Start/Status with
// Snowflake's concrete types. snowflake.Manager satisfies this directly.
type snowflakeManager interface {
	transport.Manager
	Start(ctx context.Context, profile state.SnowflakeProfile) error
	Status() state.TransportStatus
}

// transportMemory records and recalls the transport that last established a
// tunnel on a given network, so auto-connect can try it first. Satisfied by
// *state.TransportMemoryStore; a nil field disables the optimization.
type transportMemory interface {
	Lookup(networkKey string) (string, bool)
	Record(networkKey, transport string) error
}

type Service struct {
	machine    *state.Machine
	logs       *state.LogStore
	config     *state.ConfigStore
	cloak      cloakManager
	naive      naiveManager
	reality    realityManager
	hysteria2  hysteria2Manager
	snowflake  snowflakeManager
	wg         wg.Manager
	killSwitch platform.KillSwitch

	// transportMemory remembers the last-good transport per network; nil
	// disables the reorder/record optimization. networkKey fingerprints the
	// current network. Both are set once and read-only thereafter.
	transportMemory transportMemory
	networkKey      func() string

	opMu sync.Mutex

	// cancelConnect aborts the in-flight Connect when set. Disconnect uses
	// it to interrupt a connect that's still inside WaitForSession (up to
	// the 10s cloak timeout) so the user can bail without waiting for the
	// timeout to fire.
	cancelMu      sync.Mutex
	cancelConnect context.CancelFunc

	profileMu      sync.RWMutex
	currentProfile *state.Profile

	// sessionOpts are the options the live session was brought up with, so a
	// health-check rebuild can reproduce it rather than guess (AllowLAN shapes
	// both the kill switch and the WireGuard AllowedIPs). Guarded by profileMu.
	sessionOpts ConnectOptions

	// activeMu guards activeTransportKind, which of {cloak, naive, reality,
	// hysteria2, snowflake} is live for the current session. Empty string
	// when disconnected.
	activeMu            sync.RWMutex
	activeTransportKind string

	// handshakeTimeout bounds how long a single transport is given to carry a
	// first WireGuard handshake during bring-up. Defaults to
	// defaultWireGuardHandshakeTimeout; tests set it small.
	handshakeTimeout time.Duration
}

type wgPreflightChecker interface {
	Preflight(ctx context.Context, profile state.WireGuardProfile) error
}

type wgActiveInterfaceReporter interface {
	ActiveInterfaceName(ctx context.Context, profile state.WireGuardProfile) (string, error)
}

var wgListenPortPattern = regexp.MustCompile(`(?im)^\s*ListenPort\s*=\s*(\d+)\s*$`)

// wgLoopbackEndpointPattern matches "Endpoint = 127.0.0.1:<port>" lines in a
// WireGuard config's [Peer] section. We rewrite the port when cloak's
// loopback UDP socket binds to an ephemeral port instead of the default.
var wgLoopbackEndpointPattern = regexp.MustCompile(`(?im)^(\s*Endpoint\s*=\s*127\.0\.0\.1:)\d+(\s*)$`)

func NewService(
	machine *state.Machine,
	logs *state.LogStore,
	config *state.ConfigStore,
	cloakManager cloakManager,
	naiveManager naiveManager,
	realityManager realityManager,
	hysteria2Manager hysteria2Manager,
	snowflakeManager snowflakeManager,
	wgManager wg.Manager,
	killSwitch platform.KillSwitch,
) *Service {
	return &Service{
		machine:          machine,
		logs:             logs,
		config:           config,
		cloak:            cloakManager,
		naive:            naiveManager,
		reality:          realityManager,
		hysteria2:        hysteria2Manager,
		snowflake:        snowflakeManager,
		wg:               wgManager,
		killSwitch:       killSwitch,
		handshakeTimeout: defaultWireGuardHandshakeTimeout,
		networkKey:       currentNetworkKey,
	}
}

// SetTransportMemory wires in the per-network last-good-transport cache. Called
// once at startup; a nil store (or never calling this) leaves the optimization
// off, in which case connects always walk the full cascade.
func (s *Service) SetTransportMemory(store transportMemory) {
	s.transportMemory = store
}

// activeTransport returns whichever manager is live for the current session,
// or nil if disconnected. Most call sites (health check, Disconnect, Status)
// use this instead of branching on transport kind.
func (s *Service) activeTransport() transport.Manager {
	s.activeMu.RLock()
	kind := s.activeTransportKind
	s.activeMu.RUnlock()
	return s.managerForKind(kind)
}

// managerForKind maps a transport kind to its manager, or nil for an unknown
// or empty kind. Managers are fixed at construction, so this needs no lock.
func (s *Service) managerForKind(kind string) transport.Manager {
	switch kind {
	case "cloak":
		return s.cloak
	case "naive":
		return s.naive
	case "reality":
		return s.reality
	case "hysteria2":
		return s.hysteria2
	case "snowflake":
		return s.snowflake
	default:
		return nil
	}
}

func (s *Service) setActiveTransportKind(kind string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	s.activeTransportKind = kind
}

func (s *Service) StartBackground(ctx context.Context) {
	// Run reconciliation off the startup path so the HTTP API starts serving
	// immediately. The kill-switch re-apply makes blocking WFP syscalls that
	// ignore ctx and can stall at boot (Base Filtering Engine not ready);
	// gating ListenAndServe behind it would leave the frontend unable to reach
	// the daemon.
	go s.reconcileStartup(ctx)
	go s.healthLoop(ctx)
}

// ConnectOptions carries per-connect toggles from the client. Defaults to
// strict behavior (no LAN bypass) when zero-valued.
type ConnectOptions struct {
	// AllowLAN permits local-network IPv4 ranges both at the kill switch
	// and in the WireGuard AllowedIPs, so captive-portal re-checks and
	// gateway liveness probes work on restrictive WiFi.
	AllowLAN bool

	// Lockdown marks the kill switch as an intentional lock that survives
	// daemon restarts (re-applied on startup rather than cleared as stale).
	Lockdown bool

	// "cloak", "naive", "reality", "hysteria2", "snowflake", or "" (default:
	// cloak first, fall back to naive, reality, hysteria2, then snowflake).
	PreferredTransport string
}

func (s *Service) Connect(ctx context.Context, profileID string, opts ConnectOptions) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	// Make this Connect interruptible by Disconnect — see cancelConnect docs.
	connectCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.cancelMu.Lock()
	s.cancelConnect = cancel
	s.cancelMu.Unlock()
	defer func() {
		s.cancelMu.Lock()
		s.cancelConnect = nil
		s.cancelMu.Unlock()
	}()
	ctx = connectCtx

	profile, found := s.config.FindProfile(profileID)
	if !found {
		return fmt.Errorf("profile not found: %s", profileID)
	}

	if err := validateProfile(profile); err != nil {
		return err
	}

	currentState, _ := s.machine.Get()
	if currentState == state.StateConnecting || currentState == state.StateDisconnecting {
		return errors.New("daemon busy")
	}
	if currentState == state.StateConnected {
		if active, ok := s.getCurrentProfile(); ok {
			if active.ID == profile.ID {
				s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("profile %s already connected", profile.ID))
				return nil
			}
			err := fmt.Errorf("profile %s is active; disconnect before connecting profile %s", active.ID, profile.ID)
			s.setError(err.Error())
			return err
		}
	}

	s.setSessionOpts(opts)

	adopted, err := s.attachToRunningSession(ctx, profile)
	if err != nil {
		s.setError(err.Error())
		return err
	}
	if adopted {
		return nil
	}

	wireGuardProfile, err := wireGuardProfileFor(profile, opts.AllowLAN)
	if err != nil {
		s.setError(fmt.Sprintf("allow-lan config transform failed: %v", err))
		return err
	}
	if checker, ok := s.wg.(wgPreflightChecker); ok {
		if err := checker.Preflight(ctx, wireGuardProfile); err != nil {
			s.setError(fmt.Sprintf("wireguard preflight failed: %v", err))
			return err
		}
	}

	if err := s.ensureNoOtherRunningWireGuard(ctx, profile.ID); err != nil {
		s.setError(err.Error())
		return err
	}

	if err := s.ensureNoRunningWireGuard(ctx, profile); err != nil {
		s.setError(err.Error())
		return err
	}

	s.machine.Set(state.StateConnecting, "enabling kill switch")
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("connect requested with profile %s", profile.ID))
	stepStart := time.Now()
	permittedHosts := killSwitchPermits(profile)
	if err := s.killSwitch.Enable(ctx, permittedHosts, opts.AllowLAN, opts.Lockdown); err != nil {
		s.setError(fmt.Sprintf("kill switch enable failed: %v", err))
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch enabled (%dms)", time.Since(stepStart).Milliseconds()))

	if err := s.bringUpAfterKillSwitch(ctx, profile, wireGuardProfile, opts); err != nil {
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "connect flow completed")
	return nil
}

// wireGuardProfileFor builds the WireGuard profile a session runs with: the
// transport's own endpoints bypass the tunnel, and AllowLAN carves local
// ranges out of AllowedIPs.
func wireGuardProfileFor(profile state.Profile, allowLAN bool) (state.WireGuardProfile, error) {
	wireGuardProfile := withTransportBypassHosts(profile)
	if !allowLAN {
		return wireGuardProfile, nil
	}
	rewritten, err := wg.TransformWGConfigExcludeLAN(wireGuardProfile.ConfigText)
	if err != nil {
		return state.WireGuardProfile{}, err
	}
	wireGuardProfile.ConfigText = rewritten
	return wireGuardProfile, nil
}

// bringUpAfterKillSwitch starts the transport + WireGuard and updates the
// kill switch. Assumes opMu held, kill switch already Enable()d. Shared by
// Connect and Switch. StateConnected is reached only after a real WireGuard
// handshake (proven per transport inside startTransportWithHandshake), so a
// started-but-dead tunnel never reports connected.
func (s *Service) bringUpAfterKillSwitch(ctx context.Context, profile state.Profile, wireGuardProfile state.WireGuardProfile, opts ConnectOptions) error {
	s.machine.Set(state.StateConnecting, "starting transport")
	stepStart := time.Now()

	networkKey := s.currentNetworkKey()
	kind, err := s.startTransportWithHandshake(ctx, &profile, &wireGuardProfile, opts.PreferredTransport, networkKey)
	if err != nil {
		s.setError(fmt.Sprintf("transport start failed: %v", err))
		return err
	}
	s.setActiveTransportKind(kind)
	s.rememberTransport(networkKey, kind)
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("%s tunnel established with wireguard handshake (%dms)", kind, time.Since(stepStart).Milliseconds()))

	stepStart = time.Now()
	tunnelInterface := s.resolveWireGuardInterfaceName(ctx, wireGuardProfile)
	updateCh := make(chan error, 1)
	go func() {
		updateCh <- s.killSwitch.Update(ctx, tunnelInterface)
	}()

	if updateErr := <-updateCh; updateErr != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("kill switch tunnel update failed: %v", updateErr))
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch updated (%dms)", time.Since(stepStart).Milliseconds()))

	s.setCurrentProfile(profile)
	s.setSessionOpts(opts)
	s.machine.Set(state.StateConnected, "tunnel active")
	return nil
}

// rebindWireGuardEndpoint checks whether mgr bound a different local port
// than configured (LocalPort=0 requests dynamic allocation from the kernel)
// and, if so, rewrites the WireGuard peer Endpoint to match. mgr is checked
// via type assertion against transport.BoundPortReporter — passing `any`
// here (rather than a shared interface method) is fine because the check is
// optional/best-effort, exactly like the pre-existing cloakBoundPortReporter
// type assertion this replaces.
func (s *Service) rebindWireGuardEndpoint(mgr any, configuredLocalPort int, wireGuardProfile *state.WireGuardProfile) int {
	reporter, ok := mgr.(transport.BoundPortReporter)
	if !ok {
		return configuredLocalPort
	}
	boundPort := reporter.BoundLocalPort()
	if boundPort <= 0 || boundPort == configuredLocalPort {
		return configuredLocalPort
	}
	rewritten, replaced := rewriteLoopbackEndpointPort(wireGuardProfile.ConfigText, boundPort)
	if !replaced {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("transport bound loopback port %d but no matching Endpoint=127.0.0.1 line found in wireguard config; connection may fail", boundPort))
		return configuredLocalPort
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("transport bound loopback port %d; rewrote wireguard peer endpoint", boundPort))
	wireGuardProfile.ConfigText = rewritten
	return boundPort
}

// waitForManagedTransportStable polls isRunning until it reports true or
// duration elapses, catching the case where Start() returned nil but the
// underlying process/session exited immediately after (e.g. local port
// already occupied by something else).
func (s *Service) waitForManagedTransportStable(ctx context.Context, isRunning func() bool, localPort int, duration time.Duration) error {
	if isRunning() {
		return nil
	}

	deadline := time.Now().Add(duration)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		if isRunning() {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
	}

	if localPort > 0 {
		if owners, err := platform.UDPPortOwners(ctx, localPort, []int{os.Getpid()}); err == nil && len(owners) > 0 {
			return fmt.Errorf("transport process exited during startup; local port %d is already occupied by pid %d", localPort, owners[0])
		}
		return fmt.Errorf("transport process exited during startup; local port %d is already occupied by another process", localPort)
	}
	return errors.New("transport process exited during startup")
}

// startCloakTransport runs Cloak's start sequence. Caller owns cleanup on
// failure (stop cloak).
func (s *Service) startCloakTransport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error {
	if !s.cloak.Status().Running {
		cloakStartProfile := profile.Cloak
		cloakStartProfile.LocalPort = 0
		if err := s.cloak.Start(ctx, cloakStartProfile); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}
	profile.Cloak.LocalPort = s.rebindWireGuardEndpoint(s.cloak, profile.Cloak.LocalPort, wireGuardProfile)
	cloakRunning := func() bool { return s.cloak.Status().Running }
	if err := s.waitForManagedTransportStable(ctx, cloakRunning, profile.Cloak.LocalPort, 200*time.Millisecond); err != nil {
		return err
	}
	// NOTE: deliberately do NOT wait for a Cloak session here. Cloak's RouteUDP
	// only dials the server (MakeSession) after the first WireGuard packet
	// reaches the local listener, and WireGuard starts *after* this returns.
	// Blocking on WaitForSession would therefore always deadlock until timeout.
	// Cloak is ready once its process is up; the session forms as soon as WG
	// traffic flows, and a genuinely unreachable server surfaces as a failed
	// WireGuard handshake, which the health check then recovers/reports.
	return nil
}

// startNaiveTransport runs NaiveProxy's start sequence, mirroring
// startCloakTransport. Cleans up (stops naive) on its own failure, since
// it's always the last transport tried.
func (s *Service) startNaiveTransport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error {
	// De-alias from the config store's shared *NaiveProfile before mutating LocalPort.
	naiveCopy := *profile.Naive
	profile.Naive = &naiveCopy

	naiveStartProfile := *profile.Naive
	naiveStartProfile.LocalPort = 0
	if err := s.naive.Start(ctx, naiveStartProfile); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	profile.Naive.LocalPort = s.rebindWireGuardEndpoint(s.naive, profile.Naive.LocalPort, wireGuardProfile)
	naiveRunning := func() bool { return s.naive.Status().Running }
	if err := s.waitForManagedTransportStable(ctx, naiveRunning, profile.Naive.LocalPort, 200*time.Millisecond); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.naive.Stop(cleanupCtx)
		cleanupCancel()
		return err
	}
	if waiter, ok := s.naive.(transport.SessionWaiter); ok {
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := waiter.WaitForSession(waitCtx, 10*time.Second)
		cancel()
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.naive.Stop(cleanupCtx)
			cleanupCancel()
			return fmt.Errorf("session: %w", err)
		}
	}
	return nil
}

// startRealityTransport runs VLESS+REALITY's start sequence, mirroring
// startNaiveTransport. Cleans up (stops reality) on its own failure, since
// it's always the last transport tried.
func (s *Service) startRealityTransport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error {
	// De-alias from the config store's shared *RealityProfile before mutating LocalPort.
	realityCopy := *profile.Reality
	profile.Reality = &realityCopy

	realityStartProfile := *profile.Reality
	realityStartProfile.LocalPort = 0
	if err := s.reality.Start(ctx, realityStartProfile); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	profile.Reality.LocalPort = s.rebindWireGuardEndpoint(s.reality, profile.Reality.LocalPort, wireGuardProfile)
	realityRunning := func() bool { return s.reality.Status().Running }
	if err := s.waitForManagedTransportStable(ctx, realityRunning, profile.Reality.LocalPort, 200*time.Millisecond); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.reality.Stop(cleanupCtx)
		cleanupCancel()
		return err
	}
	if waiter, ok := s.reality.(transport.SessionWaiter); ok {
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := waiter.WaitForSession(waitCtx, 10*time.Second)
		cancel()
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.reality.Stop(cleanupCtx)
			cleanupCancel()
			return fmt.Errorf("session: %w", err)
		}
	}
	return nil
}

// startHysteria2Transport runs Hysteria2's start sequence, mirroring
// startNaiveTransport. Cleans up (stops hysteria2) on its own failure.
func (s *Service) startHysteria2Transport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error {
	// De-alias from the config store's shared *Hysteria2Profile before mutating LocalPort.
	hysteria2Copy := *profile.Hysteria2
	profile.Hysteria2 = &hysteria2Copy

	hysteria2StartProfile := *profile.Hysteria2
	hysteria2StartProfile.LocalPort = 0
	if err := s.hysteria2.Start(ctx, hysteria2StartProfile); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	profile.Hysteria2.LocalPort = s.rebindWireGuardEndpoint(s.hysteria2, profile.Hysteria2.LocalPort, wireGuardProfile)
	hysteria2Running := func() bool { return s.hysteria2.Status().Running }
	if err := s.waitForManagedTransportStable(ctx, hysteria2Running, profile.Hysteria2.LocalPort, 200*time.Millisecond); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.hysteria2.Stop(cleanupCtx)
		cleanupCancel()
		return err
	}
	if waiter, ok := s.hysteria2.(transport.SessionWaiter); ok {
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := waiter.WaitForSession(waitCtx, 10*time.Second)
		cancel()
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.hysteria2.Stop(cleanupCtx)
			cleanupCancel()
			return fmt.Errorf("session: %w", err)
		}
	}
	return nil
}

// startSnowflakeTransport runs Snowflake's start sequence, mirroring
// startHysteria2Transport. Cleans up (stops snowflake) on its own failure.
func (s *Service) startSnowflakeTransport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error {
	// De-alias from the config store's shared *SnowflakeProfile before mutating LocalPort.
	snowflakeCopy := *profile.Snowflake
	profile.Snowflake = &snowflakeCopy

	snowflakeStartProfile := *profile.Snowflake
	snowflakeStartProfile.LocalPort = 0
	if err := s.snowflake.Start(ctx, snowflakeStartProfile); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	profile.Snowflake.LocalPort = s.rebindWireGuardEndpoint(s.snowflake, profile.Snowflake.LocalPort, wireGuardProfile)
	snowflakeRunning := func() bool { return s.snowflake.Status().Running }
	if err := s.waitForManagedTransportStable(ctx, snowflakeRunning, profile.Snowflake.LocalPort, 200*time.Millisecond); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.snowflake.Stop(cleanupCtx)
		cleanupCancel()
		return err
	}
	if waiter, ok := s.snowflake.(transport.SessionWaiter); ok {
		// Snowflake rendezvous (broker polling, ICE gathering, DTLS/SCTP
		// handshake) is noticeably slower than the other transports' TLS/QUIC
		// handshakes, so it gets a longer session timeout.
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := waiter.WaitForSession(waitCtx, 30*time.Second)
		cancel()
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.snowflake.Stop(cleanupCtx)
			cleanupCancel()
			return fmt.Errorf("session: %w", err)
		}
	}
	return nil
}

// snowflakeReleaseGated disables the Snowflake transport for this release. Its
// WebRTC data plane is dropped by the always-on kill switch: the negotiated
// volunteer-proxy peer IP is discovered dynamically and is never permitted, so
// it cannot connect in production. All Snowflake code and wiring is retained;
// re-enable by removing the guards that read this flag (or setting it false)
// once the kill switch can permit the dynamic peer.
const snowflakeReleaseGated = true

// transportStartFn starts one transport's process/session and rebinds the
// WireGuard endpoint to its loopback port. It is the transport-level half of a
// bring-up; the WireGuard handshake in bringUpTransport is the tunnel-level
// half. Matches the signature of startCloakTransport and friends.
type transportStartFn func(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error

type transportCandidate struct {
	kind  string
	start transportStartFn
}

// transportCandidates returns the ordered transports to attempt for the given
// selection. "" / "auto" = the censorship-resistance cascade (see autoCascade).
// A specific selection = just that transport, with an error if the profile has
// no configuration for it.
func (s *Service) transportCandidates(profile *state.Profile, preferredTransport string) ([]transportCandidate, error) {
	switch preferredTransport {
	case "", "auto":
		return s.autoCascade(profile), nil
	case "cloak":
		return []transportCandidate{{"cloak", s.startCloakTransport}}, nil
	case "naive":
		if profile.Naive == nil {
			return nil, errors.New("naive transport requested but this profile has no naive configuration")
		}
		return []transportCandidate{{"naive", s.startNaiveTransport}}, nil
	case "reality":
		if profile.Reality == nil {
			return nil, errors.New("reality transport requested but this profile has no reality configuration")
		}
		return []transportCandidate{{"reality", s.startRealityTransport}}, nil
	case "hysteria2":
		if profile.Hysteria2 == nil {
			return nil, errors.New("hysteria2 transport requested but this profile has no hysteria2 configuration")
		}
		return []transportCandidate{{"hysteria2", s.startHysteria2Transport}}, nil
	case "snowflake":
		// Gated off for this release (see snowflakeReleaseGated). Re-enable by
		// removing this guard.
		if snowflakeReleaseGated {
			return nil, errors.New("snowflake transport is temporarily unavailable")
		}
		if profile.Snowflake == nil {
			return nil, errors.New("snowflake transport requested but this profile has no snowflake configuration")
		}
		return []transportCandidate{{"snowflake", s.startSnowflakeTransport}}, nil
	default:
		return nil, fmt.Errorf("unknown transport %q", preferredTransport)
	}
}

// autoCascadeOrder is the auto-mode fallback order, chosen for censorship
// resistance rather than build history — because the transports hide in
// different ways, a block that stops one often stops others that hide the same
// way, so consecutive attempts favor a different mechanism:
//
//   - cloak: the reliable default (TLS obfuscation to a decoy cover site).
//   - reality: the strongest TLS disguise — borrows a real site's TLS
//     handshake, so active probing and SNI blocking see a genuine site.
//   - hysteria2: a different wire protocol (UDP/QUIC + Salamander), so a block
//     that kills the TCP/TLS transports does not kill every attempt.
//   - naive: real Chromium TLS+HTTP/2 — a different TLS fingerprint than
//     reality, for when UDP is blocked and reality's approach was the problem.
//   - snowflake: heavy-artillery last resort (WebRTC rendezvous, no fixed
//     endpoint); currently gated off (see snowflakeReleaseGated).
//
// Per-network memory can still promote whatever last worked here ahead of this
// default (see reorderByMemory).
var autoCascadeOrder = []string{"cloak", "reality", "hysteria2", "naive", "snowflake"}

// autoCascade builds the auto-mode candidate list in autoCascadeOrder, keeping
// only the transports this profile actually configures.
func (s *Service) autoCascade(profile *state.Profile) []transportCandidate {
	candidates := make([]transportCandidate, 0, len(autoCascadeOrder))
	for _, kind := range autoCascadeOrder {
		if start, ok := s.transportStarter(profile, kind); ok {
			candidates = append(candidates, transportCandidate{kind, start})
		}
	}
	return candidates
}

// transportStarter returns the start func for kind if the profile can attempt
// it: Cloak is always available; the others require their optional profile
// block, and Snowflake is additionally gated off for this release.
func (s *Service) transportStarter(profile *state.Profile, kind string) (transportStartFn, bool) {
	switch kind {
	case "cloak":
		return s.startCloakTransport, true
	case "reality":
		if profile.Reality == nil {
			return nil, false
		}
		return s.startRealityTransport, true
	case "hysteria2":
		if profile.Hysteria2 == nil {
			return nil, false
		}
		return s.startHysteria2Transport, true
	case "naive":
		if profile.Naive == nil {
			return nil, false
		}
		return s.startNaiveTransport, true
	case "snowflake":
		if snowflakeReleaseGated || profile.Snowflake == nil {
			return nil, false
		}
		return s.startSnowflakeTransport, true
	default:
		return nil, false
	}
}

// startTransportWithHandshake brings up the first candidate transport that
// carries a real WireGuard handshake, and returns its kind. Because a mere
// started transport process is no longer accepted as "connected" (a dead or
// blocked server can start the process and never carry traffic), each
// candidate is proven end-to-end by bringUpTransport; a candidate that starts
// but never handshakes is torn down and the next is tried. In auto mode this
// is the fallback chain; with a specific transport there is a single candidate
// and no fallback.
func (s *Service) startTransportWithHandshake(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile, preferredTransport, networkKey string) (string, error) {
	autoMode := preferredTransport == "" || preferredTransport == "auto"
	candidates, err := s.transportCandidates(profile, preferredTransport)
	if err != nil {
		return "", err
	}
	candidates = s.reorderByMemory(candidates, preferredTransport, networkKey)

	var failures []string
	for i, candidate := range candidates {
		if err := s.bringUpTransport(ctx, profile, wireGuardProfile, candidate.kind, candidate.start); err != nil {
			// A cancelled context means Disconnect interrupted us; stop trying.
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			failures = append(failures, err.Error())
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("%s transport did not establish a tunnel: %v", candidate.kind, err))
			continue
		}
		if i > 0 {
			s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("fell back to %s transport", candidate.kind))
		}
		return candidate.kind, nil
	}

	if !autoMode && len(failures) == 1 {
		return "", errors.New(failures[0])
	}
	return "", fmt.Errorf("%w: %s", ErrTransportExhausted, strings.Join(failures, "; "))
}

// currentNetworkKey returns the fingerprint of the network the host is on, or
// "" when it can't be determined. Nil-safe wrapper around the injectable
// networkKey func.
func (s *Service) currentNetworkKey() string {
	if s.networkKey == nil {
		return ""
	}
	return s.networkKey()
}

// reorderByMemory moves the transport last known to work on this network to the
// front of the auto-mode candidate list, so a repeat connect on a familiar
// network tries the winner first instead of walking the whole cascade. Only
// applies in auto mode; a specific-transport request is left untouched.
func (s *Service) reorderByMemory(candidates []transportCandidate, preferredTransport, networkKey string) []transportCandidate {
	if s.transportMemory == nil || len(candidates) < 2 {
		return candidates
	}
	if preferredTransport != "" && preferredTransport != "auto" {
		return candidates
	}
	remembered, ok := s.transportMemory.Lookup(networkKey)
	if !ok {
		return candidates
	}
	idx := -1
	for i, candidate := range candidates {
		if candidate.kind == remembered {
			idx = i
			break
		}
	}
	if idx <= 0 {
		// Not configured for this profile, or already first — nothing to do.
		return candidates
	}
	reordered := make([]transportCandidate, 0, len(candidates))
	reordered = append(reordered, candidates[idx])
	reordered = append(reordered, candidates[:idx]...)
	reordered = append(reordered, candidates[idx+1:]...)
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("trying %s first (last worked on this network)", remembered))
	return reordered
}

// rememberTransport records kind as the last-good transport for networkKey so
// the next auto-connect on this network tries it first. Best-effort: a nil
// store, empty key, or persist error only forfeits the optimization.
func (s *Service) rememberTransport(networkKey, kind string) {
	if s.transportMemory == nil || networkKey == "" || kind == "" {
		return
	}
	if err := s.transportMemory.Record(networkKey, kind); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("failed to record last-good transport for network: %v", err))
	}
}

// bringUpTransport starts one transport, brings WireGuard up over it, and waits
// for a real WireGuard handshake — the proof the tunnel reaches the server
// through this transport. On any failure it tears WireGuard and the transport
// back down so the caller can try the next candidate. Making the handshake
// (not a merely started process) the success criterion is what lets a dead
// server fall through to the next transport, and what lets Cloak — which
// cannot prove its own session at start time — be validated here instead.
func (s *Service) bringUpTransport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile, kind string, start transportStartFn) (err error) {
	defer func() {
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.wg.Stop(cleanupCtx, *wireGuardProfile)
			if mgr := s.managerForKind(kind); mgr != nil {
				_ = mgr.Stop(cleanupCtx)
			}
			cleanupCancel()
		}
	}()

	if err = start(ctx, profile, wireGuardProfile); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}

	s.machine.Set(state.StateConnecting, fmt.Sprintf("starting wireguard over %s", kind))
	if err = s.wg.Start(ctx, *wireGuardProfile); err != nil {
		return fmt.Errorf("%s: wireguard start: %w", kind, err)
	}

	var wgStatus state.WireGuardStatus
	if wgStatus, err = s.wg.Status(ctx, *wireGuardProfile); err != nil {
		return fmt.Errorf("%s: wireguard status: %w", kind, err)
	}
	if !wgStatus.Running {
		err = fmt.Errorf("%s: wireguard failed to reach running state: %s", kind, wgStatus.Detail)
		return err
	}

	s.machine.Set(state.StateConnecting, fmt.Sprintf("waiting for %s handshake", kind))
	if err = s.waitForWireGuardHandshake(ctx, *wireGuardProfile); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	return nil
}

// defaultWireGuardHandshakeTimeout bounds how long a single transport is given
// to carry a first WireGuard handshake before it is abandoned for the next
// candidate. Matches the per-transport session timeouts (Cloak dials its server
// on the first WireGuard packet, so the handshake also covers Cloak's session).
const defaultWireGuardHandshakeTimeout = 10 * time.Second

// wireGuardHandshakePollInterval is how often waitForWireGuardHandshake
// re-reads WireGuard status while waiting for the first handshake.
const wireGuardHandshakePollInterval = 200 * time.Millisecond

// waitForWireGuardHandshake blocks until WireGuard completes a peer handshake
// (LastHandshakeUnix becomes non-zero) or the timeout elapses. A freshly
// started device has no prior handshake, so any non-zero value belongs to this
// session. Returns an error if the interface drops, the deadline passes, or ctx
// is cancelled (Disconnect).
func (s *Service) waitForWireGuardHandshake(ctx context.Context, wireGuardProfile state.WireGuardProfile) error {
	timeout := s.handshakeTimeout
	if timeout <= 0 {
		timeout = defaultWireGuardHandshakeTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		status, err := s.wg.Status(ctx, wireGuardProfile)
		if err != nil {
			return fmt.Errorf("wireguard status during handshake wait: %w", err)
		}
		if !status.Running {
			return errors.New("wireguard interface went down before handshake")
		}
		if status.LastHandshakeUnix > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no wireguard handshake within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wireGuardHandshakePollInterval):
		}
	}
}

// Switch hot-swaps profile without dropping the kill switch. Interruptible
// by Disconnect.
func (s *Service) Switch(ctx context.Context, newProfileID string, opts ConnectOptions) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	switchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.cancelMu.Lock()
	s.cancelConnect = cancel
	s.cancelMu.Unlock()
	defer func() {
		s.cancelMu.Lock()
		s.cancelConnect = nil
		s.cancelMu.Unlock()
	}()
	ctx = switchCtx

	currentState, _ := s.machine.Get()
	if currentState != state.StateConnected {
		return fmt.Errorf("switch requires connected state; currently %s", currentState)
	}

	oldProfile, ok := s.getCurrentProfile()
	if !ok {
		return errors.New("no active profile to switch from")
	}

	newProfile, found := s.config.FindProfile(newProfileID)
	if !found {
		return fmt.Errorf("profile not found: %s", newProfileID)
	}
	if newProfile.ID == oldProfile.ID {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("switch: already on %s, no-op", newProfile.ID))
		return nil
	}
	if err := validateProfile(newProfile); err != nil {
		return err
	}

	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("switch requested: %s -> %s (kill switch stays active)", oldProfile.ID, newProfile.ID))

	// Anything that can refuse the new server runs before the teardown, so a
	// failure costs nothing. Re-arming while the old tunnel is up is safe: every
	// backend applies the new set atomically, so the lock is never down.
	// Rejected before any teardown, so the old session is still live: go back to
	// Connected. StateError would stop healthLoop watching a working tunnel and
	// make the gate at the top of Switch reject the retry.
	refuse := func(err error, detail string) error {
		s.logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("switch: %s", detail))
		s.machine.Set(state.StateConnected, fmt.Sprintf("staying on %s: %s", oldProfile.ID, detail))
		return err
	}

	wireGuardProfile, err := wireGuardProfileFor(newProfile, opts.AllowLAN)
	if err != nil {
		return refuse(err, fmt.Sprintf("allow-lan config transform failed: %v", err))
	}
	if checker, ok := s.wg.(wgPreflightChecker); ok {
		if err := checker.Preflight(ctx, wireGuardProfile); err != nil {
			return refuse(err, fmt.Sprintf("wireguard preflight failed: %v", err))
		}
	}

	s.machine.Set(state.StateConnecting, fmt.Sprintf("switching to %s: updating kill switch", newProfile.ID))
	stepStart := time.Now()
	if err := s.killSwitch.Enable(ctx, killSwitchPermits(newProfile), opts.AllowLAN, opts.Lockdown); err != nil {
		return refuse(err, fmt.Sprintf("kill switch re-enable failed: %v", err))
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch re-enabled for %s (%dms)", newProfile.ID, time.Since(stepStart).Milliseconds()))

	s.machine.Set(state.StateConnecting, fmt.Sprintf("switching to %s: stopping wireguard", newProfile.ID))
	if err := s.wg.Stop(ctx, withTransportBypassHosts(oldProfile)); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("switch: wg stop warning: %v", err))
	}

	s.machine.Set(state.StateConnecting, fmt.Sprintf("switching to %s: stopping transport", newProfile.ID))
	if active := s.activeTransport(); active != nil {
		transportStopCtx, transportStopCancel := context.WithTimeout(context.Background(), 4*time.Second)
		if err := active.Stop(transportStopCtx); err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("switch: transport stop warning: %v", err))
		}
		transportStopCancel()
	}
	s.setActiveTransportKind("")

	s.clearCurrentProfile()

	if err := s.bringUpAfterKillSwitch(ctx, newProfile, wireGuardProfile, opts); err != nil {
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("switch flow completed: %s -> %s", oldProfile.ID, newProfile.ID))
	return nil
}

// ClearKillSwitch removes any active kill-switch rules without touching VPN
// session state. Used when the renderer turns Lockdown off while already
// disconnected: the daemon's last disconnect left the firewall in place, and
// the user now wants their network back.
func (s *Service) ClearKillSwitch(ctx context.Context) error {
	if !s.killSwitch.Active() {
		return nil
	}
	ksCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.killSwitch.Clear(ksCtx); err != nil {
		s.logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("kill switch clear (manual) failed: %v", err))
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "kill switch cleared (manual)")
	return nil
}

// loadKillSwitchState reads the persisted kill-switch state. Indirected for
// tests.
var loadKillSwitchState = platform.LoadKillSwitchStatePublic

// PermitHosts opens a control-plane hole in an already-engaged kill switch.
//
// A Lockdown lock engaged while disconnected blocks everything, including the
// Pangea hub — but the app must reach the hub to provision a profile before it
// has anything to connect to, so without this the lock is a trap: no
// provisioning, therefore no connection, therefore no way out but turning
// Lockdown off. The app calls this just before it provisions, so the hole opens
// on a connection attempt rather than sitting open the whole time the device is
// locked. The chosen server's own endpoints stay blocked until Connect permits
// them.
//
// Only IP literals are accepted: the lock blocks DNS, so a hostname could not
// be resolved while it is engaged, and the lock must never depend on a lookup.
// hosts is what the app knows (its resolved hub IP); when it has none — a cold
// start under lockdown, before it has reached the hub — the hub IP carried by
// the last provisioned profile's WireGuard bypass hosts is used instead.
// No-op when no lock is engaged.
func (s *Service) PermitHosts(ctx context.Context, hosts []string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if !s.killSwitch.Active() {
		return nil
	}

	permits := ipLiterals(hosts)
	if len(permits) == 0 {
		permits = ipLiterals(s.storedControlPlaneHosts())
	}
	if len(permits) == 0 {
		return errors.New("no control-plane IP to permit: none supplied and no provisioned profile carries one")
	}

	// Reuse the persisted AllowLAN/Locked flags: widening the permit set must
	// not change what kind of lock is engaged (dropping Locked would make a
	// deliberate lockdown lock look like crash leftover to the next startup).
	persisted, err := loadKillSwitchState()
	if err != nil {
		return fmt.Errorf("read kill switch state: %w", err)
	}
	if !persisted.Active {
		return errors.New("kill switch is active but no persisted state describes it; refusing to re-apply")
	}

	merged := mergeUniqueSorted(persisted.EndpointIPs, permits)
	if slices.Equal(merged, sortedCopy(persisted.EndpointIPs)) {
		return nil
	}

	ksCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.killSwitch.Enable(ksCtx, merged, persisted.AllowLAN, persisted.Locked); err != nil {
		s.logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("kill switch control-plane permit failed: %v", err))
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch permitted control-plane endpoints %v", permits))
	return nil
}

// storedControlPlaneHosts is the bypass hosts of every stored profile — where
// the app records the hub IP it provisioned through (see the desktop client's
// provision()). Transport endpoints are deliberately excluded: those are the
// server's own IPs and are only unblocked once Connect picks that server.
func (s *Service) storedControlPlaneHosts() []string {
	var out []string
	for _, profile := range s.config.Get().Profiles {
		out = append(out, profile.WireGuard.BypassHosts...)
	}
	return out
}

// ipLiterals keeps only entries that are already IPv4 addresses, so no caller
// can make the kill switch depend on a DNS lookup it is itself blocking.
func ipLiterals(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
			out = append(out, ip.To4().String())
		}
	}
	return out
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func mergeUniqueSorted(a, b []string) []string {
	merged := slices.Clone(a)
	for _, s := range b {
		if !slices.Contains(merged, s) {
			merged = append(merged, s)
		}
	}
	slices.Sort(merged)
	return slices.Compact(merged)
}

// EngageKillSwitch turns on the kill switch without starting a VPN session,
// giving the device a fail-closed network lock. Used when Lockdown is enabled
// while disconnected: internet is blocked immediately even though nothing is
// connected yet. Only the Pangea hub is reachable through it — the app talks to
// nothing else while disconnected, and cutting that off would leave it unable to
// list servers or provision, with no way to re-resolve anything since the lock
// blocks DNS too. VPN endpoints stay blocked: a server's IP is unblocked by
// Connect, once that server is the one being connected to.
//
// The hub IP comes from the stored profiles (see storedControlPlaneHosts) and
// is used as an IP literal, so the lock still lands with no DNS lookup to wait
// on — a lookup would delay the block and leak traffic until it landed. Before
// anything has ever been provisioned there is no hub IP to permit and this is a
// pure block-all; the app tops the permit up via PermitHosts when it provisions.
func (s *Service) EngageKillSwitch(ctx context.Context, profileID string, allowLAN bool) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.killSwitch.Active() {
		// Already armed, but it may only now be becoming a Lockdown lock.
		return s.markKillSwitchLocked(ctx, allowLAN)
	}

	// IP literals only, so the lock lands instantly with no DNS resolution —
	// which would otherwise delay the block (leaking until it lands) and can
	// hang the request. profileID is unused: every profile records the same hub,
	// and the VPN endpoints this profile would use are permitted later by
	// Connect, which re-enters Enable.
	_ = profileID
	hubPermits := ipLiterals(s.storedControlPlaneHosts())
	ksCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.killSwitch.Enable(ksCtx, hubPermits, allowLAN, true); err != nil {
		s.logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("kill switch engage failed: %v", err))
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch engaged (lockdown, no connection), hub permits %v", hubPermits))
	return nil
}

// Records an already-engaged lock as a Lockdown lock so reconcileStartup
// re-applies it instead of clearing it as stale. Re-arms with the persisted
// endpoints, which the backends see as unchanged (flag-only write).
func (s *Service) markKillSwitchLocked(ctx context.Context, allowLAN bool) error {
	persisted, err := loadKillSwitchState()
	if err != nil || !persisted.Active {
		s.logs.Add(state.LogWarn, state.SourceDaemon, "lockdown not recorded: kill switch state unreadable or inactive")
		return nil
	}
	if persisted.Locked {
		return nil
	}
	ksCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.killSwitch.Enable(ksCtx, persisted.EndpointIPs, allowLAN || persisted.AllowLAN, true); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not record lockdown on the engaged kill switch: %v", err))
		return nil
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "engaged kill switch marked as a lockdown lock")
	return nil
}

// Disconnect tears down the active VPN session. When keepKillSwitch is true
// (Lockdown mode), the firewall rules stay engaged so the device has no
// internet until the caller explicitly clears the kill switch.
func (s *Service) Disconnect(ctx context.Context, keepKillSwitch bool) error {
	return s.disconnect(ctx, func() bool { return keepKillSwitch })
}

// Shutdown serializes with all mutating operations before deciding whether a
// persisted Lockdown lock must survive process exit.
func (s *Service) Shutdown(ctx context.Context) error {
	return s.disconnect(ctx, func() bool {
		persisted, err := loadKillSwitchState()
		if err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not read kill switch state during shutdown; retaining it: %v", err))
			return true
		}
		return persisted.Active && persisted.Locked
	})
}

func (s *Service) disconnect(ctx context.Context, shouldKeepKillSwitch func() bool) error {
	// Interrupt any in-flight Connect first so we don't queue behind a
	// 10s WaitForSession. The cancelled connect will exit, release opMu,
	// and run its own cleanup — Disconnect then takes the lock and does
	// the rest (kill switch teardown, etc).
	s.cancelMu.Lock()
	if cancel := s.cancelConnect; cancel != nil {
		cancel()
	}
	s.cancelMu.Unlock()

	s.opMu.Lock()
	defer s.opMu.Unlock()
	keepKillSwitch := shouldKeepKillSwitch()

	currentState, _ := s.machine.Get()
	if currentState == state.StateDisconnecting {
		return nil
	}

	profile, hasProfile := s.getCurrentProfile()

	s.machine.Set(state.StateDisconnecting, "stopping wireguard")
	var cleanupErrors []string
	profilesToStop := make([]state.Profile, 0, 4)
	seenTunnelNames := make(map[string]struct{}, 4)

	addProfile := func(candidate state.Profile) {
		tunnelName := strings.TrimSpace(candidate.WireGuard.TunnelName)
		if tunnelName == "" {
			return
		}
		key := strings.ToLower(tunnelName)
		if _, seen := seenTunnelNames[key]; seen {
			return
		}
		seenTunnelNames[key] = struct{}{}
		profilesToStop = append(profilesToStop, candidate)
	}

	if hasProfile {
		addProfile(profile)
	}
	for _, running := range s.findRunningWireGuardProfiles(ctx) {
		addProfile(running)
	}

	for _, runningProfile := range profilesToStop {
		if err := s.wg.Stop(ctx, withTransportBypassHosts(runningProfile)); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("wireguard stop failed for %s: %v", runningProfile.WireGuard.TunnelName, err))
			s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
		}
	}

	s.machine.Set(state.StateDisconnecting, "verifying wireguard")
	for _, runningProfile := range profilesToStop {
		status, err := s.wg.Status(ctx, withTransportBypassHosts(runningProfile))
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("wireguard status check failed for %s: %v", runningProfile.WireGuard.TunnelName, err))
			s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
			continue
		}
		if status.Running {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("wireguard tunnel %s still running (%s)", runningProfile.WireGuard.TunnelName, status.Detail))
			s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
		}
	}

	s.machine.Set(state.StateDisconnecting, "stopping transport")
	// Use a short timeout for transport stop — don't let it block disconnect.
	if active := s.activeTransport(); active != nil {
		transportCtx, transportCancel := context.WithTimeout(context.Background(), 4*time.Second)
		if err := active.Stop(transportCtx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("transport stop failed: %v", err))
			s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
		}
		transportCancel()
	}
	s.setActiveTransportKind("")

	// Always clear profile and kill switch regardless of earlier errors.
	s.clearCurrentProfile()

	if s.killSwitch.Active() {
		if keepKillSwitch {
			// Retaining the lock past the session is what Lockdown means; record it
			// or the next startup clears it as stale.
			_ = s.markKillSwitchLocked(ctx, false)
			s.logs.Add(state.LogInfo, state.SourceDaemon, "kill switch retained (lockdown mode)")
		} else {
			s.machine.Set(state.StateDisconnecting, "clearing kill switch")
			ksCtx, ksCancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := s.killSwitch.Clear(ksCtx); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Sprintf("kill switch clear failed: %v", err))
				s.logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("kill switch clear failed: %v", err))
			} else {
				s.logs.Add(state.LogInfo, state.SourceDaemon, "kill switch cleared")
			}
			ksCancel()
		}
	}

	// Always transition to disconnected, even with partial cleanup failures.
	s.machine.Set(state.StateDisconnected, "idle")
	if len(cleanupErrors) > 0 {
		detail := fmt.Sprintf("disconnect completed with warnings: %s", strings.Join(cleanupErrors, "; "))
		s.logs.Add(state.LogWarn, state.SourceDaemon, detail)
		return errors.New(detail)
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "disconnect flow completed")
	return nil
}

func (s *Service) Status(ctx context.Context) state.StatusResponse {
	stateValue, detail := s.machine.Get()

	s.activeMu.RLock()
	activeKind := s.activeTransportKind
	s.activeMu.RUnlock()

	cloakStatus := s.cloak.Status()
	naiveStatus := s.naive.Status()
	realityStatus := s.reality.Status()
	hysteria2Status := s.hysteria2.Status()
	snowflakeStatus := s.snowflake.Status()

	wgStatus := state.WireGuardStatus{Running: false, Detail: "not connected"}
	if profile, ok := s.getCurrentProfile(); ok {
		if activeKind == "cloak" {
			cloakStatus = s.cloakStatusForProfile(ctx, profile)
		}

		result, err := s.wg.Status(ctx, profile.WireGuard)
		if err != nil {
			wgStatus = state.WireGuardStatus{Running: false, Detail: "status check failed"}
		} else {
			wgStatus = result
		}
	}

	return state.StatusResponse{
		State:            stateValue,
		Detail:           detail,
		ActiveTransport:  activeKind,
		Cloak:            cloakStatus,
		Naive:            naiveStatus,
		Reality:          realityStatus,
		Hysteria2:        hysteria2Status,
		Snowflake:        snowflakeStatus,
		WireGuard:        wgStatus,
		KillSwitchActive: s.killSwitch.Active(),
	}
}

func (s *Service) Logs(since int64) []state.LogEntry {
	return s.logs.Since(since)
}

func (s *Service) Config() state.Config {
	return s.config.Get()
}

func (s *Service) UpdateConfig(cfg state.Config) error {
	if err := s.config.Set(cfg); err != nil {
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "config updated")
	return nil
}

func (s *Service) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runHealthCheck(ctx)
		}
	}
}

func (s *Service) runHealthCheck(ctx context.Context) {
	currentState, _ := s.machine.Get()
	if currentState != state.StateConnected {
		return
	}

	profile, ok := s.getCurrentProfile()
	if !ok {
		s.setError("health check failed: missing active profile")
		return
	}

	s.activeMu.RLock()
	activeKind := s.activeTransportKind
	s.activeMu.RUnlock()

	transportRunning := false
	switch activeKind {
	case "cloak":
		transportRunning = s.cloakStatusForProfile(ctx, profile).Running
	case "naive":
		transportRunning = s.naive.Status().Running
	case "reality":
		transportRunning = s.reality.Status().Running
	case "hysteria2":
		transportRunning = s.hysteria2.Status().Running
	case "snowflake":
		transportRunning = s.snowflake.Status().Running
	}

	if !transportRunning {
		if err := s.recoverActiveTransport(ctx, profile, activeKind); err != nil {
			s.setError(fmt.Sprintf("health check failed: %s transport is not running and restart failed: %v", activeKind, err))
			return
		}
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("health check recovered %s transport", activeKind))
	}

	wgStatus, err := s.wg.Status(ctx, profile.WireGuard)
	if err != nil {
		s.setError(fmt.Sprintf("health check failed: wireguard status error: %v", err))
		return
	}
	if !wgStatus.Running {
		s.setError(fmt.Sprintf("health check failed: wireguard tunnel is down (%s)", wgStatus.Detail))
		return
	}
	if s.wireGuardHandshakeStale(wgStatus) {
		age := time.Since(time.Unix(wgStatus.LastHandshakeUnix, 0)).Round(time.Second)
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("wireguard tunnel is silent (no handshake for %s); rebuilding session", age))
		if err := s.rebuildSilentSession(ctx, profile); err != nil {
			// A Disconnect can land mid-rebuild and interrupt it; that's the
			// user getting what they asked for, not a session to mark failed.
			if st, _ := s.machine.Get(); st != state.StateDisconnecting && st != state.StateDisconnected {
				s.setError(fmt.Sprintf("health check failed: wireguard tunnel is silent (no handshake for %s) and rebuild failed: %v", age, err))
			}
		}
		return
	}

	if !s.killSwitch.Active() {
		s.setError("health check failed: kill switch was cleared unexpectedly")
		return
	}
}

// wireGuardHandshakeStaleAfter is how long a Connected tunnel may go without a
// completed handshake before it is treated as dead. WireGuard rekeys well
// inside this on a live link — REKEY_AFTER_TIME is 120s and a session is
// unusable after REJECT_AFTER_TIME (180s), so with keepalive traffic a fresh
// handshake always lands sooner. An older one means the peer is unreachable.
const wireGuardHandshakeStaleAfter = 180 * time.Second

// wireGuardHandshakeStale reports whether a Connected tunnel has gone silent:
// it handshaked at least once (so this isn't a still-connecting tunnel — that
// case is gated at connect time) but the newest handshake is older than
// wireGuardHandshakeStaleAfter.
func (s *Service) wireGuardHandshakeStale(status state.WireGuardStatus) bool {
	if status.LastHandshakeUnix <= 0 {
		return false
	}
	return time.Since(time.Unix(status.LastHandshakeUnix, 0)) > wireGuardHandshakeStaleAfter
}

// rebuildSilentSession restarts the transport and WireGuard in place after the
// tunnel has gone silent. The usual cause is a suspend/resume: the transport's
// session dies with the host's network (a Hysteria2 QUIC connection especially,
// which the server times out while the machine sleeps) while the transport
// itself stays up, so WireGuard keeps handshaking into a loopback socket whose
// far side leads nowhere. Only a fresh transport session recovers that, and no
// transport can see the breakage from the inside — its local writes still
// succeed — so a silent WireGuard is the signal to act on.
//
// The kill switch is deliberately left armed for the whole rebuild, so the
// device stays fail-closed while the tunnel is down. Same profile and same
// options as the live session: this restores what the user asked for, it does
// not renegotiate it.
func (s *Service) rebuildSilentSession(ctx context.Context, profile state.Profile) error {
	if !s.opMu.TryLock() {
		return errors.New("operation in progress")
	}
	defer s.opMu.Unlock()

	if currentState, _ := s.machine.Get(); currentState != state.StateConnected {
		return errors.New("state changed")
	}

	// Make the rebuild interruptible by Disconnect, like Connect and Switch —
	// the transport cascade can otherwise hold opMu for tens of seconds.
	rebuildCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.cancelMu.Lock()
	s.cancelConnect = cancel
	s.cancelMu.Unlock()
	defer func() {
		s.cancelMu.Lock()
		s.cancelConnect = nil
		s.cancelMu.Unlock()
	}()
	ctx = rebuildCtx

	// Bring up from the stored profile, not the live copy: bring-up mutates the
	// live copy's transport LocalPort to whatever port the bridge bound, which
	// no longer agrees with its own Endpoint line and would make the next
	// rebind think there is nothing to rewrite. The teardown below still uses
	// the live copy, since that's what names the tunnel actually running.
	live := profile
	if stored, found := s.config.FindProfile(profile.ID); found {
		profile = stored
	}

	opts := s.getSessionOpts()
	wireGuardProfile, err := wireGuardProfileFor(profile, opts.AllowLAN)
	if err != nil {
		return fmt.Errorf("allow-lan config transform failed: %w", err)
	}

	s.machine.Set(state.StateConnecting, "rebuilding silent tunnel")
	if err := s.wg.Stop(ctx, withTransportBypassHosts(live)); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("rebuild: wireguard stop warning: %v", err))
	}
	if active := s.activeTransport(); active != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 4*time.Second)
		if err := active.Stop(stopCtx); err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("rebuild: transport stop warning: %v", err))
		}
		stopCancel()
	}
	s.setActiveTransportKind("")

	if err := s.bringUpAfterKillSwitch(ctx, profile, wireGuardProfile, opts); err != nil {
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "silent tunnel rebuilt")
	return nil
}

// recoverActiveTransport restarts whichever transport is active in-place —
// v1 has no mid-session hot failover (design spec non-goal), so a dead
// cloak session gets a fresh cloak restart, a dead naive/reality session
// gets a fresh naive/reality restart; it never switches kind mid-session.
func (s *Service) recoverActiveTransport(ctx context.Context, profile state.Profile, activeKind string) error {
	if !s.opMu.TryLock() {
		return errors.New("operation in progress")
	}
	defer s.opMu.Unlock()

	currentState, _ := s.machine.Get()
	if currentState != state.StateConnected {
		return errors.New("state changed")
	}

	restartCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	switch activeKind {
	case "cloak":
		if s.cloak.Status().Running {
			return nil
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, "health check detected cloak stopped; attempting restart")
		if err := s.cloak.Start(restartCtx, profile.Cloak); err != nil {
			return err
		}
		return s.waitForManagedTransportStable(restartCtx, func() bool { return s.cloak.Status().Running }, profile.Cloak.LocalPort, 2*time.Second)
	case "naive":
		if s.naive.Status().Running {
			return nil
		}
		if profile.Naive == nil {
			return errors.New("active transport is naive but profile has no naive config")
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, "health check detected naive stopped; attempting restart")
		if err := s.naive.Start(restartCtx, *profile.Naive); err != nil {
			return err
		}
		return s.waitForManagedTransportStable(restartCtx, func() bool { return s.naive.Status().Running }, profile.Naive.LocalPort, 2*time.Second)
	case "reality":
		if s.reality.Status().Running {
			return nil
		}
		if profile.Reality == nil {
			return errors.New("active transport is reality but profile has no reality config")
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, "health check detected reality stopped; attempting restart")
		if err := s.reality.Start(restartCtx, *profile.Reality); err != nil {
			return err
		}
		return s.waitForManagedTransportStable(restartCtx, func() bool { return s.reality.Status().Running }, profile.Reality.LocalPort, 2*time.Second)
	case "hysteria2":
		if s.hysteria2.Status().Running {
			return nil
		}
		if profile.Hysteria2 == nil {
			return errors.New("active transport is hysteria2 but profile has no hysteria2 config")
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, "health check detected hysteria2 stopped; attempting restart")
		if err := s.hysteria2.Start(restartCtx, *profile.Hysteria2); err != nil {
			return err
		}
		return s.waitForManagedTransportStable(restartCtx, func() bool { return s.hysteria2.Status().Running }, profile.Hysteria2.LocalPort, 2*time.Second)
	case "snowflake":
		if s.snowflake.Status().Running {
			return nil
		}
		if profile.Snowflake == nil {
			return errors.New("active transport is snowflake but profile has no snowflake config")
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, "health check detected snowflake stopped; attempting restart")
		if err := s.snowflake.Start(restartCtx, *profile.Snowflake); err != nil {
			return err
		}
		return s.waitForManagedTransportStable(restartCtx, func() bool { return s.snowflake.Status().Running }, profile.Snowflake.LocalPort, 2*time.Second)
	default:
		return fmt.Errorf("unknown active transport kind: %q", activeKind)
	}
}

func (s *Service) resolveWireGuardInterfaceName(ctx context.Context, profile state.WireGuardProfile) string {
	fallback := strings.TrimSpace(profile.TunnelName)

	reporter, ok := s.wg.(wgActiveInterfaceReporter)
	if !ok {
		return fallback
	}

	interfaceName, err := reporter.ActiveInterfaceName(ctx, profile)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("wireguard interface lookup failed; falling back to configured tunnel name: %v", err))
		return fallback
	}

	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return fallback
	}

	return interfaceName
}

func (s *Service) setError(detail string) {
	s.machine.Set(state.StateError, detail)
	s.logs.Add(state.LogError, state.SourceDaemon, detail)
}

func (s *Service) reconcileStartup(ctx context.Context) {
	reconcileStart := time.Now()
	s.opMu.Lock()
	defer s.opMu.Unlock()
	defer func() {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("startup reconciliation completed in %dms", time.Since(reconcileStart).Milliseconds()))
	}()

	startupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	runningProfiles := s.findRunningWireGuardProfiles(startupCtx)

	// Reconcile a kill switch left from a previous session. A Lockdown lock
	// (state.Locked) is intentional and must stay fail-closed across daemon
	// restarts, so re-apply it when there's no tunnel. Anything else with no
	// tunnel is stale (e.g. a crash) and is cleared to restore networking.
	if len(runningProfiles) == 0 {
		persisted, _ := loadKillSwitchState()
		switch {
		case persisted.Active && persisted.Locked:
			if !s.killSwitch.Active() {
				// On Windows the dynamic WFP session was torn down on the prior
				// exit; on pf/nftables the rules may persist. Re-Enable is
				// idempotent and reuses the persisted endpoint IPs (no DNS).
				// The hub is topped up so a lock persisted without it (an older
				// daemon, or one engaged before anything was provisioned) comes
				// back reachable rather than leaving the app unable to bootstrap.
				endpoints := mergeUniqueSorted(persisted.EndpointIPs, ipLiterals(s.storedControlPlaneHosts()))
				s.logs.Add(state.LogInfo, state.SourceDaemon, "re-applying lockdown kill switch (no tunnel)")
				if err := s.killSwitch.Enable(startupCtx, endpoints, persisted.AllowLAN, true); err != nil {
					s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("lockdown kill switch re-apply failed: %v", err))
				}
			}
		case s.killSwitch.Active():
			s.logs.Add(state.LogInfo, state.SourceDaemon, "clearing stale kill switch from previous session")
			if err := s.killSwitch.Clear(startupCtx); err != nil {
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("stale kill switch clear failed: %v", err))
			}
		case persisted.Active:
			s.logs.Add(state.LogInfo, state.SourceDaemon, "clearing persisted kill switch state from previous session")
			if err := s.killSwitch.Clear(startupCtx); err != nil {
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("persisted kill switch clear failed: %v", err))
			}
		}
	}

	// Clean up stale tunnel adapters from previous sessions using native APIs.
	allTunnelNames := s.allConfiguredTunnelNames()
	if len(allTunnelNames) > 0 {
		activeLUIDs := s.wg.ActiveLUIDs()
		if actions, err := platform.CleanupStaleTunnelArtifactsNative(allTunnelNames, activeLUIDs); err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("startup stale tunnel cleanup failed: %v", err))
		} else {
			for _, action := range actions {
				s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("startup cleanup: %s", action))
			}
		}
	}

	if len(runningProfiles) == 0 {
		return
	}

	if len(runningProfiles) > 1 {
		names := make([]string, 0, len(runningProfiles))
		for _, profile := range runningProfiles {
			names = append(names, profile.WireGuard.TunnelName)
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("multiple running WireGuard tunnels detected on startup; choosing first: %s", strings.Join(names, ", ")))
	}

	active := runningProfiles[0]
	adopted, err := s.attachToRunningSession(startupCtx, active)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("startup tunnel recovery encountered an issue: %v", err))
		return
	}
	if adopted {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("recovered active tunnel on startup: %s", active.WireGuard.TunnelName))
	}
}

func (s *Service) allConfiguredTunnelNames() []string {
	cfg := s.config.Get()
	names := make([]string, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		if name := strings.TrimSpace(p.WireGuard.TunnelName); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func (s *Service) attachToRunningSession(ctx context.Context, profile state.Profile) (bool, error) {
	status, err := s.wg.Status(ctx, profile.WireGuard)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("attach preflight status check failed for %s: %v", profile.WireGuard.TunnelName, err))
		return false, nil
	}
	if !status.Running {
		return false, nil
	}

	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("adopting existing wireguard tunnel %s", profile.WireGuard.TunnelName))
	s.setCurrentProfile(profile)

	if !s.cloakStatusForProfile(ctx, profile).Running {
		s.machine.Set(state.StateConnecting, "restoring cloak for active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, "wireguard was already running; restoring cloak process")
		if err := s.cloak.Start(ctx, profile.Cloak); err != nil {
			return true, fmt.Errorf("wireguard tunnel is already running but cloak restore failed: %w", err)
		}
		if !s.cloak.Status().Running {
			return true, errors.New("wireguard tunnel is already running but cloak failed to stay running")
		}
	}

	// Reconciliation only ever restores Cloak (never NaiveProxy), so an
	// adopted session is always "cloak" for health-check/Status purposes.
	s.setActiveTransportKind("cloak")
	s.machine.Set(state.StateConnected, "recovered active tunnel")
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("adopted running tunnel %s", profile.WireGuard.TunnelName))
	return true, nil
}

func (s *Service) ensureNoOtherRunningWireGuard(ctx context.Context, requestedProfileID string) error {
	cfg := s.config.Get()
	for _, candidate := range cfg.Profiles {
		if candidate.ID == requestedProfileID {
			continue
		}
		status, err := s.wg.Status(ctx, candidate.WireGuard)
		if err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("wireguard preflight status check failed for %s: %v", candidate.WireGuard.TunnelName, err))
			continue
		}
		if status.Running {
			return fmt.Errorf("wireguard tunnel %s is already running; disconnect it before starting another profile", candidate.WireGuard.TunnelName)
		}
	}
	return nil
}

func (s *Service) ensureNoRunningWireGuard(ctx context.Context, profile state.Profile) error {
	status, err := s.wg.Status(ctx, profile.WireGuard)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("wireguard preflight status check failed: %v", err))
		return nil
	}
	if status.Running {
		return fmt.Errorf("wireguard tunnel %s is already running; disconnect it before starting a new session", profile.WireGuard.TunnelName)
	}
	return nil
}

func (s *Service) findRunningWireGuardProfiles(ctx context.Context) []state.Profile {
	cfg := s.config.Get()
	running := make([]state.Profile, 0, len(cfg.Profiles))

	for _, profile := range cfg.Profiles {
		status, err := s.wg.Status(ctx, profile.WireGuard)
		if err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("startup wireguard status check failed for %s: %v", profile.WireGuard.TunnelName, err))
			continue
		}
		if status.Running {
			running = append(running, profile)
		}
	}

	return running
}

func (s *Service) cloakStatusForProfile(ctx context.Context, profile state.Profile) state.CloakStatus {
	cloakStatus := s.cloak.Status()
	if cloakStatus.Running {
		return cloakStatus
	}

	if profile.Cloak.LocalPort <= 0 {
		return cloakStatus
	}

	owners, err := platform.UDPPortOwners(ctx, profile.Cloak.LocalPort, []int{os.Getpid()})
	if err != nil || len(owners) == 0 {
		return cloakStatus
	}

	pid := owners[0]
	cloakStatus.Running = true
	cloakStatus.PID = &pid
	return cloakStatus
}

func (s *Service) setCurrentProfile(profile state.Profile) {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()

	copyProfile := profile
	copyProfile.WireGuard.DNS = append([]string(nil), profile.WireGuard.DNS...)
	copyProfile.WireGuard.BypassHosts = append([]string(nil), profile.WireGuard.BypassHosts...)
	s.currentProfile = &copyProfile
}

func (s *Service) clearCurrentProfile() {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	s.currentProfile = nil
}

func (s *Service) setSessionOpts(opts ConnectOptions) {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	s.sessionOpts = opts
}

func (s *Service) getSessionOpts() ConnectOptions {
	s.profileMu.RLock()
	defer s.profileMu.RUnlock()
	return s.sessionOpts
}

func (s *Service) getCurrentProfile() (state.Profile, bool) {
	s.profileMu.RLock()
	defer s.profileMu.RUnlock()

	if s.currentProfile == nil {
		return state.Profile{}, false
	}

	copyProfile := *s.currentProfile
	copyProfile.WireGuard.DNS = append([]string(nil), s.currentProfile.WireGuard.DNS...)
	copyProfile.WireGuard.BypassHosts = append([]string(nil), s.currentProfile.WireGuard.BypassHosts...)
	return copyProfile, true
}

func validateProfile(profile state.Profile) error {
	if profile.ID == "" {
		return errors.New("profile id is required")
	}
	if profile.Cloak.LocalPort <= 0 {
		return errors.New("cloak.localPort must be > 0")
	}
	if profile.Cloak.RemoteHost == "" {
		return errors.New("cloak.remoteHost is required")
	}
	if profile.Cloak.RemotePort <= 0 {
		return errors.New("cloak.remotePort must be > 0")
	}
	if listenPort, ok := parseWireGuardListenPort(profile.WireGuard.ConfigText); ok && listenPort == profile.Cloak.LocalPort {
		return fmt.Errorf("wireguard listenport %d conflicts with cloak.localPort %d; remove ListenPort from client config or choose a different cloak.localPort", listenPort, profile.Cloak.LocalPort)
	}
	if profile.WireGuard.TunnelName == "" {
		return errors.New("wireguard.tunnelName is required")
	}
	return nil
}

func parseWireGuardListenPort(configText string) (int, bool) {
	matches := wgListenPortPattern.FindAllStringSubmatch(configText, -1)
	if len(matches) == 0 {
		return 0, false
	}

	last := matches[len(matches)-1]
	if len(last) < 2 {
		return 0, false
	}

	port, err := strconv.Atoi(strings.TrimSpace(last[1]))
	if err != nil || port <= 0 {
		return 0, false
	}
	return port, true
}

// rewriteLoopbackEndpointPort replaces the port in "Endpoint = 127.0.0.1:<n>"
// lines of a WireGuard config. Returns the rewritten text and whether any
// replacement was made.
func rewriteLoopbackEndpointPort(configText string, newPort int) (string, bool) {
	if !wgLoopbackEndpointPattern.MatchString(configText) {
		return configText, false
	}
	replacement := fmt.Sprintf("${1}%d${2}", newPort)
	return wgLoopbackEndpointPattern.ReplaceAllString(configText, replacement), true
}

// snowflakeHosts extracts the static hostnames Snowflake rendezvous touches
// directly: the broker (or its front domains, when domain fronting is
// configured), the AMP cache, and any STUN/TURN servers. Unlike the other
// transports' single fixed RemoteHost, these are rendezvous-only endpoints —
// once WebRTC negotiation completes, the actual data plane runs to a
// volunteer proxy peer whose address is discovered dynamically per-session
// and can't be known (or permitted by hostname) ahead of time. Kill-switch
// coverage here is therefore best-effort for the rendezvous phase only.
func snowflakeHosts(p *state.SnowflakeProfile) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, 2+len(p.FrontDomains)+len(p.ICEServers))
	if host := urlHost(p.BrokerURL); host != "" {
		out = append(out, host)
	}
	if host := urlHost(p.AmpCacheURL); host != "" {
		out = append(out, host)
	}
	for _, front := range p.FrontDomains {
		if front = strings.TrimSpace(front); front != "" {
			out = append(out, front)
		}
	}
	for _, ice := range p.ICEServers {
		if host := stunHost(ice); host != "" {
			out = append(out, host)
		}
	}
	return out
}

// urlHost extracts the hostname (no port) from a URL string, tolerating a
// bare hostname with no scheme.
func urlHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	return raw
}

// stunHost extracts the hostname from a "stun:host:port" / "turn:host:port"
// ICE server URL.
func stunHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if idx := strings.Index(raw, ":"); idx >= 0 {
		raw = raw[idx+1:]
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	return raw
}

// killSwitchPermits is the cloak, naive, reality, hysteria2, and snowflake
// endpoints (whichever are configured) plus any bypassHosts that need direct
// reachability (e.g. Pangea hub for re-provisioning during a switch). All
// configured transport endpoints must be permitted here — the kill switch
// arms once, before Connect knows which transport will actually succeed
// (bringUpAfterKillSwitch / startTransport runs after this), so permitting
// only Cloak's host would have the kill switch itself block a fallback or
// explicitly-selected transport's very first connection attempt whenever
// transports use different remote hosts (the normal case — see
// hub/config/nodes.json, where each transport's remoteHost is typically a
// distinct domain).
func killSwitchPermits(profile state.Profile) []string {
	out := make([]string, 0, 4+len(profile.WireGuard.BypassHosts))
	if host := strings.TrimSpace(profile.Cloak.RemoteHost); host != "" {
		out = append(out, host)
	}
	out = append(out, transportPermitHosts(profile)...)
	for _, h := range profile.WireGuard.BypassHosts {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// transportPermitHosts is where each non-Cloak transport can be reached.
//
// The hub's own addresses (TransportEndpointIPs) are used whenever it sent
// any, and then the node hostnames are left out entirely: resolving one costs
// a cleartext DNS query that tells the user's ISP exactly which node they are
// about to use, and behind an engaged Lockdown lock it cannot be answered at
// all — leaving every transport but Cloak blocked by our own kill switch.
//
// Hostnames remain the fallback for a profile the hub gave no addresses for
// (one provisioned by an older build, or hand-written).
func transportPermitHosts(profile state.Profile) []string {
	out := make([]string, 0, 4)
	for _, ip := range profile.TransportEndpointIPs {
		if ip = strings.TrimSpace(ip); ip != "" {
			out = append(out, ip)
		}
	}
	if len(out) > 0 {
		return out
	}
	if profile.Naive != nil {
		if host := strings.TrimSpace(profile.Naive.RemoteHost); host != "" {
			out = append(out, host)
		}
	}
	if profile.Reality != nil {
		if host := strings.TrimSpace(profile.Reality.RemoteHost); host != "" {
			out = append(out, host)
		}
	}
	if profile.Hysteria2 != nil {
		if host := strings.TrimSpace(profile.Hysteria2.RemoteHost); host != "" {
			out = append(out, host)
		}
	}
	return append(out, snowflakeHosts(profile.Snowflake)...)
}

// withTransportBypassHosts adds the cloak, naive, reality, hysteria2, and
// snowflake (whichever are configured) remote hosts to the WireGuard bypass
// list, so no transport's own connection to its remote endpoint gets routed
// back through the tunnel it's establishing — same "arm before the transport
// is chosen" reasoning as killSwitchPermits above.
func withTransportBypassHosts(profile state.Profile) state.WireGuardProfile {
	copyProfile := profile.WireGuard
	copyProfile.DNS = append([]string(nil), profile.WireGuard.DNS...)
	copyProfile.BypassHosts = append([]string(nil), profile.WireGuard.BypassHosts...)
	if host := strings.TrimSpace(profile.Cloak.RemoteHost); host != "" {
		copyProfile.BypassHosts = append(copyProfile.BypassHosts, host)
	}
	// Same source as the kill-switch permits, and for the same reason: the
	// hub's addresses when it sent any, never a node hostname we would have to
	// look up (see transportPermitHosts).
	copyProfile.BypassHosts = append(copyProfile.BypassHosts, transportPermitHosts(profile)...)
	return copyProfile
}
