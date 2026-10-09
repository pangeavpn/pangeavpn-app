package api

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

// splitController is the part of splittunnel.Controller the service drives.
type splitController interface {
	SetRules(apps []string, neverBypass []string) []procmatch.RuleError
	NetworkChanged()
	Status() splittunnel.Status
}

var _ splitController = (*splittunnel.Controller)(nil)

// wgAllowedIPsApplier moves a live device onto new AllowedIPs without reconnecting it.
type wgAllowedIPsApplier interface {
	ApplyAllowedIPs(ctx context.Context, profile state.WireGuardProfile) error
}

var errSplitTunnelUnavailable = errors.New("split tunnelling is not available")

const (
	// splitApplyTimeout bounds one reconcile pass; the apply itself needs no handshake.
	splitApplyTimeout = 30 * time.Second
	// splitRetryDelay spaces health-tick retries of an apply that keeps failing, so
	// it cannot keep opMu away from recovery.
	splitRetryDelay    = 15 * time.Second
	strictRPCheckEvery = 30 * time.Second
	// splitRouteBudgetPerEndpoint is the AllowedIPs headroom for each address the
	// desktop carves out of them (a /32 hole costs up to 32 prefixes).
	splitRouteBudgetPerEndpoint = 32
)

// appliedSplit is what the live session runs with: the ranges carved out of its
// AllowedIPs and those the lock permits. known is false until a bring-up records it.
type appliedSplit struct {
	gen     uint64
	known   bool
	routes  []netip.Prefix
	permits []netip.Prefix
	dropped bool
	// routesUnsure: a failed live apply may have half-moved the device's routes.
	routesUnsure bool
}

// splitTunnelState is the service's share of split tunnelling; store is nil when it is
// not wired in, and ctl is nil where app exclusion is unavailable.
type splitTunnelState struct {
	store *splitTunnelStore
	ctl   splitController
	self  string
	kick  chan struct{}
	// writeMu orders POSTs, so the engine sees rule sets in the order they were saved.
	writeMu sync.Mutex

	mu      sync.Mutex
	applied appliedSplit
	// lockRanges is what the lock was last asked to permit; lockUnsure means that ask
	// failed, so the lock may still hold other ranges.
	lockRanges  []netip.Prefix
	lockUnsure  bool
	retryAt     time.Time
	rpCheckedAt time.Time
	strictRP    bool
	strictRPFn  func() (bool, error)
}

func newSplitTunnelState() *splitTunnelState {
	return &splitTunnelState{kick: make(chan struct{}, 1), strictRPFn: platform.StrictReversePathFilter}
}

// splitTunnelView is GET /split-tunnel and the reply to a POST.
type splitTunnelView struct {
	Enabled           bool     `json:"enabled"`
	Apps              []string `json:"apps"`
	CIDRs             []string `json:"cidrs"`
	AppsSupported     bool     `json:"appsSupported"`
	UnavailableReason string   `json:"unavailableReason"`
	Active            bool     `json:"active"`
	Pending           bool     `json:"pending"`
	CIDRsDropped      bool     `json:"cidrsDropped,omitempty"`
}

// splitTunnelUpdate is a validated POST body; a nil Protect keeps the stored list.
type splitTunnelUpdate struct {
	Enabled bool
	Apps    []string
	CIDRs   []string
	Protect *[]string
}

// SetSplitTunnel wires split tunnelling in before StartBackground: it loads the stored
// settings and hands the app rules to ctl, which is nil where app exclusion is unavailable.
func (s *Service) SetSplitTunnel(ctl splitController, storePath string) {
	store, err := openSplitTunnelStore(storePath)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("split tunnel: %v", err))
	}
	self, _ := os.Executable()
	st := s.splitTunnel
	st.store, st.ctl, st.self = store, ctl, self
	cfg, _ := store.snapshot()
	s.applySplitRules(cfg)
	s.refreshStrictReversePath(true)
	s.logs.Add(state.LogInfo, state.SourceDaemon, "split tunnel: "+splitSummary(cfg))
}

func splitSummary(cfg splitTunnelSettings) string {
	onOff := "off"
	if cfg.Enabled {
		onOff = "on"
	}
	return fmt.Sprintf("%s, %d apps, %d ranges", onOff, len(cfg.Apps), len(cfg.CIDRs))
}

