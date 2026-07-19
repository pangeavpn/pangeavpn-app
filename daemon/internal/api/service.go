package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

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

type Service struct {
	machine    *state.Machine
	logs       *state.LogStore
	config     *state.ConfigStore
	cloak      cloakManager
	naive      naiveManager
	reality    realityManager
	hysteria2  hysteria2Manager
	wg         wg.Manager
	killSwitch platform.KillSwitch

	opMu sync.Mutex

	// cancelConnect aborts the in-flight Connect when set. Disconnect uses
	// it to interrupt a connect that's still inside WaitForSession (up to
	// the 10s cloak timeout) so the user can bail without waiting for the
	// timeout to fire.
	cancelMu      sync.Mutex
	cancelConnect context.CancelFunc

	profileMu      sync.RWMutex
	currentProfile *state.Profile

	// activeMu guards activeTransportKind, which of {cloak, naive, reality,
	// hysteria2} is live for the current session. Empty string when disconnected.
	activeMu            sync.RWMutex
	activeTransportKind string
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
	wgManager wg.Manager,
	killSwitch platform.KillSwitch,
) *Service {
	return &Service{
		machine:    machine,
		logs:       logs,
		config:     config,
		cloak:      cloakManager,
		naive:      naiveManager,
		reality:    realityManager,
		hysteria2:  hysteria2Manager,
		wg:         wgManager,
		killSwitch: killSwitch,
	}
}

// activeTransport returns whichever manager is live for the current session,
// or nil if disconnected. Most call sites (health check, Disconnect, Status)
// use this instead of branching on transport kind.
func (s *Service) activeTransport() transport.Manager {
	s.activeMu.RLock()
	defer s.activeMu.RUnlock()
	switch s.activeTransportKind {
	case "cloak":
		return s.cloak
	case "naive":
		return s.naive
	case "reality":
		return s.reality
	case "hysteria2":
		return s.hysteria2
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

	// "cloak", "naive", "hysteria2", or "" (default: cloak first, fall back
	// to naive).
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

	adopted, err := s.attachToRunningSession(ctx, profile)
	if err != nil {
		s.setError(err.Error())
		return err
	}
	if adopted {
		return nil
	}

	wireGuardProfile := withTransportBypassHosts(profile)
	if opts.AllowLAN {
		rewritten, err := wg.TransformWGConfigExcludeLAN(wireGuardProfile.ConfigText)
		if err != nil {
			s.setError(fmt.Sprintf("allow-lan config transform failed: %v", err))
			return err
		}
		wireGuardProfile.ConfigText = rewritten
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

	if err := s.bringUpAfterKillSwitch(ctx, profile, wireGuardProfile, opts.PreferredTransport); err != nil {
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "connect flow completed")
	return nil
}

// bringUpAfterKillSwitch starts the transport + WireGuard and updates the
// kill switch. Assumes opMu held, kill switch already Enable()d. Shared by
// Connect and Switch.
func (s *Service) bringUpAfterKillSwitch(ctx context.Context, profile state.Profile, wireGuardProfile state.WireGuardProfile, preferredTransport string) error {
	s.machine.Set(state.StateConnecting, "starting transport")
	stepStart := time.Now()

	kind, err := s.startTransport(ctx, &profile, &wireGuardProfile, preferredTransport)
	if err != nil {
		s.setError(fmt.Sprintf("transport start failed: %v", err))
		return err
	}
	s.setActiveTransportKind(kind)
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("%s transport started (%dms)", kind, time.Since(stepStart).Milliseconds()))

	s.machine.Set(state.StateConnecting, "starting wireguard")
	stepStart = time.Now()
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("starting wireguard tunnel=%s", wireGuardProfile.TunnelName))
	if err := s.wg.Start(ctx, wireGuardProfile); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = s.activeTransport().Stop(cleanupCtx)
		s.setActiveTransportKind("")
		s.setError(fmt.Sprintf("wireguard start failed: %v", err))
		return err
	}

	wgStatus, err := s.wg.Status(ctx, wireGuardProfile)
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = s.wg.Stop(cleanupCtx, wireGuardProfile)
		_ = s.activeTransport().Stop(cleanupCtx)
		s.setActiveTransportKind("")
		s.setError(fmt.Sprintf("wireguard status failed: %v", err))
		return err
	}
	if !wgStatus.Running {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = s.wg.Stop(cleanupCtx, wireGuardProfile)
		_ = s.activeTransport().Stop(cleanupCtx)
		s.setActiveTransportKind("")
		s.setError(fmt.Sprintf("wireguard failed to reach running state: %s", wgStatus.Detail))
		return errors.New("wireguard not running")
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("wireguard started (%dms)", time.Since(stepStart).Milliseconds()))

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
	if waiter, ok := s.cloak.(transport.SessionWaiter); ok {
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := waiter.WaitForSession(waitCtx, 10*time.Second)
		cancel()
		if err != nil {
			return err
		}
	}
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

