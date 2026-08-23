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

// errRebuildBusy signals rebuildSilentSession found opMu already held by a
// real operation (Connect/Switch, or an overlapping rebuild). It is not a
// failed rebuild — attemptSessionRebuild must not book it against the retry
// backoff or stamp an error over whatever that other operation is doing.
var errRebuildBusy = errors.New("operation in progress")

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

// shadowsocksManager is transport.Manager (Stop) plus Start/Status with
// Shadowsocks's concrete types. shadowsocks.Manager satisfies this directly.
type shadowsocksManager interface {
	transport.Manager
	Start(ctx context.Context, profile state.ShadowsocksProfile) error
	Status() state.TransportStatus
}

// shadowsocksProxyManager carries hub control-plane traffic: no WireGuard, no
// activeTransport, runs before a profile exists. shadowsocks.ProxyManager fits.
type shadowsocksProxyManager interface {
	Start(ctx context.Context, profile state.ShadowsocksProfile) (int, error)
	Stop(ctx context.Context) error
	Port() int
	// Credentials returns the Basic Auth username/password required to use
	// the live proxy port; both empty when stopped.
	Credentials() (string, string)
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
	Clear() error
}

type Service struct {
	machine     *state.Machine
	logs        *state.LogStore
	config      *state.ConfigStore
	cloak       cloakManager
	naive       naiveManager
	reality     realityManager
	hysteria2   hysteria2Manager
	shadowsocks shadowsocksManager
	snowflake   snowflakeManager
	wg          wg.Manager
	killSwitch  platform.KillSwitch

	// transportMemory remembers the last-good transport per network; nil
	// disables the reorder/record optimization. networkKey fingerprints the
	// current network. Both are set once and read-only thereafter.
	transportMemory transportMemory
	networkKey      func() string

	// shadowsocksProxy serves the hub control plane; nil leaves /ssproxy/*
	// reporting unavailable. Set once at startup.
	shadowsocksProxy shadowsocksProxyManager

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
	// hysteria2, shadowsocks, snowflake} is live for the current session.
	// Empty string when disconnected.
	activeMu            sync.RWMutex
	activeTransportKind string
	// connectingTransportKind is the cascade candidate currently being tried,
	// "" outside a bring-up. Also guarded by activeMu.
	connectingTransportKind string

	// recoveryMu guards the reconnect schedule for a session that dropped on
	// its own: how many rebuilds have been tried, when the next one is due, and
	// how long health checks are held off after a host resume.
	recoveryMu       sync.Mutex
	recoveryAttempts int
	recoveryNextAt   time.Time
	healthHoldUntil  time.Time

	// dnsProbe* schedule the end-to-end data-path check: when the next round is
	// due, how many consecutive rounds have failed, and the earliest a
	// probe-driven rebuild may fire again. Guarded by recoveryMu.
	dnsProbeNextAt     time.Time
	dnsProbeFailures   int
	dnsProbeQuietUntil time.Time

	// dnsGuardNextAt is the earliest the DNS guard may run again. Zero means
	// every health tick, which is the normal state; it is pushed out only after
	// a correction. Guarded by recoveryMu.
	dnsGuardNextAt time.Time

	// demotedTransport is sent to the back of the next cascade; transportsExhausted
	// says the cascade ran out here, the app's cue to try another server.
	demotedTransport    string
	transportsExhausted bool

	// endpointRouteRepairs counts consecutive health checks that had to re-pin
	// the tunnel's endpoint routes, so a route that never settles cannot hold
	// off recovery forever. Guarded by recoveryMu.
	endpointRouteRepairs int

	// probeResolver resolves over the live tunnel to prove it still carries
	// traffic. Defaults to probeResolverOverUDP; tests stub it, and a nil value
	// disables the check.
	probeResolver func(ctx context.Context, tunnelInterface, server string) error

	// recoveryDelays is the backoff between reconnect attempts; the last entry
	// repeats for every attempt beyond it. Tests shorten it.
	recoveryDelays []time.Duration

	// networkRepair is the post-disconnect route/DNS cleanup, injectable so
	// tests don't spawn real netsh/powershell. Runs in the background.
	networkRepair func(ctx context.Context, tunnelNames []string) ([]string, error)

	// repairCancel aborts the in-flight background repair; a new connect must
	// not race adapter renews under its fresh tunnel. Guarded by repairMu.
	repairMu     sync.Mutex
	repairCancel context.CancelFunc

	// repairNames and repairSeq let a cancelled repair be restarted if the
	// connect that interrupted it fails. Guarded by repairMu.
	repairNames []string
	repairSeq   int

	// handshakeTimeout bounds how long a single transport is given to carry a
	// first WireGuard handshake during bring-up. Defaults to
	// defaultWireGuardHandshakeTimeout; tests set it small.
	handshakeTimeout time.Duration

	// cloakStartedFor is the remote startCloakTransport last started Cloak
	// against, so a live cloak.Status().Running is only trusted as "already
	// bridging this server" when it actually is. Guarded by cloakMu.
	cloakMu         sync.Mutex
	cloakStartedFor state.CloakProfile
}

type wgPreflightChecker interface {
	Preflight(ctx context.Context, profile state.WireGuardProfile) error
}

// wgInPlaceSwitcher marks a manager whose live device Start can re-point at a
// new server (Windows); Switch then keeps the adapter instead of rebuilding it.
type wgInPlaceSwitcher interface {
	PinEndpointRoutes(ctx context.Context, profile state.WireGuardProfile) error
}

type wgActiveInterfaceReporter interface {
	ActiveInterfaceName(ctx context.Context, profile state.WireGuardProfile) (string, error)
}

// wgTunnelLUIDReporter reports the Windows interface LUID of the live tunnel
// device, which identifies the adapter exactly where its name does not.
// Optional: a manager that does not implement it leaves the kill switch to
// resolve the name itself.
type wgTunnelLUIDReporter interface {
	ActiveTunnelLUID(ctx context.Context, profile state.WireGuardProfile) (uint64, error)
}

// wgRouteGuard re-pins the tunnel's endpoint bypass routes when the host has
// moved or dropped the default route they hang off, reporting whether it
// corrected anything. Optional: a manager that does not implement it leaves the
// routes as bring-up installed them.
type wgRouteGuard interface {
	EnsureEndpointRoutes(ctx context.Context, profile state.WireGuardProfile) (bool, error)
}

// wgDNSGuard re-asserts the tunnel's resolvers when the host has stopped
// pointing at them, reporting whether it corrected anything. Optional: a manager
// that does not implement it leaves host DNS alone after bring-up.
type wgDNSGuard interface {
	EnsureDNS(ctx context.Context, profile state.WireGuardProfile) (bool, error)
}

var wgListenPortPattern = regexp.MustCompile(`(?im)^\s*ListenPort\s*=\s*(\d+)\s*$`)

// wgLoopbackEndpointPattern matches "Endpoint = 127.0.0.1:<port>" lines in a
// WireGuard config's [Peer] section. We rewrite the port when cloak's
// loopback UDP socket binds to an ephemeral port instead of the default.
var wgLoopbackEndpointPattern = regexp.MustCompile(`(?im)^(\s*Endpoint\s*=\s*127\.0\.0\.1:)\d+(\s*)$`)

// wgEndpointPattern matches an "Endpoint = <host:port>" line whatever it points
// at, for repointing the tunnel at the node itself (see startDirectWireGuard).
var wgEndpointPattern = regexp.MustCompile(`(?im)^(\s*Endpoint\s*=\s*)\S+(\s*)$`)

// wgMTUPattern matches the "MTU = N" line in a WireGuard config's [Interface].
var wgMTUPattern = regexp.MustCompile(`(?im)^(\s*MTU\s*=\s*)(\d+)(\s*)$`)

