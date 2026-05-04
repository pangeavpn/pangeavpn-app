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

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

type Service struct {
	machine    *state.Machine
	logs       *state.LogStore
	config     *state.ConfigStore
	cloak      cloak.Manager
	wg         wg.Manager
	killSwitch platform.KillSwitch

	opMu sync.Mutex

	profileMu      sync.RWMutex
	currentProfile *state.Profile
}

type wgPreflightChecker interface {
	Preflight(ctx context.Context, profile state.WireGuardProfile) error
}

type cloakSessionWaiter interface {
	WaitForSession(ctx context.Context, timeout time.Duration) error
}

type cloakBoundPortReporter interface {
	BoundLocalPort() int
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
	cloakManager cloak.Manager,
	wgManager wg.Manager,
	killSwitch platform.KillSwitch,
) *Service {
	return &Service{
		machine:    machine,
		logs:       logs,
		config:     config,
		cloak:      cloakManager,
		wg:         wgManager,
		killSwitch: killSwitch,
	}
}

func (s *Service) StartBackground(ctx context.Context) {
	s.reconcileStartup(ctx)
	go s.healthLoop(ctx)
}

// ConnectOptions carries per-connect toggles from the client. Defaults to
// strict behavior (no LAN bypass) when zero-valued.
type ConnectOptions struct {
	// AllowLAN permits local-network IPv4 ranges both at the kill switch
	// and in the WireGuard AllowedIPs, so captive-portal re-checks and
	// gateway liveness probes work on restrictive WiFi.
	AllowLAN bool
}

func (s *Service) Connect(ctx context.Context, profileID string, opts ConnectOptions) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

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

	wireGuardProfile := withCloakBypassHost(profile)
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

	// Step 1: Enable kill switch before any network activity.
	s.machine.Set(state.StateConnecting, "enabling kill switch")
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("connect requested with profile %s", profile.ID))
	stepStart := time.Now()
	if err := s.killSwitch.Enable(ctx, profile.Cloak.RemoteHost, opts.AllowLAN); err != nil {
		s.setError(fmt.Sprintf("kill switch enable failed: %v", err))
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch enabled (%dms)", time.Since(stepStart).Milliseconds()))

	// Step 2: Start Cloak. On failure, kill switch stays active (fail-closed).
	s.machine.Set(state.StateConnecting, "starting cloak")
	stepStart = time.Now()
	if !s.cloak.Status().Running {
		// Ask cloak to bind an ephemeral loopback UDP port instead of the
		// client-supplied default. The port is purely internal glue between
		// cloak and wireguard-go, and requesting 0 lets the OS avoid
		// reserved ranges (e.g. Windows Hyper-V UDP exclusions) that can
		// claim the default 51820 after a reboot.
		cloakStartProfile := profile.Cloak
		cloakStartProfile.LocalPort = 0
		if err := s.cloak.Start(ctx, cloakStartProfile); err != nil {
			s.setError(fmt.Sprintf("cloak start failed: %v", err))
			return err
		}
	}

	if reporter, ok := s.cloak.(cloakBoundPortReporter); ok {
		if boundPort := reporter.BoundLocalPort(); boundPort > 0 && boundPort != profile.Cloak.LocalPort {
			rewritten, replaced := rewriteLoopbackEndpointPort(wireGuardProfile.ConfigText, boundPort)
			if replaced {
				s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("cloak bound loopback port %d; rewrote wireguard peer endpoint", boundPort))
				wireGuardProfile.ConfigText = rewritten
				profile.Cloak.LocalPort = boundPort
			} else {
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("cloak bound loopback port %d but no matching Endpoint=127.0.0.1 line found in wireguard config; connection may fail", boundPort))
			}
		}
	}

	if err := s.waitForManagedCloakStable(ctx, profile.Cloak.LocalPort, 200*time.Millisecond); err != nil {
		s.setError(err.Error())
		return err
	}
	if cloakStatus := s.cloak.Status(); !cloakStatus.Running {
		err := errors.New("cloak process exited during startup; local port is already occupied by another process")
		s.setError(err.Error())
		return err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("cloak started (%dms)", time.Since(stepStart).Milliseconds()))

	// Step 3: Start WireGuard. On failure, kill switch stays active (fail-closed).
	s.machine.Set(state.StateConnecting, "starting wireguard")
	stepStart = time.Now()
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("starting wireguard tunnel=%s", wireGuardProfile.TunnelName))
	if err := s.wg.Start(ctx, wireGuardProfile); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = s.cloak.Stop(cleanupCtx)
		s.setError(fmt.Sprintf("wireguard start failed: %v", err))
		return err
	}

	wgStatus, err := s.wg.Status(ctx, wireGuardProfile)
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = s.wg.Stop(cleanupCtx, wireGuardProfile)
		_ = s.cloak.Stop(cleanupCtx)
		s.setError(fmt.Sprintf("wireguard status failed: %v", err))
		return err
	}
	if !wgStatus.Running {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = s.wg.Stop(cleanupCtx, wireGuardProfile)
		_ = s.cloak.Stop(cleanupCtx)
		s.setError(fmt.Sprintf("wireguard failed to reach running state: %s", wgStatus.Detail))
		return errors.New("wireguard not running")
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("wireguard started (%dms)", time.Since(stepStart).Milliseconds()))

	// Step 4: Wait for Cloak session and update kill switch concurrently.
	stepStart = time.Now()
	tunnelInterface := s.resolveWireGuardInterfaceName(ctx, wireGuardProfile)
	updateCh := make(chan error, 1)
	go func() {
		updateCh <- s.killSwitch.Update(ctx, tunnelInterface)
	}()

	if waiter, ok := s.cloak.(cloakSessionWaiter); ok {
		waitCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		if err := waiter.WaitForSession(waitCtx, 5*time.Second); err != nil {
			<-updateCh
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cleanupCancel()
			_ = s.wg.Stop(cleanupCtx, wireGuardProfile)
			_ = s.cloak.Stop(cleanupCtx)
			s.setError(fmt.Sprintf("cloak session establishment failed: %v", err))
			return err
		}
	}

	if updateErr := <-updateCh; updateErr != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("kill switch tunnel update failed: %v", updateErr))
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("session established + ks update (%dms)", time.Since(stepStart).Milliseconds()))

	s.setCurrentProfile(profile)
	s.machine.Set(state.StateConnected, "tunnel active")
	s.logs.Add(state.LogInfo, state.SourceDaemon, "connect flow completed")

	return nil
}