// startTransport: "cloak"/"naive"/"reality"/"hysteria2" = that transport
// only, no fallback (errors if the profile has no config for it). ""
// (default) = cloak first, then naive, then reality.
func (s *Service) startTransport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile, preferredTransport string) (string, error) {
	switch preferredTransport {
	case "naive":
		if profile.Naive == nil {
			return "", errors.New("naive transport requested but this profile has no naive configuration")
		}
		if err := s.startNaiveTransport(ctx, profile, wireGuardProfile); err != nil {
			return "", fmt.Errorf("naive: %w", err)
		}
		return "naive", nil
	case "reality":
		if profile.Reality == nil {
			return "", errors.New("reality transport requested but this profile has no reality configuration")
		}
		if err := s.startRealityTransport(ctx, profile, wireGuardProfile); err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.reality.Stop(cleanupCtx)
			cleanupCancel()
			return "", fmt.Errorf("reality: %w", err)
		}
		return "reality", nil
	case "hysteria2":
		if profile.Hysteria2 == nil {
			return "", errors.New("hysteria2 transport requested but this profile has no hysteria2 configuration")
		}
		if err := s.startHysteria2Transport(ctx, profile, wireGuardProfile); err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.hysteria2.Stop(cleanupCtx)
			cleanupCancel()
			return "", fmt.Errorf("hysteria2: %w", err)
		}
		return "hysteria2", nil
	case "cloak":
		if err := s.startCloakTransport(ctx, profile, wireGuardProfile); err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.cloak.Stop(cleanupCtx)
			cleanupCancel()
			return "", fmt.Errorf("cloak: %w", err)
		}
		return "cloak", nil
	default:
		if err := s.startCloakTransport(ctx, profile, wireGuardProfile); err != nil {
			return s.fallbackToNaive(ctx, profile, wireGuardProfile, err)
		}
		return "cloak", nil
	}
}

// fallbackToNaive is called once Cloak has failed at some stage of startup
// during automatic ("" / "auto") mode. It stops whatever Cloak left
// behind, and — only if the profile has a NaiveProxy fallback configured —
// starts NaiveProxy in its place. Falls through to fallbackToReality if
// naive isn't configured or also fails.
func (s *Service) fallbackToNaive(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile, cloakErr error) (string, error) {
	s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("cloak transport failed, checking naive fallback: %v", cloakErr))
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = s.cloak.Stop(cleanupCtx)
	cleanupCancel()

	if profile.Naive == nil {
		return s.fallbackToReality(ctx, profile, wireGuardProfile, cloakErr)
	}

	if err := s.startNaiveTransport(ctx, profile, wireGuardProfile); err != nil {
		return s.fallbackToReality(ctx, profile, wireGuardProfile, fmt.Errorf("cloak: %v; naive: %w", cloakErr, err))
	}

	s.logs.Add(state.LogInfo, state.SourceDaemon, "fell back to naive transport after cloak failure")
	return "naive", nil
}