// shadowsocksMaxMTU caps the tunnel MTU while Shadowsocks carries it. SS wraps
// each datagram in a salt, address header and AEAD tag (~55 B for
// chacha20-ietf-poly1305, more for SS-2022), on top of WireGuard's own 32 B.
// At the 1380 default that lands within a few bytes of a 1500 B path, and any
// link below it (PPPoE at 1492, another tunnel) makes the OS refuse the send
// outright — EMSGSIZE on Windows — as soon as a full-size packet appears. The
// handshake is small enough to succeed first, so it fails only once traffic
// flows. 1280 is IPv6's guaranteed minimum, and this is the cascade's last
// resort: reaching the node at all beats a wider MTU that sometimes cannot.
const shadowsocksMaxMTU = 1280

// clampWireGuardMTU lowers an existing MTU line to max, leaving a lower one
// alone. Returns the text unchanged when there is no MTU line to rewrite.
func clampWireGuardMTU(configText string, max int) string {
	return wgMTUPattern.ReplaceAllStringFunc(configText, func(line string) string {
		groups := wgMTUPattern.FindStringSubmatch(line)
		if len(groups) != 4 {
			return line
		}
		current, err := strconv.Atoi(groups[2])
		if err != nil || current <= max {
			return line
		}
		return groups[1] + strconv.Itoa(max) + groups[3]
	})
}

func NewService(
	machine *state.Machine,
	logs *state.LogStore,
	config *state.ConfigStore,
	cloakManager cloakManager,
	naiveManager naiveManager,
	realityManager realityManager,
	hysteria2Manager hysteria2Manager,
	shadowsocksManager shadowsocksManager,
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
		shadowsocks:      shadowsocksManager,
		snowflake:        snowflakeManager,
		wg:               wgManager,
		killSwitch:       killSwitch,
		handshakeTimeout: defaultWireGuardHandshakeTimeout,
		networkKey:       currentNetworkKey,
		recoveryDelays:   defaultRecoveryDelays,
		probeResolver:    probeResolverOverUDP,
		networkRepair:    platform.RepairNetworkAfterTunnelDisconnect,
	}
}

// SetTransportMemory wires in the per-network last-good-transport cache. Called
// once at startup; a nil store (or never calling this) leaves the optimization
// off, in which case connects always walk the full cascade.
func (s *Service) SetTransportMemory(store transportMemory) {
	s.transportMemory = store
}

// ClearTransportMemory forgets every remembered network. No-op when the cache
// is not wired, since then there is nothing to forget.
func (s *Service) ClearTransportMemory() error {
	if s.transportMemory == nil {
		return nil
	}
	return s.transportMemory.Clear()
}

// SetShadowsocksProxy wires the hub proxy; nil makes /ssproxy/* report it
// unavailable.
func (s *Service) SetShadowsocksProxy(proxy shadowsocksProxyManager) {
	s.shadowsocksProxy = proxy
}

// StartShadowsocksProxy returns the proxy's loopback port. Permits the remote
// first, or a Lockdown lock would block our own dial.
func (s *Service) StartShadowsocksProxy(ctx context.Context, profile state.ShadowsocksProfile) (int, error) {
	if s.shadowsocksProxy == nil {
		return 0, errors.New("shadowsocks proxy is not available")
	}
	if host := strings.TrimSpace(profile.RemoteHost); host != "" {
		if err := s.PermitHosts(ctx, []string{host}); err != nil {
			s.logs.Add(state.LogWarn, state.SourceShadowsocks, fmt.Sprintf(
				"could not permit shadowsocks hub proxy remote %s through the kill switch: %v", host, err))
		}
	}
	return s.shadowsocksProxy.Start(ctx, profile)
}

func (s *Service) StopShadowsocksProxy(ctx context.Context) error {
	if s.shadowsocksProxy == nil {
		return nil
	}
	return s.shadowsocksProxy.Stop(ctx)
}

// ShadowsocksProxyCredentials returns the live proxy's Basic Auth
// username/password, both empty if no proxy is running. The server.go
// /ssproxy/start handler must surface these to the caller (as proxyUsername/
// proxyPassword) alongside the port from StartShadowsocksProxy, or the
// desktop client has no way to send the now-mandatory Proxy-Authorization
// header and hub-over-Shadowsocks starts failing with 407.
func (s *Service) ShadowsocksProxyCredentials() (string, string) {
	if s.shadowsocksProxy == nil {
		return "", ""
	}
	return s.shadowsocksProxy.Credentials()
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
	case "shadowsocks":
		return s.shadowsocks
	case "snowflake":
		return s.snowflake
	case transportKindWireGuard:
		// Nothing to manage: no transport is the point of the direct method.
		return nil
	default:
		return nil
	}
}

func (s *Service) setActiveTransportKind(kind string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	s.activeTransportKind = kind
}

func (s *Service) setConnectingTransportKind(kind string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	s.connectingTransportKind = kind
}

func (s *Service) activeTransportKindSnapshot() string {
	s.activeMu.RLock()
	defer s.activeMu.RUnlock()
	return s.activeTransportKind
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

	// "cloak", "naive", "reality", "hysteria2", "shadowsocks", "snowflake", or
	// "" for the auto cascade (see autoCascadeOrder).
	PreferredTransport string
}