func (s *Service) Disconnect(ctx context.Context) error {
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
		if err := s.wg.Stop(ctx, withCloakBypassHost(runningProfile)); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("wireguard stop failed for %s: %v", runningProfile.WireGuard.TunnelName, err))
			s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
		}
	}

	s.machine.Set(state.StateDisconnecting, "verifying wireguard")
	for _, runningProfile := range profilesToStop {
		status, err := s.wg.Status(ctx, withCloakBypassHost(runningProfile))
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

	s.machine.Set(state.StateDisconnecting, "stopping cloak")
	// Use a short timeout for cloak stop — don't let it block disconnect.
	cloakCtx, cloakCancel := context.WithTimeout(context.Background(), 4*time.Second)
	if err := s.cloak.Stop(cloakCtx); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("cloak stop failed: %v", err))
		s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
	}
	cloakCancel()

	// Always clear profile and kill switch regardless of earlier errors.
	s.clearCurrentProfile()

	if s.killSwitch.Active() {
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
	cloakStatus := s.cloak.Status()

	wgStatus := state.WireGuardStatus{Running: false, Detail: "not connected"}
	if profile, ok := s.getCurrentProfile(); ok {
		cloakStatus = s.cloakStatusForProfile(ctx, profile)

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
		Cloak:            cloakStatus,
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

	cloakStatus := s.cloakStatusForProfile(ctx, profile)
	if !cloakStatus.Running {
		if err := s.recoverCloak(ctx, profile); err != nil {
			s.setError(fmt.Sprintf("health check failed: cloak process is not running and restart failed: %v", err))
			return
		}
		s.logs.Add(state.LogInfo, state.SourceDaemon, "health check recovered cloak process")
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

func (s *Service) recoverCloak(ctx context.Context, profile state.Profile) error {
	if !s.opMu.TryLock() {
		return errors.New("operation in progress")
	}
	defer s.opMu.Unlock()

	currentState, _ := s.machine.Get()
	if currentState != state.StateConnected {
		return errors.New("state changed")
	}
	if s.cloak.Status().Running {
		return nil
	}

	s.logs.Add(state.LogWarn, state.SourceDaemon, "health check detected cloak stopped; attempting restart")

	restartCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.cloak.Start(restartCtx, profile.Cloak); err != nil {
		return err
	}

	return s.waitForManagedCloakStable(restartCtx, profile.Cloak.LocalPort, 2*time.Second)
}

func (s *Service) waitForManagedCloakStable(ctx context.Context, localPort int, duration time.Duration) error {
	// Fast path: in-process Cloak is stable immediately after Start() succeeds.
	if s.cloak.Status().Running {
		return nil
	}

	// Slow path: brief poll in case of startup race.
	deadline := time.Now().Add(duration)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}

		if s.cloak.Status().Running {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
	}

	if localPort > 0 {
		if owners, err := platform.UDPPortOwners(ctx, localPort, []int{os.Getpid()}); err == nil && len(owners) > 0 {
			return fmt.Errorf("cloak process exited during startup; local port %d is already occupied by pid %d", localPort, owners[0])
		}
		return fmt.Errorf("cloak process exited during startup; local port %d is already occupied by another process", localPort)
	}
	return errors.New("cloak process exited during startup")
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

	// If kill switch was left active from a previous session but there are
	// no running tunnels, clear it to restore normal networking.
	if s.killSwitch.Active() && len(runningProfiles) == 0 {
		s.logs.Add(state.LogInfo, state.SourceDaemon, "clearing stale kill switch from previous session")
		if err := s.killSwitch.Clear(startupCtx); err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("stale kill switch clear failed: %v", err))
		}
	}
	// Also check persisted state in case in-memory state was lost (fresh start).
	if !s.killSwitch.Active() && len(runningProfiles) == 0 {
		if st, err := platform.LoadKillSwitchStatePublic(); err == nil && st.Active {
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

func withCloakBypassHost(profile state.Profile) state.WireGuardProfile {
	copyProfile := profile.WireGuard
	copyProfile.DNS = append([]string(nil), profile.WireGuard.DNS...)
	copyProfile.BypassHosts = append([]string(nil), profile.WireGuard.BypassHosts...)
	if host := strings.TrimSpace(profile.Cloak.RemoteHost); host != "" {
		copyProfile.BypassHosts = append(copyProfile.BypassHosts, host)
	}
	return copyProfile
}

func tunnelNamesForCleanup(cfg state.Config, current state.Profile) []string {
	seen := make(map[string]struct{}, len(cfg.Profiles)+1)
	names := make([]string, 0, len(cfg.Profiles)+1)

	add := func(name string) {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		names = append(names, trimmed)
	}

	add(current.WireGuard.TunnelName)
	for _, profile := range cfg.Profiles {
		add(profile.WireGuard.TunnelName)
	}

	return names
}