func (s *Service) splitNeverBypass(protect []string) []string {
	never := slices.Clone(protect)
	if s.splitTunnel.self != "" {
		never = append(never, s.splitTunnel.self)
	}
	return never
}

// applySplitRules hands the engine the effective app rules; it resets flows that lose
// their exclusion before returning.
func (s *Service) applySplitRules(cfg splitTunnelSettings) {
	if ctl := s.splitTunnel.ctl; ctl != nil {
		ctl.SetRules(cfg.effectiveApps(), s.splitNeverBypass(cfg.Protect))
	}
}

// SplitTunnel is GET /split-tunnel; false when split tunnelling is not wired in.
func (s *Service) SplitTunnel() (splitTunnelView, bool) {
	store := s.splitTunnel.store
	if store == nil {
		return splitTunnelView{}, false
	}
	cfg, gen := store.snapshot()
	return s.splitTunnelView(cfg, gen), true
}

// UpdateSplitTunnel validates and saves new settings, applies the app rules live and
// leaves the ranges to the reconciler. It never takes opMu, so it answers at once.
func (s *Service) UpdateSplitTunnel(update splitTunnelUpdate) (splitTunnelView, []splitTunnelInvalid, error) {
	st := s.splitTunnel
	if st.store == nil {
		return splitTunnelView{}, nil, errSplitTunnelUnavailable
	}
	st.writeMu.Lock()
	defer st.writeMu.Unlock()

	old, oldGen := st.store.snapshot()
	protect := old.Protect
	if update.Protect != nil {
		protect = slices.Clone(*update.Protect)
	}
	invalid := validateSplitApps(update.Apps, s.splitNeverBypass(protect))
	cidrs, cidrInvalid := normalizeSplitCIDRList(update.CIDRs)
	invalid = append(invalid, cidrInvalid...)
	invalid = append(invalid, validateSplitProtect(protect)...)
	if len(invalid) > 0 {
		return splitTunnelView{}, invalid, nil
	}

	next := splitTunnelSettings{Enabled: update.Enabled, Apps: dedupeSplitApps(update.Apps), CIDRs: cidrs, Protect: protect}
	gen, err := st.store.save(next)
	if err != nil {
		s.logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("split tunnel: saving settings failed: %v", err))
		return splitTunnelView{}, nil, err
	}
	s.logs.Add(state.LogInfo, state.SourceDaemon, "split tunnel settings saved: "+splitSummary(next))
	s.applySplitRules(next)
	s.notePOSTKeptRoutes(old, oldGen, next, gen)
	s.kickSplitReconcile()
	return s.splitTunnelView(next, gen), nil, nil
}

// notePOSTKeptRoutes marks an edit that leaves the ranges alone as applied, since the
// app rules already went live; it only ever advances a session that was up to date.
func (s *Service) notePOSTKeptRoutes(old splitTunnelSettings, oldGen uint64, next splitTunnelSettings, gen uint64) {
	if !slices.Equal(old.effectiveCIDRs(), next.effectiveCIDRs()) {
		return
	}
	st := s.splitTunnel
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.applied.known && st.applied.gen == oldGen {
		st.applied.gen = gen
	}
}

func (s *Service) splitTunnelView(cfg splitTunnelSettings, gen uint64) splitTunnelView {
	view := splitTunnelView{
		Enabled:       cfg.Enabled,
		Apps:          append([]string{}, cfg.Apps...),
		CIDRs:         append([]string{}, cfg.CIDRs...),
		AppsSupported: s.splitTunnel.ctl != nil,
	}
	live := s.splitSessionLive()
	applied := s.appliedSplitSnapshot()
	view.Pending = live && applied.gen != gen
	view.CIDRsDropped = live && applied.dropped
	appsActive := false
	if ctl := s.splitTunnel.ctl; ctl != nil {
		cs := ctl.Status()
		appsActive = cs.AppsActive
		view.UnavailableReason = s.splitReason(cfg, cs)
	}
	current, _ := s.machine.Get()
	routed := current == state.StateConnected && applied.known && len(applied.routes) > 0
	view.Active = cfg.Enabled && (appsActive || routed)
	return view
}