func (s *Service) Connect(ctx context.Context, profileID string, opts ConnectOptions) (err error) {
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

	// A connect that fails after interrupting the repair must re-run it, or a
	// genuinely broken host network is stranded with no retry.
	interruptedRepair := s.cancelNetworkRepair()
	defer func() {
		if err != nil {
			s.startNetworkRepair(interruptedRepair)
		}
	}()
	adopted, err := s.attachToRunningSession(ctx, profile, opts.PreferredTransport)
	if err != nil {
		s.setError(err.Error())
		return err
	}
	if adopted {
		if err := s.armKillSwitchForAdoptedTunnel(ctx, profile, opts); err != nil {
			s.setError(err.Error())
			return err
		}
		s.setSessionOpts(opts)
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
func (s *Service) bringUpAfterKillSwitch(ctx context.Context, profile state.Profile, wireGuardProfile state.WireGuardProfile, opts ConnectOptions) (err error) {
	// Cancelled here too because Switch and the recovery rebuild reach this
	// without going through Connect; restarted on failure for the same reason.
	interruptedRepair := s.cancelNetworkRepair()
	defer func() {
		if err != nil {
			s.startNetworkRepair(interruptedRepair)
		}
	}()
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

	s.setCurrentProfile(profile)
	s.setSessionOpts(opts)
	// A new session starts with a clean slate: a deferral count left over
	// from the last one must not blunt this session's first genuine repair.
	s.resetEndpointRouteRepairs()
	s.machine.Set(state.StateConnected, "tunnel active")
	return nil
}

// armKillSwitchForAdoptedTunnel enables and updates the kill switch for a
// tunnel Connect just adopted rather than built. Without this, adopting a
// running session skips Enable entirely: opts.Lockdown is silently ignored
// and any permit left from a previous run never gets Update()'d onto the
// adapter actually in use.
func (s *Service) armKillSwitchForAdoptedTunnel(ctx context.Context, profile state.Profile, opts ConnectOptions) error {
	wireGuardProfile, err := wireGuardProfileFor(profile, opts.AllowLAN)
	if err != nil {
		return fmt.Errorf("allow-lan config transform failed: %w", err)
	}
	s.machine.Set(state.StateConnecting, "arming kill switch for adopted tunnel")
	if err := s.killSwitch.Enable(ctx, killSwitchPermits(profile), opts.AllowLAN, opts.Lockdown); err != nil {
		return fmt.Errorf("kill switch enable failed for adopted tunnel: %w", err)
	}
	tunnel := s.resolveTunnelRef(ctx, wireGuardProfile)
	if err := s.killSwitch.Update(ctx, tunnel); err != nil {
		return fmt.Errorf("kill switch tunnel update failed for adopted tunnel: %w", err)
	}
	s.machine.Set(state.StateConnected, "adopted tunnel active")
	return nil
}

// tearDownFailedBringUp stops the tunnel a bring-up had running before it
// failed, so the next Connect is not refused by a session nobody owns —
// ensureNoRunningWireGuard would otherwise make the user disconnect by hand
// first. The kill switch stays armed: a failed connect is fail-closed.
func (s *Service) tearDownFailedBringUp(wireGuardProfile state.WireGuardProfile) {
	// Not the caller's context: it may already be cancelled, and this cleanup
	// still has to run.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	if err := s.wg.Stop(cleanupCtx, wireGuardProfile); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("cleanup after failed bring-up: wireguard stop warning: %v", err))
	}
	if active := s.activeTransport(); active != nil {
		if err := active.Stop(cleanupCtx); err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("cleanup after failed bring-up: transport stop warning: %v", err))
		}
	}
	s.setActiveTransportKind("")
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
	if !s.cloak.Status().Running || !s.cloakStartedForRemote(profile.Cloak) {
		cloakStartProfile := profile.Cloak
		cloakStartProfile.LocalPort = 0
		if err := s.cloak.Start(ctx, cloakStartProfile); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		s.rememberCloakRemote(profile.Cloak)
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

// cloakStartedForRemote reports whether Cloak was last started against the
// same server as target (ignoring LocalPort, which is rebound per session).
// A live cloak.Status().Running says a process is up, not which server it is
// bridging — after a failed teardown that can still be the old remote.
func (s *Service) cloakStartedForRemote(target state.CloakProfile) bool {
	s.cloakMu.Lock()
	defer s.cloakMu.Unlock()
	last := s.cloakStartedFor
	last.LocalPort = target.LocalPort
	return last == target
}

func (s *Service) rememberCloakRemote(target state.CloakProfile) {
	s.cloakMu.Lock()
	defer s.cloakMu.Unlock()
	s.cloakStartedFor = target
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

// startShadowsocksTransport runs Shadowsocks's start sequence, mirroring
// startHysteria2Transport minus the SessionWaiter block — see Manager.Start.
func (s *Service) startShadowsocksTransport(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error {
	// De-alias from the config store's shared *ShadowsocksProfile before mutating LocalPort.
	shadowsocksCopy := *profile.Shadowsocks
	profile.Shadowsocks = &shadowsocksCopy

	if clamped := clampWireGuardMTU(wireGuardProfile.ConfigText, shadowsocksMaxMTU); clamped != wireGuardProfile.ConfigText {
		wireGuardProfile.ConfigText = clamped
		s.logs.Add(state.LogInfo, state.SourceShadowsocks, fmt.Sprintf(
			"clamped wireguard MTU to %d for the shadowsocks transport", shadowsocksMaxMTU))
	}

	shadowsocksStartProfile := *profile.Shadowsocks
	shadowsocksStartProfile.LocalPort = 0
	if err := s.shadowsocks.Start(ctx, shadowsocksStartProfile); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	profile.Shadowsocks.LocalPort = s.rebindWireGuardEndpoint(s.shadowsocks, profile.Shadowsocks.LocalPort, wireGuardProfile)
	shadowsocksRunning := func() bool { return s.shadowsocks.Status().Running }
	if err := s.waitForManagedTransportStable(ctx, shadowsocksRunning, profile.Shadowsocks.LocalPort, 200*time.Millisecond); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.shadowsocks.Stop(cleanupCtx)
		cleanupCancel()
		return err
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

// transportKindWireGuard names the direct method: WireGuard straight to the
// node's own UDP listener, with nothing in front of it. It is a transport kind
// only so the rest of the session machinery (status, health checks, recovery)
// can name what is carrying the tunnel; there is no transport process.
const transportKindWireGuard = "wireguard"

// startDirectWireGuard points the tunnel at the node itself instead of a
// loopback bridge. Nothing to start — the "transport" is the absence of one — so
// all this does is rewrite the Endpoint line the other transports would have
// bound a local port for. Faster and lower-overhead than every other method,
// and trivially recognizable on the wire, which is why the user has to ask for
// it by name (it is not in autoCascadeOrder).
func (s *Service) startDirectWireGuard(_ context.Context, _ *state.Profile, wireGuardProfile *state.WireGuardProfile) error {
	endpoint, err := validDirectEndpoint(wireGuardProfile.DirectEndpoint)
	if err != nil {
		return err
	}
	rewritten, replaced := rewriteWireGuardEndpoint(wireGuardProfile.ConfigText, endpoint)
	if !replaced {
		return errors.New("wireguard config has no Endpoint line to repoint at the node")
	}
	wireGuardProfile.ConfigText = rewritten
	s.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf(
		"connecting straight to %s with no transport in front of it", endpoint))
	return nil
}

// validDirectEndpoint accepts a host:port with a numeric port, the shape a WireGuard
// Endpoint line needs. Checked here so a malformed profile fails the connect
// with something readable instead of producing a config WireGuard rejects.
func validDirectEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", errors.New("this profile carries no direct wireguard endpoint")
	}
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("direct wireguard endpoint %q is not host:port", endpoint)
	}
	if portNum, err := strconv.Atoi(port); err != nil || portNum <= 0 || portNum > 65535 {
		return "", fmt.Errorf("direct wireguard endpoint %q has no valid port", endpoint)
	}
	return endpoint, nil
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
	case transportKindWireGuard:
		if _, err := validDirectEndpoint(profile.WireGuard.DirectEndpoint); err != nil {
			return nil, fmt.Errorf("plain wireguard requested but %w", err)
		}
		return []transportCandidate{{transportKindWireGuard, s.startDirectWireGuard}}, nil
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
	case "shadowsocks":
		if profile.Shadowsocks == nil {
			return nil, errors.New("shadowsocks transport requested but this profile has no shadowsocks configuration")
		}
		return []transportCandidate{{"shadowsocks", s.startShadowsocksTransport}}, nil
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
//   - reality: the strongest TLS disguise — borrows a real site's TLS
//     handshake, so active probing and SNI blocking see a genuine site. First
//     because it survives the probing that catches the others.
//   - cloak: the reliable fallback (TLS obfuscation to a decoy cover site), and
//     the only transport every profile carries, so it is what auto mode lands
//     on when reality is unprovisioned or blocked.
//   - shadowsocks: SS-2022, which presents no TLS at all on its own port, so it
//     is the first attempt that shares nothing with a TLS block above it.
//   - hysteria2: a different wire protocol (UDP/QUIC + Salamander), so a block
//     that kills the TCP transports does not kill every attempt.
//   - naive: real Chromium TLS+HTTP/2 — a different TLS fingerprint than
//     reality, for when UDP is blocked and reality's approach was the problem.
//   - snowflake: heavy-artillery last resort (WebRTC rendezvous, no fixed
//     endpoint); currently gated off (see snowflakeReleaseGated).
//
// The direct wireguard method is deliberately absent: a bare WireGuard handshake
// is the single easiest thing on this list for a censor to fingerprint and drop,
// so auto mode never falls back to it — only an explicit request selects it.
//
// Per-network memory can still promote whatever last worked here ahead of this
// default (see reorderByMemory).
var autoCascadeOrder = []string{"reality", "cloak", "shadowsocks", "hysteria2", "naive", "snowflake"}

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
	case "shadowsocks":
		if profile.Shadowsocks == nil {
			return nil, false
		}
		return s.startShadowsocksTransport, true
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
	candidates = s.demoteFailedTransport(candidates)
	defer s.setConnectingTransportKind("")

	var failures []string
	for i, candidate := range candidates {
		s.setConnectingTransportKind(candidate.kind)
		// A fresh copy per candidate: bringUpTransport's start funcs mutate
		// ConfigText (MTU clamp, loopback Endpoint rewrite), and without this
		// a failed candidate's rewrite leaked into whichever one succeeded
		// after it — a permanently clamped MTU, or an Endpoint still pointing
		// at the dead candidate's port.
		attempt := *wireGuardProfile
		if err := s.bringUpTransport(ctx, profile, &attempt, candidate.kind, candidate.start); err != nil {
			// A cancelled context means Disconnect interrupted us; stop trying.
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			failures = append(failures, err.Error())
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("%s transport did not establish a tunnel: %v", candidate.kind, err))
			continue
		}
		*wireGuardProfile = attempt
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

// demoteFailedTransport moves the transport whose data path just died to the
// back, so recovery tries every other way out before walking into the block.
func (s *Service) demoteFailedTransport(candidates []transportCandidate) []transportCandidate {
	kind := s.takeDemotedTransport()
	if kind == "" || len(candidates) < 2 {
		return candidates
	}
	demoted := make([]transportCandidate, 0, len(candidates))
	var tail []transportCandidate
	for _, candidate := range candidates {
		if candidate.kind == kind {
			tail = append(tail, candidate)
			continue
		}
		demoted = append(demoted, candidate)
	}
	return append(demoted, tail...)
}

// takeDemotedTransport reads and clears the demotion, which applies to the next
// cascade only — a later session must not inherit it.
func (s *Service) takeDemotedTransport() string {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	kind := s.demotedTransport
	s.demotedTransport = ""
	return kind
}

// rememberTransport records kind as the last-good transport for networkKey so
// the next auto-connect on this network tries it first. Best-effort: a nil
// store, empty key, or persist error only forfeits the optimization.
func (s *Service) rememberTransport(networkKey, kind string) {
	if s.transportMemory == nil || networkKey == "" || kind == "" {
		return
	}
	// A direct connection says nothing about which obfuscated transport gets
	// through here, and auto mode never offers it — recording it would only
	// displace a memory that is useful.
	if kind == transportKindWireGuard {
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
			// Each stop gets its own deadline: on the common handshake-timeout
			// path, wg.Stop consuming a deadline shared with mgr.Stop left the
			// transport never killed, so the next candidate started while the
			// previous one still held its loopback UDP port.
			wgCleanupCtx, wgCleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			if stopErr := s.wg.Stop(wgCleanupCtx, *wireGuardProfile); stopErr != nil {
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("%s cleanup after failed bring-up: wireguard stop warning: %v", kind, stopErr))
			}
			wgCleanupCancel()

			if mgr := s.managerForKind(kind); mgr != nil {
				mgrCleanupCtx, mgrCleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
				if stopErr := mgr.Stop(mgrCleanupCtx); stopErr != nil {
					s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("%s cleanup after failed bring-up: transport stop warning: %v", kind, stopErr))
				}
				mgrCleanupCancel()

				// Best-effort: a transport that didn't release its local port
				// in time would otherwise block the next candidate from
				// binding it.
				if port := transportLocalPort(profile, kind); port > 0 {
					killCtx, killCancel := context.WithTimeout(context.Background(), 2*time.Second)
					if killed, killErr := platform.KillUDPPortOwners(killCtx, port, []int{os.Getpid()}); killErr == nil && len(killed) > 0 {
						s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("%s cleanup: killed leftover process(es) still holding local port %d: %v", kind, port, killed))
					}
					killCancel()
				}
			}
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

	// The permit must open before the probe: WFP blocks the daemon's own
	// packets out the tunnel adapter, so probing a locked tunnel always times
	// out. It doesn't depend on the handshake, so it runs alongside the wait.
	updateDone := make(chan error, 1)
	go func() {
		updateStart := time.Now()
		tunnel := s.resolveTunnelRef(ctx, *wireGuardProfile)
		updateErr := s.killSwitch.Update(ctx, tunnel)
		if updateErr == nil {
			s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf(
				"kill switch updated (%dms), permitting %s (LUID %d)", time.Since(updateStart).Milliseconds(), tunnel.Name, tunnel.WindowsLUID))
		}
		updateDone <- updateErr
	}()

	s.machine.Set(state.StateConnecting, fmt.Sprintf("waiting for %s handshake", kind))
	handshakeStart := time.Now()
	if err = s.waitForWireGuardHandshake(ctx, *wireGuardProfile); err != nil {
		<-updateDone
		return fmt.Errorf("%s: %w", kind, err)
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("%s wireguard handshake completed (%dms)", kind, time.Since(handshakeStart).Milliseconds()))

	if err = <-updateDone; err != nil {
		return fmt.Errorf("%s: kill switch tunnel update: %w", kind, err)
	}

	s.machine.Set(state.StateConnecting, fmt.Sprintf("checking traffic over %s", kind))
	probeStart := time.Now()
	if err = s.proveDataPath(ctx, *wireGuardProfile); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("%s data path verified (%dms)", kind, time.Since(probeStart).Milliseconds()))
	return nil
}

