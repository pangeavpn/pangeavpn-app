// Package splittunnel keeps traffic of excluded apps out of the tunnel: it wraps the
// OS TUN, attributes new flows to processes and re-originates excluded ones off-tunnel.
package splittunnel

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

type ControllerOptions struct {
	NewClassifier   func() (procmatch.Classifier, error)
	NewEgress       func() (egress.Dialer, error)
	SetEgressPermit func(ctx context.Context, on bool) error
	Logf            func(format string, args ...any)
}

// Unavailable reasons reported in Status (stable; surfaced to the desktop app).
const (
	ReasonClassifierFailed  = "classifierFailed"
	ReasonEgressFailed      = "egressFailed"
	ReasonPermitFailed      = "permitFailed"
	ReasonStackFailed       = "stackFailed"
	ReasonStrictReversePath = "strictReversePath"
)

type Status struct {
	AppsActive        bool
	UnavailableReason string
	BypassTCP         int
	BypassUDP         int
	Counters          Counters
}

type Counters struct {
	Classified      uint64
	Bypassed        uint64
	LookupFails     uint64
	PendingTimeouts uint64
	UDPDrops        uint64
	OutDrops        uint64
	Revalidated     uint64
	Torn            uint64
}

func (c *Counters) add(o Counters) {
	c.Classified += o.Classified
	c.Bypassed += o.Bypassed
	c.LookupFails += o.LookupFails
	c.PendingTimeouts += o.PendingTimeouts
	c.UDPDrops += o.UDPDrops
	c.OutDrops += o.OutDrops
	c.Revalidated += o.Revalidated
	c.Torn += o.Torn
}

var _ wg.SplitTunnelHook = (*Controller)(nil)

const (
	engineNone = iota
	engineStarting
	engineReady
	engineFailed
)

const (
	permitRetry      = 30 * time.Second
	permitCallBudget = 15 * time.Second
	engineRetry      = 30 * time.Second
)

// Controller owns the rules, the lazily created classifier and egress dialer, the
// kill-switch egress permit and the set of live wrapped devices.
type Controller struct {
	opts    ControllerOptions
	lim     limits
	log     *rateLog
	compile func(apps, never []string) (*ruleSet, []procmatch.RuleError)

	ctx    context.Context
	cancel context.CancelFunc

	setMu sync.Mutex

	mu            sync.Mutex
	closed        bool
	devices       map[*Device]struct{}
	rules         *ruleSet
	rulesOn       bool
	retired       Counters
	engState      int
	engReason     string
	engFailedAt   time.Time
	classifier    procmatch.Classifier
	dialer        egress.Dialer
	permitStarted bool
	permitDone    chan struct{}

	permitted    atomic.Bool
	permitFailed atomic.Bool
	engDown      atomic.Bool
	permitKick   chan struct{}

	netMu    sync.Mutex
	netGen   atomic.Uint64
	lastHint atomic.Int64
	upAddrs  func() (map[netip.Addr]struct{}, error)
}

func NewController(opts ControllerOptions) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{
		opts:       opts,
		lim:        defaultLimits,
		log:        newRateLog(opts.Logf),
		compile:    compileRuleSet,
		ctx:        ctx,
		cancel:     cancel,
		devices:    make(map[*Device]struct{}),
		permitKick: make(chan struct{}, 1),
		upAddrs:    interfaceAddrs,
	}
}

// WrapTUN implements wg.SplitTunnelHook. A tunnel without an IPv4 address stays unwrapped.
func (c *Controller) WrapTUN(dev tun.Device, info wg.TunnelInfo) tun.Device {
	addr := firstIPv4(info.Addresses)
	if !addr.IsValid() {
		c.log.printf("split tunnel: tunnel has no IPv4 address; app exclusion is off for it")
		return dev
	}
	d := newDevice(c, dev, info, addr)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return dev
	}
	c.devices[d] = struct{}{}
	d.applyRules(c.rules)
	c.updatePermittedLocked()
	c.mu.Unlock()
	c.kickPermit()
	return d
}

// SetRules replaces the excluded-app rules (empty apps turns app exclusion off). It
// returns once flows that lost their exclusion have been reset.
func (c *Controller) SetRules(apps []string, neverBypass []string) []procmatch.RuleError {
	rs, errs := c.compile(apps, neverBypass)
	if len(errs) > 0 {
		c.log.printf("split tunnel: %d app rules refused (%s)", len(errs), ruleErrorSummary(errs))
	}
	c.setMu.Lock()
	defer c.setMu.Unlock()
	c.mu.Lock()
	if c.closed || c.rules.sameAs(rs) {
		c.mu.Unlock()
		c.retryEngines()
		return errs
	}
	c.rules = rs
	// Observed under mu so a concurrent startEngines can't hand the classifier older rules.
	observeRules(c.classifier, rs)
	if rs.empty() {
		c.updatePermittedLocked()
	}
	devs := c.deviceListLocked()
	c.mu.Unlock()

	for _, d := range devs {
		d.applyRules(rs)
	}
	c.mu.Lock()
	c.rulesOn = !rs.empty()
	c.updatePermittedLocked()
	c.mu.Unlock()
	c.kickPermit()
	c.retryEngines()
	c.log.printf("split tunnel: %d app rules active", rs.apps)
	return errs
}