// splitTunnelStatus is the /status block: counts only, never the lists themselves.
func (s *Service) splitTunnelStatus() *state.SplitTunnelStatus {
	store := s.splitTunnel.store
	if store == nil {
		return nil
	}
	cfg, gen := store.snapshot()
	out := &state.SplitTunnelStatus{Enabled: cfg.Enabled, AppCount: len(cfg.Apps), CIDRCount: len(cfg.CIDRs)}
	live := s.splitSessionLive()
	applied := s.appliedSplitSnapshot()
	out.Pending = live && applied.gen != gen
	out.CIDRsDropped = live && applied.dropped
	if ctl := s.splitTunnel.ctl; ctl != nil {
		cs := ctl.Status()
		out.AppsActive = cfg.Enabled && cs.AppsActive
		out.BypassFlows = cs.BypassTCP + cs.BypassUDP
		out.UnavailableReason = s.splitReason(cfg, cs)
	}
	return out
}

// splitReason prefers the engine's own reason; none matters until there are apps to
// exclude, and the engine keeps an old failure after the apps are gone.
func (s *Service) splitReason(cfg splitTunnelSettings, cs splittunnel.Status) string {
	if len(cfg.effectiveApps()) == 0 {
		return ""
	}
	if cs.UnavailableReason != "" {
		return cs.UnavailableReason
	}
	if s.strictReversePathOn() {
		return splittunnel.ReasonStrictReversePath
	}
	return ""
}

func (s *Service) strictReversePathOn() bool {
	st := s.splitTunnel
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.strictRP
}

// refreshStrictReversePath re-reads rp_filter on a network change, or every
// strictRPCheckEvery from the health loop.
func (s *Service) refreshStrictReversePath(force bool) {
	st := s.splitTunnel
	if st.store == nil || st.strictRPFn == nil {
		return
	}
	st.mu.Lock()
	due := force || time.Since(st.rpCheckedAt) >= strictRPCheckEvery
	if due {
		st.rpCheckedAt = time.Now()
	}
	st.mu.Unlock()
	if !due {
		return
	}
	strict, err := st.strictRPFn()
	st.mu.Lock()
	changed := err == nil && strict != st.strictRP
	if err == nil {
		st.strictRP = strict
	}
	st.mu.Unlock()
	if changed && strict {
		s.logs.Add(state.LogWarn, state.SourceDaemon, "split tunnel: strict reverse-path filtering (rp_filter=1) is on; replies to excluded apps will be dropped")
	}
}

// splitSessionLive is whether a session holds the lock for a profile: the states the
// reconciler may touch. An idle lock is not one.
func (s *Service) splitSessionLive() bool {
	s.profileMu.RLock()
	held := s.currentProfile != nil
	s.profileMu.RUnlock()
	if !held {
		return false
	}
	switch current, _ := s.machine.Get(); current {
	case state.StateConnected, state.StateConnecting, state.StateError:
		return true
	}
	return false
}

func (s *Service) appliedSplitSnapshot() appliedSplit {
	st := s.splitTunnel
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.applied
	out.routes = slices.Clone(out.routes)
	out.permits = slices.Clone(out.permits)
	return out
}

func (s *Service) updateAppliedSplit(update func(*appliedSplit)) {
	st := s.splitTunnel
	st.mu.Lock()
	defer st.mu.Unlock()
	update(&st.applied)
}

func (s *Service) resetAppliedSplit() {
	s.updateAppliedSplit(func(a *appliedSplit) { *a = appliedSplit{} })
}

func (s *Service) splitPending() bool {
	store := s.splitTunnel.store
	if store == nil || !s.splitSessionLive() {
		return false
	}
	return s.appliedSplitSnapshot().gen != store.generation()
}

// kickSplitReconcile wakes the reconciler; one pending kick is enough, so this never blocks.
func (s *Service) kickSplitReconcile() {
	select {
	case s.splitTunnel.kick <- struct{}{}:
	default:
	}
}

// kickSplitReconcileIfPending runs at the end of every session operation, which may
// have read the settings before a POST changed them.
func (s *Service) kickSplitReconcileIfPending() {
	if s.splitPending() {
		s.kickSplitReconcile()
	}
}

func (s *Service) splitReconcileLoop(ctx context.Context) {
	if s.splitTunnel.store == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.splitTunnel.kick:
		}
		s.reconcileSplit(ctx)
	}
}