// transportLocalPort reads the local port a candidate was configured to bind,
// or 0 if kind isn't configured on profile. Used only for best-effort cleanup
// after a failed bring-up.
func transportLocalPort(profile *state.Profile, kind string) int {
	switch kind {
	case "cloak":
		return profile.Cloak.LocalPort
	case "naive":
		if profile.Naive != nil {
			return profile.Naive.LocalPort
		}
	case "reality":
		if profile.Reality != nil {
			return profile.Reality.LocalPort
		}
	case "hysteria2":
		if profile.Hysteria2 != nil {
			return profile.Hysteria2.LocalPort
		}
	case "shadowsocks":
		if profile.Shadowsocks != nil {
			return profile.Shadowsocks.LocalPort
		}
	case "snowflake":
		if profile.Snowflake != nil {
			return profile.Snowflake.LocalPort
		}
	}
	return 0
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

	// Adapter reuse: pin the new server's bypass routes while the old tunnel
	// still owns the default route, then let wg.Start re-point the live device.
	// A dial that fails before wg.Start leaves the pinned routes on the old
	// session; the next teardown or in-place diff cleans them up.
	keepDevice := false
	if pinner, ok := s.wg.(wgInPlaceSwitcher); ok {
		if err := pinner.PinEndpointRoutes(ctx, wireGuardProfile); err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("switch: could not pre-route the new endpoints (%v); rebuilding the device", err))
		} else {
			keepDevice = true
		}
	}
	if !keepDevice {
		s.machine.Set(state.StateConnecting, fmt.Sprintf("switching to %s: stopping wireguard", newProfile.ID))
		if err := s.wg.Stop(ctx, withTransportBypassHosts(oldProfile)); err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("switch: wg stop warning: %v", err))
		}
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

	// Set eagerly, not cleared: a failed bring-up below still needs a current
	// profile for retryDroppedSession to rebuild toward, or the kill switch
	// stays armed with nothing to recover it (see attemptSessionRebuild).
	s.setCurrentProfile(newProfile)
	s.setSessionOpts(opts)

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
	s.opMu.Lock()
	defer s.opMu.Unlock()

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
	if errors.Is(err, platform.ErrKillSwitchStateUnreadable) {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("lockdown not recorded: kill switch state file is corrupt: %v", err))
		return nil
	}
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
			if errors.Is(err, platform.ErrKillSwitchStateUnreadable) {
				// A corrupt file says nothing about whether the lock is a
				// deliberate Lockdown; ask the platform whether it's live
				// instead of guessing from a file we can't trust.
				live := s.killSwitch.Active()
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("kill switch state file is corrupt during shutdown; retaining based on live rules (active=%v): %v", live, err))
				return live
			}
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

	// Teardown must finish regardless of the caller's context: an HTTP
	// request abort (app quit, renderer timeout) must not leave WireGuard up
	// with the kill switch already cleared and no profile left for recovery
	// to find. Every mutating step below uses this instead of ctx.
	teardownCtx, teardownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer teardownCancel()

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
	for _, running := range s.findRunningWireGuardProfiles(teardownCtx) {
		addProfile(running)
	}

	for _, runningProfile := range profilesToStop {
		if err := s.wg.Stop(teardownCtx, withTransportBypassHosts(runningProfile)); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("wireguard stop failed for %s: %v", runningProfile.WireGuard.TunnelName, err))
			s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
		}
	}

	s.machine.Set(state.StateDisconnecting, "verifying wireguard")
	for _, runningProfile := range profilesToStop {
		status, err := s.wg.Status(teardownCtx, withTransportBypassHosts(runningProfile))
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

	repairTunnelNames := make([]string, 0, len(profilesToStop))
	for _, p := range profilesToStop {
		if name := strings.TrimSpace(p.WireGuard.TunnelName); name != "" {
			repairTunnelNames = append(repairTunnelNames, name)
		}
	}

	// Always clear profile and kill switch regardless of earlier errors.
	s.clearCurrentProfile()
	s.resetRecovery()
	s.resetEndpointRouteRepairs()

	if s.killSwitch.Active() {
		if keepKillSwitch {
			// Retaining the lock past the session is what Lockdown means, but the
			// departing server's endpoints have no business staying permitted once
			// its session is gone — re-arm with just the hub's control-plane hosts
			// instead of reusing the persisted (still server-inclusive) permit set.
			hubPermits := ipLiterals(s.storedControlPlaneHosts())
			if err := s.killSwitch.Enable(teardownCtx, hubPermits, false, true); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Sprintf("lockdown re-arm after disconnect failed: %v", err))
				s.logs.Add(state.LogWarn, state.SourceDaemon, cleanupErrors[len(cleanupErrors)-1])
			} else {
				s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("kill switch retained (lockdown mode), narrowed to control-plane hosts %v", hubPermits))
			}
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
	s.startNetworkRepair(repairTunnelNames)
	if len(cleanupErrors) > 0 {
		detail := fmt.Sprintf("disconnect completed with warnings: %s", strings.Join(cleanupErrors, "; "))
		s.logs.Add(state.LogWarn, state.SourceDaemon, detail)
		return errors.New(detail)
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "disconnect flow completed")
	return nil
}