// fallbackToReality is the last-resort step in automatic mode, tried after
// cloak (and naive, if configured) have failed. prevErr carries the
// accumulated failure detail from earlier transports.
func (s *Service) fallbackToReality(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile, prevErr error) (string, error) {
	if profile.Reality == nil {
		return "", fmt.Errorf("all configured transports failed: %w", prevErr)
	}

	s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("checking reality fallback: %v", prevErr))
	if err := s.startRealityTransport(ctx, profile, wireGuardProfile); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.reality.Stop(cleanupCtx)
		cleanupCancel()
		return "", fmt.Errorf("all transports failed: %v; reality: %w", prevErr, err)
	}

	s.logs.Add(state.LogInfo, state.SourceDaemon, "fell back to reality transport")
	return "reality", nil
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

	wireGuardProfile := withTransportBypassHosts(newProfile)
	if opts.AllowLAN {
		rewritten, err := wg.TransformWGConfigExcludeLAN(wireGuardProfile.ConfigText)
		if err != nil {
			s.setError(fmt.Sprintf("switch: allow-lan config transform failed: %v", err))
			return err
		}
		wireGuardProfile.ConfigText = rewritten
	}
	if checker, ok := s.wg.(wgPreflightChecker); ok {
		if err := checker.Preflight(ctx, wireGuardProfile); err != nil {
			s.setError(fmt.Sprintf("switch: wireguard preflight failed: %v", err))
			return err
		}
	}

	s.machine.Set(state.StateConnecting, fmt.Sprintf("switching to %s: updating kill switch", newProfile.ID))
	stepStart := time.Now()
	if err := s.killSwitch.Enable(ctx, killSwitchPermits(newProfile), opts.AllowLAN, opts.Lockdown); err != nil {
		s.setError(fmt.Sprintf("switch: kill switch re-enable failed: %v", err))
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch re-enabled for %s (%dms)", newProfile.ID, time.Since(stepStart).Milliseconds()))

	if err := s.bringUpAfterKillSwitch(ctx, newProfile, wireGuardProfile, opts.PreferredTransport); err != nil {
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

// EngageKillSwitch turns on the kill switch without starting a VPN session,
// giving the device a fail-closed network lock. Used when Lockdown is enabled
// while disconnected: internet is blocked immediately even though nothing is
// connected yet. When profileID names a known profile, that profile's transport
// endpoints are permitted so a later Connect is seamless; otherwise the lock
// blocks all outbound except loopback/DHCP.
func (s *Service) EngageKillSwitch(ctx context.Context, profileID string, allowLAN bool) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.killSwitch.Active() {
		return nil
	}

	// Pure block-all lock: no endpoint permits, so this lands instantly with no
	// DNS resolution — which would otherwise delay the block (leaking until it
	// lands) and can hang the request. The VPN endpoint is permitted later by
	// Connect, which re-enters Enable.
	_ = profileID
	ksCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.killSwitch.Enable(ksCtx, nil, allowLAN, true); err != nil {
		s.logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("kill switch engage failed: %v", err))
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "kill switch engaged (lockdown, no connection)")
	return nil
}

// Disconnect tears down the active VPN session. When keepKillSwitch is true
// (Lockdown mode), the firewall rules stay engaged so the device has no
// internet until the caller explicitly clears the kill switch.
func (s *Service) Disconnect(ctx context.Context, keepKillSwitch bool) error {
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

	if !s.killSwitch.Active() {
		s.setError("health check failed: kill switch was cleared unexpectedly")
		return
	}
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
		persisted, _ := platform.LoadKillSwitchStatePublic()
		switch {
		case persisted.Active && persisted.Locked:
			if !s.killSwitch.Active() {
				// On Windows the dynamic WFP session was torn down on the prior
				// exit; on pf/nftables the rules may persist. Re-Enable is
				// idempotent and reuses the persisted endpoint IPs (no DNS).
				s.logs.Add(state.LogInfo, state.SourceDaemon, "re-applying lockdown kill switch (no tunnel)")
				if err := s.killSwitch.Enable(startupCtx, persisted.EndpointIPs, persisted.AllowLAN, true); err != nil {
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

// killSwitchPermits is the cloak, naive, reality, and hysteria2 endpoints
// (whichever are configured) plus any bypassHosts that need direct
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
	for _, h := range profile.WireGuard.BypassHosts {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// withTransportBypassHosts adds the cloak, naive, reality, and hysteria2
// (whichever are configured) remote hosts to the WireGuard bypass list, so no
// transport's own connection to its remote endpoint gets routed back through
// the tunnel it's establishing — same "arm before the transport is chosen"
// reasoning as killSwitchPermits above.
func withTransportBypassHosts(profile state.Profile) state.WireGuardProfile {
	copyProfile := profile.WireGuard
	copyProfile.DNS = append([]string(nil), profile.WireGuard.DNS...)
	copyProfile.BypassHosts = append([]string(nil), profile.WireGuard.BypassHosts...)
	if host := strings.TrimSpace(profile.Cloak.RemoteHost); host != "" {
		copyProfile.BypassHosts = append(copyProfile.BypassHosts, host)
	}
	if profile.Naive != nil {
		if host := strings.TrimSpace(profile.Naive.RemoteHost); host != "" {
			copyProfile.BypassHosts = append(copyProfile.BypassHosts, host)
		}
	}
	if profile.Reality != nil {
		if host := strings.TrimSpace(profile.Reality.RemoteHost); host != "" {
			copyProfile.BypassHosts = append(copyProfile.BypassHosts, host)
		}
	}
	if profile.Hysteria2 != nil {
		if host := strings.TrimSpace(profile.Hysteria2.RemoteHost); host != "" {
			copyProfile.BypassHosts = append(copyProfile.BypassHosts, host)
		}
	}
	return copyProfile
}