func ruleErrorSummary(errs []procmatch.RuleError) string {
	const show = 8
	parts := make([]string, 0, show+1)
	for i, e := range errs {
		if i == show {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, fmt.Sprintf("%d:%s", e.Index, e.Code))
	}
	return strings.Join(parts, " ")
}

func observeRules(cls procmatch.Classifier, rs *ruleSet) {
	if o, ok := cls.(procmatch.RulesObserver); ok && rs != nil {
		o.ObserveRules(rs.rules)
	}
}

// Status is lock-light: it never waits on the engines' flow tables.
func (c *Controller) Status() Status {
	var st Status
	c.mu.Lock()
	st.Counters = c.retired
	rs := c.rules
	reason := c.engReason
	pumping, stackFailed := false, false
	for d := range c.devices {
		st.Counters.add(d.stats.snapshot())
		st.BypassTCP += int(d.stats.bypassTCP.Load())
		st.BypassUDP += int(d.stats.bypassUDP.Load())
		if d.pumping.Load() && !d.tunnelOnly.Load() {
			pumping = true
		}
		if d.stackFailed.Load() {
			stackFailed = true
		}
	}
	c.mu.Unlock()
	switch {
	case reason != "":
		st.UnavailableReason = reason
	case c.permitFailed.Load():
		st.UnavailableReason = ReasonPermitFailed
	case stackFailed:
		st.UnavailableReason = ReasonStackFailed
	}
	st.AppsActive = !rs.empty() && pumping && c.permitted.Load() && !c.engDown.Load()
	return st
}

// Close resets bypassed flows, withdraws the egress permit and releases the classifier
// and egress dialer. Devices keep running tunnel-only until wireguard-go closes them.
func (c *Controller) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.rulesOn = false
	c.permitted.Store(false)
	devs := c.deviceListLocked()
	cls, dl := c.classifier, c.dialer
	c.classifier, c.dialer = nil, nil
	started, done := c.permitStarted, c.permitDone
	c.mu.Unlock()
	for _, d := range devs {
		d.tunnelOnly.Store(true)
		d.resetEngine()
	}
	c.cancel()
	if started {
		<-done
	}
	var errs []error
	if cls != nil {
		errs = append(errs, cls.Close())
	}
	if dl != nil {
		errs = append(errs, dl.Close())
	}
	return errors.Join(errs...)
}

func (c *Controller) deviceListLocked() []*Device {
	devs := make([]*Device, 0, len(c.devices))
	for d := range c.devices {
		devs = append(devs, d)
	}
	return devs
}

func (c *Controller) deregister(d *Device) {
	c.mu.Lock()
	if _, ok := c.devices[d]; ok {
		delete(c.devices, d)
		c.retired.add(d.stats.snapshot())
	}
	c.updatePermittedLocked()
	c.mu.Unlock()
	c.kickPermit()
}

// desiredLocked is whether the kill switch should let the daemon's bypass sockets out;
// rulesOn changes only after a rule change's teardown has run, so no withdrawal overtakes it.
func (c *Controller) desiredLocked() bool {
	return !c.closed && c.rulesOn && len(c.devices) > 0
}

// gateLocked is whether new flows may bypass: the permit is wanted and the rules in force
// are non-empty (emptied rules close the gate before their teardown runs).
func (c *Controller) gateLocked() bool {
	return c.desiredLocked() && !c.rules.empty()
}

// updatePermittedLocked closes the gate at once when the permit is no longer wanted;
// opening it waits for the kill switch unless no permit hook is configured.
func (c *Controller) updatePermittedLocked() {
	if !c.gateLocked() {
		c.permitted.Store(false)
		return
	}
	if c.opts.SetEgressPermit == nil {
		c.permitted.Store(true)
	}
}

func (c *Controller) kickPermit() {
	if c.opts.SetEgressPermit == nil {
		return
	}
	c.mu.Lock()
	if !c.permitStarted && !c.closed {
		c.permitStarted = true
		c.permitDone = make(chan struct{})
		go c.permitLoop()
	}
	c.mu.Unlock()
	select {
	case c.permitKick <- struct{}{}:
	default:
	}
}