// networkRepairTimeout bounds the background post-disconnect repair; renews
// can block on DHCP, and nothing user-visible waits on this anymore.
const networkRepairTimeout = 45 * time.Second

// startNetworkRepair runs the post-disconnect route/DNS cleanup in the
// background: the user is already disconnected, only the host's plumbing waits.
func (s *Service) startNetworkRepair(tunnelNames []string) {
	if s.networkRepair == nil || len(tunnelNames) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkRepairTimeout)
	s.repairMu.Lock()
	if s.repairCancel != nil {
		s.repairCancel()
	}
	s.repairCancel = cancel
	s.repairNames = tunnelNames
	s.repairSeq++
	seq := s.repairSeq
	s.repairMu.Unlock()

	go func() {
		defer cancel()
		actions, err := s.networkRepair(ctx, tunnelNames)
		s.repairMu.Lock()
		if s.repairSeq == seq {
			s.repairCancel = nil
			s.repairNames = nil
		}
		s.repairMu.Unlock()
		for _, action := range actions {
			s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("post-disconnect repair: %s", action))
		}
		// A deliberate cancel (new connect superseding this repair) is quiet; a
		// genuine failure or the 45s deadline expiring is worth a warning.
		if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("post-disconnect network repair failed: %v", err))
		}
	}()
}

// cancelNetworkRepair aborts any in-flight background repair so it can't renew
// adapters under a fresh tunnel; returns its tunnel names for a later restart.
func (s *Service) cancelNetworkRepair() []string {
	s.repairMu.Lock()
	defer s.repairMu.Unlock()
	if s.repairCancel == nil {
		return nil
	}
	s.repairCancel()
	s.repairCancel = nil
	names := s.repairNames
	s.repairNames = nil
	return names
}

func (s *Service) Status(ctx context.Context) state.StatusResponse {
	stateValue, detail := s.machine.Get()

	s.activeMu.RLock()
	activeKind := s.activeTransportKind
	connectingKind := s.connectingTransportKind
	s.activeMu.RUnlock()

	cloakStatus := s.cloak.Status()
	naiveStatus := s.naive.Status()
	realityStatus := s.reality.Status()
	hysteria2Status := s.hysteria2.Status()
	shadowsocksStatus := s.shadowsocks.Status()
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
		State:               stateValue,
		Detail:              detail,
		ActiveTransport:     activeKind,
		ConnectingTransport: connectingKind,
		Cloak:               cloakStatus,
		Naive:               naiveStatus,
		Reality:             realityStatus,
		Hysteria2:           hysteria2Status,
		Shadowsocks:         shadowsocksStatus,
		Snowflake:           snowflakeStatus,
		WireGuard:           wgStatus,
		KillSwitchActive:    s.killSwitch.Active(),
		Reconnecting:        s.recoveryPending(),
		TransportsExhausted: s.transportsAreExhausted(),
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

// healthTickInterval is how often the health loop samples the live session.
const healthTickInterval = 3 * time.Second

// suspendGapThreshold is how far behind schedule a health tick must land before
// it is read as "the host was asleep" rather than "the host was busy".
const suspendGapThreshold = 30 * time.Second

// resumeSettleGrace is how long health checks pause after a detected resume.
// The tunnel is almost certainly dead, but the host's interfaces and routes
// come back over several seconds — tearing the session down and re-dialing into
// a network that is not up yet just fails, and on an unlucky wake that failure
// used to be the last thing the daemon ever did about it.
const resumeSettleGrace = 15 * time.Second

// defaultRecoveryDelays is the backoff between attempts to rebuild a session
// that dropped on its own. The last entry repeats: recovery does not give up
// while the session is still the user's, because the kill switch stays armed
// the whole time and stopping would leave the device with no network at all.
var defaultRecoveryDelays = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

func (s *Service) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(healthTickInterval)
	defer ticker.Stop()

	lastTick := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			if gap := now.Sub(lastTick); gap > suspendGapThreshold {
				s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf(
					"resume detected (health check gap of %s); letting the network settle before checking the tunnel", gap.Round(time.Second)))
				s.holdHealthChecks(resumeSettleGrace)
			}
			lastTick = now
			s.runHealthCheck(ctx)
		}
	}
}

func (s *Service) runHealthCheck(ctx context.Context) {
	if s.healthHeld() {
		return
	}

	currentState, _ := s.machine.Get()
	if currentState == state.StateError {
		s.retryDroppedSession(ctx)
		return
	}
	if currentState != state.StateConnected {
		return
	}

	profile, ok := s.getCurrentProfile()
	if !ok {
		s.setErrorFromHealthCheck("health check failed: missing active profile")
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
	case "shadowsocks":
		transportRunning = s.shadowsocks.Status().Running
	case "snowflake":
		transportRunning = s.snowflake.Status().Running
	case transportKindWireGuard:
		// No transport process to be up; the WireGuard checks below are the whole
		// health picture for a direct session.
		transportRunning = true
	}

	if !transportRunning {
		if err := s.recoverActiveTransport(ctx, profile, activeKind); err != nil {
			s.setErrorFromHealthCheck(fmt.Sprintf("health check failed: %s transport is not running and restart failed: %v", activeKind, err))
			return
		}
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("health check recovered %s transport", activeKind))
	}

	wgStatus, err := s.wg.Status(ctx, profile.WireGuard)
	if err != nil {
		s.setErrorFromHealthCheck(fmt.Sprintf("health check failed: wireguard status error: %v", err))
		return
	}
	if !wgStatus.Running {
		s.setErrorFromHealthCheck(fmt.Sprintf("health check failed: wireguard tunnel is down (%s)", wgStatus.Detail))
		return
	}
	// Checked before the silence detector because a lost bypass route is one of
	// the things that silences a tunnel, and re-pinning it is far cheaper than
	// the rebuild below. A repair earns the session this tick to handshake on
	// the restored path; a route that needed no repair reports nothing, so a
	// tunnel silent for another reason still reaches the rebuild.
	routeRepaired := s.ensureEndpointRoutes(ctx, profile)

	// A stale handshake is a hint, never the verdict: WireGuard's counters say
	// nothing about whether the tunnel still carries traffic.
	if !routeRepaired && s.wireGuardHandshakeStale(wgStatus) {
		age := time.Since(time.Unix(wgStatus.LastHandshakeUnix, 0)).Round(time.Second)
		if !s.canProveDataPath(profile) {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("wireguard tunnel is silent (no handshake for %s); rebuilding session", age))
			s.attemptSessionRebuild(ctx, profile, fmt.Sprintf("wireguard tunnel is silent (no handshake for %s)", age))
			return
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("no wireguard handshake for %s; testing the tunnel now", age))
		s.probeDataPathNow()
	}

	if !s.killSwitch.Active() {
		s.setErrorFromHealthCheck("health check failed: kill switch was cleared unexpectedly")
		return
	}

	// A route that was just re-pinned has not had a round trip on it yet, so
	// probing now would judge the session on the path it no longer uses.
	if routeRepaired {
		return
	}

	// Everything above says the session is up; these two ask whether it still
	// works — whether the host still resolves through it, and whether the tunnel
	// still carries anything.
	s.ensureTunnelDNS(ctx, profile)

	if s.dataPathIsDead(ctx, profile) {
		s.escalateDeadDataPath(ctx, profile, activeKind)
		return
	}

	s.resetRecovery()
}