// reconcileSplit brings the live session onto the stored ranges. It waits for opMu
// rather than skipping a busy turn, and any user operation may interrupt it.
func (s *Service) reconcileSplit(parent context.Context) {
	if s.splitTunnel.store == nil {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, splitApplyTimeout)
	defer cancel()
	defer s.registerCancel(cancel, true)()
	for range 4 {
		if !s.reconcileSplitOnce(ctx) {
			return
		}
	}
}

// reconcileSplitOnce applies the newest settings to the live session; true means it
// did, so the caller looks again for settings saved meanwhile.
func (s *Service) reconcileSplitOnce(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	profile, ok := s.getCurrentProfile()
	current, _ := s.machine.Get()
	if !ok || (current != state.StateConnected && current != state.StateError && current != state.StateConnecting) {
		return false
	}
	withDevice := current == state.StateConnected && s.deviceRunning(ctx, profile)
	return s.applySplitLive(ctx, profile, s.getSessionOpts().AllowLAN, withDevice)
}

// applySplitLive moves a live session to the stored ranges without touching the state
// machine: the lock permits old and new ranges while the routes move. opMu held.
func (s *Service) applySplitLive(ctx context.Context, profile state.Profile, allowLAN, withDevice bool) bool {
	if s.splitTunnel.store == nil {
		return false
	}
	desired, gen, dropped := s.desiredSplit(profile, allowLAN)
	prev := s.appliedSplitSnapshot()
	if prev.known && prev.gen == gen {
		return false
	}
	if prev.known && !prev.routesUnsure && slices.Equal(prev.routes, desired) {
		permits, err := s.setSplitPermits(ctx, prev.permits, desired)
		s.updateAppliedSplit(func(a *appliedSplit) { a.permits = permits })
		if err != nil {
			s.splitApplyFailed(fmt.Errorf("kill switch: %w", err))
			return false
		}
		s.splitApplied(gen, dropped)
		return true
	}

	// No route can move without a live device, so the lock goes straight to the new
	// ranges; the next bring-up routes them, and until then the session stays pending.
	if !withDevice {
		permits, err := s.setSplitPermits(ctx, prev.permits, desired)
		s.updateAppliedSplit(func(a *appliedSplit) { a.permits = permits })
		if err != nil {
			s.splitApplyFailed(fmt.Errorf("kill switch: %w", err))
		}
		return false
	}
	union := unionPrefixes(prev.permits, desired)
	permits, err := s.setSplitPermits(ctx, prev.permits, union)
	s.updateAppliedSplit(func(a *appliedSplit) { a.permits = permits })
	if err != nil {
		s.splitApplyFailed(fmt.Errorf("kill switch: %w", err))
		return false
	}
	applier, ok := s.wg.(wgAllowedIPsApplier)
	if !ok {
		s.splitApplyFailed(errors.New("this tunnel cannot move its routes in place; the next reconnect applies them"))
		return false
	}
	wireGuardProfile, err := wireGuardProfileFor(profile, allowLAN, desired)
	if err == nil {
		err = applier.ApplyAllowedIPs(ctx, wireGuardProfile)
	}
	if err != nil {
		s.updateAppliedSplit(func(a *appliedSplit) { a.routesUnsure = true })
		s.splitApplyFailed(fmt.Errorf("tunnel routes: %w", err))
		return false
	}
	s.updateAppliedSplit(func(a *appliedSplit) { a.known, a.routes, a.routesUnsure = true, slices.Clone(desired), false })
	permits, err = s.setSplitPermits(ctx, union, desired)
	s.updateAppliedSplit(func(a *appliedSplit) { a.permits = permits })
	if err != nil {
		s.splitApplyFailed(fmt.Errorf("kill switch: %w", err))
		return false
	}
	s.splitApplied(gen, dropped)
	s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("split tunnel: %d excluded ranges applied to the live tunnel", len(desired)))
	return true
}

func (s *Service) splitApplied(gen uint64, dropped bool) {
	s.updateAppliedSplit(func(a *appliedSplit) { a.gen, a.dropped = gen, dropped })
	st := s.splitTunnel
	st.mu.Lock()
	st.retryAt = time.Time{}
	st.mu.Unlock()
}