// permitLoop is the single worker that applies permit changes to the kill switch in order;
// dirty covers a failed grant the kill switch may still have recorded.
func (c *Controller) permitLoop() {
	defer close(c.permitDone)
	applied, dirty := false, false
	var retry <-chan time.Time
	for {
		select {
		case <-c.ctx.Done():
			if dirty {
				ctx, cancel := context.WithTimeout(context.Background(), permitCallBudget)
				if err := c.opts.SetEgressPermit(ctx, false); err != nil {
					c.log.printf("split tunnel: withdrawing egress permit failed: %v", err)
				}
				cancel()
			}
			return
		case <-c.permitKick:
		case <-retry:
		}
		retry = nil
		c.mu.Lock()
		desired, grant := c.desiredLocked(), c.gateLocked()
		c.mu.Unlock()
		switch {
		case grant && !applied:
			dirty = true
			if err := c.callPermit(true); err != nil {
				c.permitFailed.Store(true)
				c.log.limited("permit", "split tunnel: egress permit failed: %v", err)
				retry = time.After(permitRetry)
				continue
			}
			applied = true
			// Before the gate opens, so the egress route a dialer may need is already in place.
			_ = c.refreshNetwork()
		case !desired && dirty:
			if err := c.callPermit(false); err != nil {
				c.log.limited("permit-off", "split tunnel: withdrawing egress permit failed: %v", err)
				retry = time.After(permitRetry)
				continue
			}
			applied, dirty = false, false
			_ = c.refreshNetwork()
		}
		c.permitFailed.Store(false)
		if applied {
			c.mu.Lock()
			if c.gateLocked() {
				c.permitted.Store(true)
			}
			c.mu.Unlock()
		}
	}
}

func (c *Controller) callPermit(on bool) error {
	ctx, cancel := context.WithTimeout(c.ctx, permitCallBudget)
	defer cancel()
	return c.opts.SetEgressPermit(ctx, on)
}

// ensureEngines starts creating the classifier and egress dialer when a device first pumps,
// and again once engineRetry has passed since a failed start (apps stay tunnelled until then).
func (c *Controller) ensureEngines() {
	c.mu.Lock()
	if c.engState == engineFailed && !c.rules.empty() && len(c.devices) > 0 && time.Since(c.engFailedAt) >= engineRetry {
		c.engState = engineNone
	}
	if c.closed || c.engState != engineNone {
		c.mu.Unlock()
		return
	}
	c.engState = engineStarting
	needCls, needEg := c.classifier == nil, c.dialer == nil
	c.mu.Unlock()
	go c.startEngines(needCls, needEg)
}

// retryEngines lets a failed classifier or egress start recover without a rule edit; it
// never starts engines that no pump has asked for yet.
func (c *Controller) retryEngines() {
	c.mu.Lock()
	failed := c.engState == engineFailed
	c.mu.Unlock()
	if failed {
		c.ensureEngines()
	}
}

func (c *Controller) startEngines(needCls, needEg bool) {
	var (
		cls    procmatch.Classifier
		dl     egress.Dialer
		reason string
	)
	if needCls {
		var err error
		if c.opts.NewClassifier == nil {
			err = procmatch.ErrUnsupported
		} else {
			cls, err = newSafely(c.opts.NewClassifier)
		}
		if err == nil && cls == nil {
			err = procmatch.ErrUnsupported
		}
		if err != nil {
			reason = ReasonClassifierFailed
			c.log.limited("classifier-init", "split tunnel: app lookup unavailable: %v", err)
		}
	}
	if needEg {
		var err error
		if c.opts.NewEgress == nil {
			err = egress.ErrUnsupported
		} else {
			dl, err = newSafely(c.opts.NewEgress)
		}
		if err == nil && dl == nil {
			err = egress.ErrUnsupported
		}
		if err != nil {
			if reason == "" {
				reason = ReasonEgressFailed
			}
			c.log.limited("egress-init", "split tunnel: off-tunnel egress unavailable: %v", err)
		}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		closeIf(cls)
		closeIf(dl)
		return
	}
	if cls != nil {
		c.classifier = cls
	}
	if dl != nil {
		c.dialer = dl
	}
	if reason != "" {
		c.engState = engineFailed
		c.engFailedAt = time.Now()
		c.engReason = reason
		c.engDown.Store(true)
	} else {
		c.engState = engineReady
		c.engReason = ""
		c.engDown.Store(false)
	}
	if cls != nil {
		observeRules(cls, c.rules)
	}
	devs := c.deviceListLocked()
	c.mu.Unlock()
	if reason == "" {
		c.log.printf("split tunnel: app lookup and off-tunnel egress ready")
	}
	for _, d := range devs {
		d.kickWorker()
	}
}

func newSafely[T any](fn func() (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("init panicked: %v", r)
		}
	}()
	return fn()
}

func closeIf[T interface{ Close() error }](v T) {
	if any(v) != nil {
		_ = v.Close()
	}
}

func (c *Controller) engines() (procmatch.Classifier, egress.Dialer, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.classifier, c.dialer, c.engState
}

func (c *Controller) egressDialer() egress.Dialer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dialer
}