// escalateDeadDataPath rebuilds a session whose tunnel stopped carrying traffic,
// re-running the whole cascade rather than restarting the transport that died.
func (s *Service) escalateDeadDataPath(ctx context.Context, profile state.Profile, activeKind string) {
	s.setDemotedTransport(activeKind)
	s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
		"%s tunnel stopped carrying traffic; trying every transport again", activeKind))
	s.attemptSessionRebuild(ctx, profile, "tunnel stopped carrying traffic")
}

// probeDataPathNow brings the next probe round forward to this tick.
func (s *Service) probeDataPathNow() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeNextAt = time.Time{}
}

func (s *Service) setDemotedTransport(kind string) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.demotedTransport = kind
}

// setTransportsExhausted records whether the cascade ran out on this server, so
// Status can tell the app to rotate.
func (s *Service) setTransportsExhausted(exhausted bool) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.transportsExhausted = exhausted
}

func (s *Service) transportsAreExhausted() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return s.transportsExhausted
}

// retryDroppedSession keeps rebuilding a session that dropped on its own until
// it comes back. Without it a single failed rebuild is terminal: the health
// check stops at StateError, so nothing looks at the session again, and the
// kill switch it deliberately left armed means the device has no network until
// the user notices and clicks something. The common trigger is a laptop waking
// up — the first rebuild lands before the NIC has a route and fails with
// "unreachable network", whichever transport it happens to be dialling.
//
// Only a session that is still the user's is retried: Disconnect clears the
// current profile, and a first Connect that never landed never set one, so both
// are left alone.
func (s *Service) retryDroppedSession(ctx context.Context) {
	profile, ok := s.getCurrentProfile()
	if !ok {
		return
	}
	// Not every error means a broken tunnel — a refused Connect stamps one on a
	// session that is still carrying traffic. Rebuilding that would tear down a
	// working tunnel, so a live, fail-closed session just gets its state back.
	if s.sessionIsHealthy(ctx, profile) {
		if s.machine.CompareAndSet([]state.DaemonState{state.StateError}, state.StateConnected, "tunnel active") {
			s.logs.Add(state.LogInfo, state.SourceDaemon, "tunnel is still handshaking; clearing the error state")
		}
		s.resetRecovery()
		return
	}
	// Nothing to dial out of yet (still resuming, WiFi not re-associated). That
	// is a wait, not a failed attempt — it must not push the backoff out.
	if !s.networkLooksUsable() {
		return
	}
	if !s.recoveryDue() {
		return
	}
	s.attemptSessionRebuild(ctx, profile, "connection lost")
}

// attemptSessionRebuild runs one rebuild and books the result against the retry
// schedule: success clears it, failure backs the next attempt off. cause says
// why the session needs rebuilding and is carried into the error detail so the
// UI keeps naming the original fault rather than just the latest retry.
func (s *Service) attemptSessionRebuild(ctx context.Context, profile state.Profile, cause string) {
	err := s.rebuildSilentSession(ctx, profile)
	if errors.Is(err, errRebuildBusy) {
		// A real operation (or an overlapping rebuild) already owns opMu;
		// this tick simply didn't get to run, which must not burn a retry
		// attempt or push the backoff out.
		return
	}
	s.setTransportsExhausted(errors.Is(err, ErrTransportExhausted))

	attempt := s.beginRecoveryAttempt()
	if attempt > 1 {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("reconnecting dropped session (attempt %d)", attempt))
	}

	if err != nil {
		delay := s.scheduleNextRecovery(attempt)
		detail := fmt.Sprintf("%s and reconnect attempt %d failed: %v; retrying in %s", cause, attempt, err, delay)
		// A Disconnect can land mid-rebuild and interrupt it; CompareAndSet
		// only stamps Error over Connected/Error, so a Disconnect that
		// already moved the state on wins the race instead of being
		// silently overwritten by this stale failure.
		if !s.machine.CompareAndSet([]state.DaemonState{state.StateConnected, state.StateError}, state.StateError, detail) {
			s.resetRecovery()
			return
		}
		s.logs.Add(state.LogError, state.SourceDaemon, detail)
		return
	}

	if attempt > 1 {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("session recovered after %d reconnect attempts", attempt))
	}
	s.resetRecovery()
}

// sessionIsHealthy reports whether the session is in fact carrying traffic
// fail-closed: the interface is up, its newest handshake is inside the stale
// window, and the kill switch is still armed. An unarmed kill switch is not
// healthy — the rebuild path re-arms it, so let it run.
func (s *Service) sessionIsHealthy(ctx context.Context, profile state.Profile) bool {
	if !s.killSwitch.Active() {
		return false
	}
	status, err := s.wg.Status(ctx, profile.WireGuard)
	if err != nil || !status.Running || status.LastHandshakeUnix <= 0 {
		return false
	}
	return !s.wireGuardHandshakeStale(status)
}

// networkLooksUsable reports whether the host has an off-tunnel address to dial
// from. An unknown answer (no fingerprint configured) counts as usable, so the
// retry falls back to letting the transport itself fail.
func (s *Service) networkLooksUsable() bool {
	if s.networkKey == nil {
		return true
	}
	return s.networkKey() != ""
}

func (s *Service) beginRecoveryAttempt() int {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.recoveryAttempts++
	return s.recoveryAttempts
}

// scheduleNextRecovery books the next attempt after a failure and reports the
// wait, so the error detail can say when the daemon will try again.
func (s *Service) scheduleNextRecovery(attempt int) time.Duration {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()

	delays := s.recoveryDelays
	if len(delays) == 0 {
		delays = defaultRecoveryDelays
	}
	index := min(max(attempt-1, 0), len(delays)-1)
	delay := delays[index]
	s.recoveryNextAt = time.Now().Add(delay)
	return delay
}

func (s *Service) recoveryDue() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return !time.Now().Before(s.recoveryNextAt)
}

// recoveryPending reports whether a dropped session is mid-recovery: at least
// one rebuild has been tried and the loop has not given the session back yet.
func (s *Service) recoveryPending() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return s.recoveryAttempts > 0
}

func (s *Service) resetRecovery() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.recoveryAttempts = 0
	s.recoveryNextAt = time.Time{}
}

// holdHealthChecks pauses health evaluation for d and clears any backoff, so
// the first attempt after a resume is immediate once the pause expires.
func (s *Service) holdHealthChecks(d time.Duration) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.healthHoldUntil = time.Now().Add(d)
	s.recoveryAttempts = 0
	s.recoveryNextAt = time.Time{}
	// Rounds that failed while the host was asleep say nothing about the network
	// it woke up on, and the first probe should land after it has settled.
	s.dnsProbeFailures = 0
	s.dnsProbeNextAt = time.Now().Add(d + nextDNSProbeDelay())
}