func (s *Service) splitApplyFailed(err error) {
	st := s.splitTunnel
	st.mu.Lock()
	st.retryAt = time.Now().Add(splitRetryDelay)
	st.mu.Unlock()
	s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("split tunnel: applying the excluded ranges failed, retrying: %v", err))
}

func (s *Service) splitRetryDue() bool {
	st := s.splitTunnel
	st.mu.Lock()
	defer st.mu.Unlock()
	return !time.Now().Before(st.retryAt)
}

// desiredSplit is the stored ranges for this session, or none when carving them would
// leave the tunnel too many routes (dropped).
func (s *Service) desiredSplit(profile state.Profile, allowLAN bool) ([]netip.Prefix, uint64, bool) {
	store := s.splitTunnel.store
	if store == nil {
		return nil, 0, false
	}
	cfg, gen := store.snapshot()
	cidrs := cfg.effectiveCIDRs()
	if len(cidrs) == 0 {
		return nil, gen, false
	}
	if !splitRoutesFit(profile, allowLAN, cidrs) {
		return nil, gen, true
	}
	return cidrs, gen, false
}

// splitCIDRsFor is the ranges a bring-up is expected to route around the tunnel.
func (s *Service) splitCIDRsFor(profile state.Profile, allowLAN bool) []netip.Prefix {
	cidrs, _, _ := s.desiredSplit(profile, allowLAN)
	return cidrs
}

// syncSplitForBringUp permits the stored ranges through the lock and returns those the
// session may route around the tunnel: never more than the lock lets out. opMu held.
func (s *Service) syncSplitForBringUp(ctx context.Context, profile state.Profile, allowLAN bool) []netip.Prefix {
	if s.splitTunnel.store == nil {
		return nil
	}
	desired, gen, dropped := s.desiredSplit(profile, allowLAN)
	if dropped {
		cfg, _ := s.splitTunnel.store.snapshot()
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
			"split tunnel: the %d excluded ranges need more tunnel routes than this server allows; they stay in the tunnel", len(cfg.CIDRs)))
	}
	prev := s.appliedSplitSnapshot()
	permits, err := s.setSplitPermits(ctx, prev.permits, desired)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
			"split tunnel: the kill switch did not permit the excluded ranges, so %d of %d stay in the tunnel: %v", len(desired)-len(permits), len(desired), err))
	}
	routes := permits
	if len(routes) > 0 && !splitRoutesFit(profile, allowLAN, routes) {
		routes = nil
		var narrowErr error
		permits, narrowErr = s.setSplitPermits(ctx, permits, nil)
		err = errors.Join(err, narrowErr)
	}
	s.updateAppliedSplit(func(a *appliedSplit) {
		appliedGen := a.gen
		if err != nil {
			// Pending whatever was recorded before, so the reconciler asks the lock again.
			appliedGen = 0
		}
		*a = appliedSplit{gen: appliedGen, known: true, routes: slices.Clone(routes), permits: permits, dropped: dropped}
	})
	if err == nil {
		s.splitApplied(gen, dropped)
	}
	return routes
}

// setSplitPermits asks the lock for want and reports what it surely permits now: on
// failure only what both sets share, while the lock may still hold more until asked again.
func (s *Service) setSplitPermits(ctx context.Context, prev, want []netip.Prefix) ([]netip.Prefix, error) {
	st := s.splitTunnel
	st.mu.Lock()
	settled := !st.lockUnsure && slices.Equal(st.lockRanges, want)
	st.mu.Unlock()
	if settled && slices.Equal(prev, want) {
		return slices.Clone(want), nil
	}
	permitter, ok := s.killSwitch.(platform.SplitTunnelPermitter)
	if !ok {
		if len(want) == 0 {
			return nil, nil
		}
		return intersectPrefixes(prev, want), errors.New("the kill switch cannot permit split-tunnel ranges")
	}
	err := permitter.SetSplitCIDRs(ctx, prefixStrings(want))
	st.mu.Lock()
	st.lockRanges, st.lockUnsure = slices.Clone(want), err != nil
	st.mu.Unlock()
	if err != nil {
		return intersectPrefixes(prev, want), err
	}
	return slices.Clone(want), nil
}

// clearSplitPermits drops the range permits a session asked for, so an idle lock
// never carries them; a lock known to hold none is left alone.
func (s *Service) clearSplitPermits(ctx context.Context) error {
	if s.splitTunnel.store == nil {
		return nil
	}
	defer s.resetAppliedSplit()
	_, err := s.setSplitPermits(ctx, nil, nil)
	return err
}