func (s *Service) healthHeld() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return time.Now().Before(s.healthHoldUntil)
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
//
// Runs from Connected (the first rebuild) and from Error (every retry after
// one failed); anything else means a Disconnect got here first.
func (s *Service) rebuildSilentSession(ctx context.Context, profile state.Profile) error {
	// Registered before TryLock, not after: Disconnect cancels cancelConnect
	// and only then blocks on opMu.Lock(), so a cancel func that only exists
	// once TryLock has already won is invisible to a Disconnect that landed
	// in between — it cancels nothing and then blocks behind the whole
	// cascade. Setting it first closes that window; if TryLock then fails,
	// it's cleared again immediately below.
	rebuildCtx, cancel := context.WithCancel(ctx)
	s.cancelMu.Lock()
	s.cancelConnect = cancel
	s.cancelMu.Unlock()
	clearCancel := func() {
		s.cancelMu.Lock()
		s.cancelConnect = nil
		s.cancelMu.Unlock()
	}

	if !s.opMu.TryLock() {
		cancel()
		clearCancel()
		return errRebuildBusy
	}
	defer s.opMu.Unlock()
	defer clearCancel()
	defer cancel()
	ctx = rebuildCtx

	if currentState, _ := s.machine.Get(); currentState != state.StateConnected && currentState != state.StateError {
		return errors.New("state changed")
	}

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

	// Bring-up assumes the kill switch is already armed — true across a rebuild
	// of a live session, but not when recovery is retrying after the firewall
	// was cleared out from under us (a resume can take the WFP session with it).
	// Re-arming first keeps the tunnel from coming back up wide open.
	if !s.killSwitch.Active() {
		s.logs.Add(state.LogWarn, state.SourceDaemon, "rebuild: kill switch was not armed; re-arming before bring-up")
		if err := s.killSwitch.Enable(ctx, killSwitchPermits(profile), opts.AllowLAN, opts.Lockdown); err != nil {
			return fmt.Errorf("kill switch re-arm failed: %w", err)
		}
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
	case "shadowsocks":
		if s.shadowsocks.Status().Running {
			return nil
		}
		if profile.Shadowsocks == nil {
			return errors.New("active transport is shadowsocks but profile has no shadowsocks config")
		}
		s.logs.Add(state.LogWarn, state.SourceDaemon, "health check detected shadowsocks stopped; attempting restart")
		if err := s.shadowsocks.Start(restartCtx, *profile.Shadowsocks); err != nil {
			return err
		}
		return s.waitForManagedTransportStable(restartCtx, func() bool { return s.shadowsocks.Status().Running }, profile.Shadowsocks.LocalPort, 2*time.Second)
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

// resolveTunnelRef names the live tunnel for the kill switch. The LUID matters
// most: it points at the device the manager actually created, where a name has
// to be resolved back through the OS and can land on an adapter of the same name
// that a rebuild is still tearing down.
func (s *Service) resolveTunnelRef(ctx context.Context, profile state.WireGuardProfile) platform.TunnelRef {
	return platform.TunnelRef{
		Name:        s.resolveWireGuardInterfaceName(ctx, profile),
		WindowsLUID: s.resolveTunnelLUID(ctx, profile),
	}
}

func (s *Service) resolveTunnelLUID(ctx context.Context, profile state.WireGuardProfile) uint64 {
	reporter, ok := s.wg.(wgTunnelLUIDReporter)
	if !ok {
		return 0
	}

	luid, err := reporter.ActiveTunnelLUID(ctx, profile)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("wireguard tunnel LUID lookup failed; the kill switch will resolve the interface by name: %v", err))
		return 0
	}
	return luid
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

// setErrorFromHealthCheck stamps StateError only if the machine is still
// StateConnected, so a Disconnect that finished inside the health check's
// unlocked read-then-act window (runHealthCheck holds no lock across it)
// cannot have its StateDisconnected overwritten by a stale failure.
func (s *Service) setErrorFromHealthCheck(detail string) {
	if !s.machine.CompareAndSet([]state.DaemonState{state.StateConnected}, state.StateError, detail) {
		return
	}
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
		persisted, err := loadKillSwitchState()
		switch {
		case errors.Is(err, platform.ErrKillSwitchStateUnreadable):
			// A corrupt file must not read as "nothing engaged" — that's
			// exactly what turns a real lock into a machine reported as
			// unprotected. Probe the platform for live rules instead of
			// falling into the stale-cleanup/no-op branches below.
			if s.killSwitch.Active() {
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("kill switch state file is corrupt but firewall rules are live; leaving them in place: %v", err))
			} else {
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("kill switch state file is corrupt and no live rules found; nothing to reconcile: %v", err))
			}
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
	// Nothing records which method built the tunnel we are adopting across a
	// restart, so this keeps the long-standing assumption: Cloak.
	adopted, err := s.attachToRunningSession(startupCtx, active, "")
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("startup tunnel recovery encountered an issue: %v", err))
		if adopted {
			// attachToRunningSession already stamped StateConnecting and
			// attached the profile; left alone that's a permanent stuck
			// state with a live, unmanaged tunnel and no kill switch. Move
			// to Error so the health loop's retryDroppedSession takes over.
			s.setError(fmt.Sprintf("startup tunnel recovery failed: %v", err))
		}
		return
	}
	if adopted {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("recovered active tunnel on startup: %s", active.WireGuard.TunnelName))
		s.reconcileKillSwitchForAdoptedTunnel(startupCtx, active)
	}
}

// reconcileKillSwitchForAdoptedTunnel arms the kill switch for a tunnel
// adopted on startup. Without this, adopting a running session on restart
// skips kill-switch reconciliation entirely (it only ran in the no-tunnel
// branch above), reaching Connected with the lock unarmed and any persisted
// Lockdown silently downgraded.
func (s *Service) reconcileKillSwitchForAdoptedTunnel(ctx context.Context, profile state.Profile) {
	persisted, err := loadKillSwitchState()
	locked := err == nil && persisted.Active && persisted.Locked
	allowLAN := err == nil && persisted.AllowLAN

	if err := s.killSwitch.Enable(ctx, killSwitchPermits(profile), allowLAN, locked); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not arm kill switch for adopted tunnel: %v", err))
		return
	}
	tunnel := s.resolveTunnelRef(ctx, withTransportBypassHosts(profile))
	if err := s.killSwitch.Update(ctx, tunnel); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not update kill switch tunnel ref for adopted tunnel: %v", err))
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

// nonDisconnectingStates is every machine state a background/adoption path is
// allowed to stamp Connected over; StateDisconnecting is deliberately absent
// so a Disconnect racing this codepath always wins instead of being
// overwritten by a stale "recovered active tunnel".
var nonDisconnectingStates = []state.DaemonState{
	state.StateDisconnected, state.StateConnecting, state.StateConnected, state.StateError,
}

// attachToRunningSession adopts a WireGuard tunnel that is already up rather
// than rebuilding it. preferredTransport is what the user asked for, which
// decides what the adopted session is taken to be running over; "" (the
// startup path, which has no record of it) falls back to cloak, the one
// transport every profile carries.
func (s *Service) attachToRunningSession(ctx context.Context, profile state.Profile, preferredTransport string) (bool, error) {
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

	// Asked for plain WireGuard: adopt the tunnel as-is. Starting a bridge
	// transport here would run something the tunnel is not pointed at.
	if preferredTransport == transportKindWireGuard {
		s.setActiveTransportKind(transportKindWireGuard)
		s.machine.CompareAndSet(nonDisconnectingStates, state.StateConnected, "recovered active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("adopted running tunnel %s", profile.WireGuard.TunnelName))
		return true, nil
	}

	kind := preferredTransport
	if kind == "" || kind == "auto" {
		kind = "cloak"
	}
	if err := s.restoreTransportForAdoption(ctx, &profile, kind); err != nil {
		return true, err
	}

	s.setActiveTransportKind(kind)
	s.machine.CompareAndSet(nonDisconnectingStates, state.StateConnected, "recovered active tunnel")
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("adopted running tunnel %s over %s", profile.WireGuard.TunnelName, kind))
	return true, nil
}

// restoreTransportForAdoption makes sure kind's bridge is actually live for a
// tunnel adopted from a previous session, starting it if the manager reports
// it down. Previously this only ever restored Cloak regardless of what the
// caller asked for, so adopting a reality/hysteria2/naive/shadowsocks session
// silently ran Cloak instead — a bridge the tunnel's Endpoint was never
// pointed at.
func (s *Service) restoreTransportForAdoption(ctx context.Context, profile *state.Profile, kind string) error {
	switch kind {
	case "cloak":
		if s.cloakStatusForProfile(ctx, *profile).Running {
			return nil
		}
		s.machine.Set(state.StateConnecting, "restoring cloak for active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, "wireguard was already running; restoring cloak process")
		if err := s.cloak.Start(ctx, profile.Cloak); err != nil {
			return fmt.Errorf("wireguard tunnel is already running but cloak restore failed: %w", err)
		}
		if !s.cloak.Status().Running {
			return errors.New("wireguard tunnel is already running but cloak failed to stay running")
		}
		s.rememberCloakRemote(profile.Cloak)
		return nil
	case "naive":
		if s.naive.Status().Running {
			return nil
		}
		if profile.Naive == nil {
			return errors.New("wireguard tunnel is already running but this profile has no naive configuration to restore")
		}
		s.machine.Set(state.StateConnecting, "restoring naive for active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, "wireguard was already running; restoring naive process")
		if err := s.naive.Start(ctx, *profile.Naive); err != nil {
			return fmt.Errorf("wireguard tunnel is already running but naive restore failed: %w", err)
		}
		return nil
	case "reality":
		if s.reality.Status().Running {
			return nil
		}
		if profile.Reality == nil {
			return errors.New("wireguard tunnel is already running but this profile has no reality configuration to restore")
		}
		s.machine.Set(state.StateConnecting, "restoring reality for active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, "wireguard was already running; restoring reality process")
		if err := s.reality.Start(ctx, *profile.Reality); err != nil {
			return fmt.Errorf("wireguard tunnel is already running but reality restore failed: %w", err)
		}
		return nil
	case "hysteria2":
		if s.hysteria2.Status().Running {
			return nil
		}
		if profile.Hysteria2 == nil {
			return errors.New("wireguard tunnel is already running but this profile has no hysteria2 configuration to restore")
		}
		s.machine.Set(state.StateConnecting, "restoring hysteria2 for active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, "wireguard was already running; restoring hysteria2 process")
		if err := s.hysteria2.Start(ctx, *profile.Hysteria2); err != nil {
			return fmt.Errorf("wireguard tunnel is already running but hysteria2 restore failed: %w", err)
		}
		return nil
	case "shadowsocks":
		if s.shadowsocks.Status().Running {
			return nil
		}
		if profile.Shadowsocks == nil {
			return errors.New("wireguard tunnel is already running but this profile has no shadowsocks configuration to restore")
		}
		s.machine.Set(state.StateConnecting, "restoring shadowsocks for active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, "wireguard was already running; restoring shadowsocks process")
		if err := s.shadowsocks.Start(ctx, *profile.Shadowsocks); err != nil {
			return fmt.Errorf("wireguard tunnel is already running but shadowsocks restore failed: %w", err)
		}
		return nil
	case "snowflake":
		if snowflakeReleaseGated {
			return errors.New("snowflake transport is temporarily unavailable")
		}
		if s.snowflake.Status().Running {
			return nil
		}
		if profile.Snowflake == nil {
			return errors.New("wireguard tunnel is already running but this profile has no snowflake configuration to restore")
		}
		s.machine.Set(state.StateConnecting, "restoring snowflake for active tunnel")
		s.logs.Add(state.LogInfo, state.SourceDaemon, "wireguard was already running; restoring snowflake process")
		if err := s.snowflake.Start(ctx, *profile.Snowflake); err != nil {
			return fmt.Errorf("wireguard tunnel is already running but snowflake restore failed: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown transport %q for adopted tunnel", kind)
	}
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
	if err != nil {
		if errors.Is(err, platform.ErrUDPPortOwnersUnsupported) {
			// Unknown, not "no owner": on platforms where the probe itself is
			// unsupported this always errors, and treating that as confirmed-dead
			// would restart a Cloak that may well still be alive on this port.
			cloakStatus.Running = true
			return cloakStatus
		}
		return cloakStatus
	}
	if len(owners) == 0 {
		return cloakStatus
	}

	pid := owners[0]
	cloakStatus.Running = true
	cloakStatus.PID = &pid
	return cloakStatus
}

// setCurrentProfile and clearCurrentProfile release profileMu before touching
// the probe schedule: recoveryMu is taken on its own everywhere else, and
// nesting it under profileMu here would be the only place with an ordering.
// deepCopyProfile independently copies profile's slices and its transport
// pointers (Naive, Reality, Hysteria2, Shadowsocks, Snowflake), mirroring what
// state's own (unexported) cloneProfile does for the config store. Without
// this, setCurrentProfile/getCurrentProfile only copied the WireGuard slices
// and left every transport pointer aliased, so e.g. rebuildSilentSession's
// profile.Naive.LocalPort write could clobber a copy another caller was
// concurrently reading, outside profileMu.
func deepCopyProfile(profile state.Profile) state.Profile {
	out := profile
	out.WireGuard.DNS = append([]string(nil), profile.WireGuard.DNS...)
	out.WireGuard.BypassHosts = append([]string(nil), profile.WireGuard.BypassHosts...)
	out.TransportEndpointIPs = append([]string(nil), profile.TransportEndpointIPs...)
	if profile.Naive != nil {
		naiveCopy := *profile.Naive
		out.Naive = &naiveCopy
	}
	if profile.Reality != nil {
		realityCopy := *profile.Reality
		out.Reality = &realityCopy
	}
	if profile.Hysteria2 != nil {
		hysteria2Copy := *profile.Hysteria2
		out.Hysteria2 = &hysteria2Copy
	}
	if profile.Shadowsocks != nil {
		shadowsocksCopy := *profile.Shadowsocks
		out.Shadowsocks = &shadowsocksCopy
	}
	if profile.Snowflake != nil {
		snowflakeCopy := *profile.Snowflake
		snowflakeCopy.FrontDomains = append([]string(nil), profile.Snowflake.FrontDomains...)
		snowflakeCopy.ICEServers = append([]string(nil), profile.Snowflake.ICEServers...)
		out.Snowflake = &snowflakeCopy
	}
	return out
}

func (s *Service) setCurrentProfile(profile state.Profile) {
	s.profileMu.Lock()
	copyProfile := deepCopyProfile(profile)
	s.currentProfile = &copyProfile
	s.profileMu.Unlock()

	s.resetDNSProbe()
}

func (s *Service) clearCurrentProfile() {
	s.profileMu.Lock()
	s.currentProfile = nil
	s.profileMu.Unlock()

	s.endDNSProbeSession()
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

	return deepCopyProfile(*s.currentProfile), true
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

// rewriteWireGuardEndpoint repoints every "Endpoint =" line at endpoint.
// Returns the rewritten text and whether there was a line to rewrite.
// ReplaceAllStringFunc rather than ReplaceAllString so a "$" in endpoint is
// never read as a capture-group reference.
func rewriteWireGuardEndpoint(configText, endpoint string) (string, bool) {
	if !wgEndpointPattern.MatchString(configText) {
		return configText, false
	}
	rewritten := wgEndpointPattern.ReplaceAllStringFunc(configText, func(line string) string {
		groups := wgEndpointPattern.FindStringSubmatch(line)
		if len(groups) != 3 {
			return line
		}
		return groups[1] + endpoint + groups[2]
	})
	return rewritten, true
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
	// Snowflake's rendezvous hosts are never covered by the hub's endpoint
	// IPs (those name the node, not the broker/STUN infrastructure), so they
	// must be appended regardless of which branch below runs.
	if len(out) > 0 {
		return append(out, snowflakeHosts(profile.Snowflake)...)
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
	if profile.Shadowsocks != nil {
		if host := strings.TrimSpace(profile.Shadowsocks.RemoteHost); host != "" {
			out = append(out, host)
		}
	}
	return append(out, snowflakeHosts(profile.Snowflake)...)
}

// withTransportBypassHosts adds the cloak, naive, reality, hysteria2,
// shadowsocks and snowflake (whichever are configured) remote hosts to the bypass
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