// splitNetworkChanged lets the engine re-pin its off-tunnel sockets; a no-op unless the
// physical interface really moved.
func (s *Service) splitNetworkChanged() {
	if s.splitTunnel.store == nil {
		return
	}
	if ctl := s.splitTunnel.ctl; ctl != nil {
		ctl.NetworkChanged()
	}
	s.refreshStrictReversePath(true)
}

// splitTick is the health loop's share: the engine's network check, and a retry of
// ranges the live session does not carry yet.
func (s *Service) splitTick() {
	if s.splitTunnel.store == nil {
		return
	}
	// NetworkChanged also retries a failed egress permit, which has its own backoff; with
	// no permit there are no off-tunnel sockets to re-pin either.
	if ctl := s.splitTunnel.ctl; ctl != nil && ctl.Status().UnavailableReason != splittunnel.ReasonPermitFailed {
		ctl.NetworkChanged()
	}
	s.refreshStrictReversePath(false)
	if current, _ := s.machine.Get(); current == state.StateConnected && s.splitPending() && s.splitRetryDue() {
		s.kickSplitReconcile()
	}
}

// carveAllowedIPs subtracts the LAN set (allowLAN) and the split ranges from the
// AllowedIPs; resolvers and the tunnel's own address stay routed through it.
func carveAllowedIPs(wireGuardProfile state.WireGuardProfile, allowLAN bool, splitCIDRs []netip.Prefix) (string, error) {
	text := wireGuardProfile.ConfigText
	if len(splitCIDRs) == 0 {
		if !allowLAN {
			return text, nil
		}
		return wg.TransformWGConfigExcludeLAN(text)
	}
	var v4, v6 []netip.Prefix
	if allowLAN {
		v4, v6 = wg.LANExcludePrefixes(), wg.LANExcludePrefixesV6()
	}
	v4 = append(v4, splitCIDRs...)
	return wg.TransformWGConfigExclude(text, v4, v6, resolverPrefixes(wireGuardProfile))
}

func resolverPrefixes(wireGuardProfile state.WireGuardProfile) []netip.Prefix {
	var out []netip.Prefix
	for _, raw := range wg.Resolvers(wireGuardProfile) {
		if addr, err := netip.ParseAddr(raw); err == nil {
			addr = addr.Unmap()
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return out
}

// splitRoutesFit is the fail-toward-tunnel guard: ranges whose carve would leave too
// many routes, or no IPv4 route at all, all stay in the tunnel.
func splitRoutesFit(profile state.Profile, allowLAN bool, splitCIDRs []netip.Prefix) bool {
	text, err := carveAllowedIPs(withTransportBypassHosts(profile), allowLAN, splitCIDRs)
	if err != nil {
		return false
	}
	count, err := wg.CountAllowedIPv4(text)
	if err != nil || count == 0 {
		return false
	}
	return count <= maxSplitRoutes+splitRouteBudgetPerEndpoint*splitEndpointCount(profile)
}

// splitEndpointCount is the WireGuard endpoint plus every address the session permits
// outside the tunnel, each of which the desktop carves out of AllowedIPs.
func splitEndpointCount(profile state.Profile) int {
	seen := map[string]struct{}{}
	for _, ip := range ipLiterals(killSwitchPermits(profile)) {
		seen[ip] = struct{}{}
	}
	return len(seen) + 1
}

func prefixStrings(prefixes []netip.Prefix) []string {
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, p.String())
	}
	return out
}

// sortedPrefixes is the canonical form every applied set is kept in, so sets compare
// with slices.Equal.
func sortedPrefixes(prefixes []netip.Prefix) []netip.Prefix {
	out := slices.Clone(prefixes)
	slices.SortFunc(out, netip.Prefix.Compare)
	return slices.Compact(out)
}

func unionPrefixes(a, b []netip.Prefix) []netip.Prefix {
	return sortedPrefixes(append(slices.Clone(a), b...))
}

func intersectPrefixes(a, b []netip.Prefix) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range b {
		if slices.Contains(a, p) {
			out = append(out, p)
		}
	}
	return sortedPrefixes(out)
}
