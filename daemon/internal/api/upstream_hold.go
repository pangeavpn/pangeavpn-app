package api

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reach"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// upstreamHoldLimit caps how long a silent network parks recovery, so a hub outage
// or a fresh block can delay a server rotation but never strand the session.
const upstreamHoldLimit = 90 * time.Second

// upstreamProbeGaps paces hub probes while holding (the last repeats), each
// jittered ±30% so clients behind one NAT do not probe in step.
var upstreamProbeGaps = []time.Duration{2 * time.Second, 3 * time.Second, 5 * time.Second, 8 * time.Second, 10 * time.Second}

func (s *Service) enterUpstreamHold(profileID string) {
	now := time.Now()
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.upstreamHoldProfile = profileID
	s.upstreamHoldUntil = now.Add(upstreamHoldLimit)
	s.upstreamProbes = 0
	s.upstreamProbeAt = now.Add(jitterGap(upstreamProbeGaps[0]))
}

func (s *Service) upstreamHoldActive() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return s.upstreamHoldProfile != ""
}

func (s *Service) clearUpstreamHold() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.clearUpstreamHoldLocked()
}

func (s *Service) clearUpstreamHoldLocked() {
	s.upstreamHoldProfile = ""
	s.upstreamHoldUntil = time.Time{}
	s.upstreamProbeAt = time.Time{}
	s.upstreamProbes = 0
}

func (s *Service) scheduleUpstreamProbe() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.upstreamProbes++
	gap := upstreamProbeGaps[min(s.upstreamProbes, len(upstreamProbeGaps)-1)]
	s.upstreamProbeAt = time.Now().Add(jitterGap(gap))
}

func jitterGap(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.7 + 0.6*rand.Float64()))
}

// sessionMoved reports whether the live session is no longer profileID: a Switch
// or Disconnect landed while a hub probe ran without opMu.
func (s *Service) sessionMoved(profileID string) bool {
	current, ok := s.getCurrentProfile()
	return !ok || current.ID != profileID
}

// holdForSilentNetwork parks a dead session when the hub is silent too: that is
// the network's fault, which neither the cascade nor a new server can fix.
func (s *Service) holdForSilentNetwork(ctx context.Context, profile state.Profile, what string) bool {
	verdict, summary := s.reachVerdictFor(ctx, profile)
	if s.sessionMoved(profile.ID) {
		return false
	}
	switch verdict {
	case reachOffline:
		s.enterUpstreamHold(profile.ID)
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
			"%s and the hub is silent too (%s); waiting up to %s for the network", what, summary, upstreamHoldLimit))
		return true
	case reachOnline:
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("%s, but the hub answered (%s); the network works", what, summary))
	}
	return false
}

// holdIfNetworkSilent keeps an exhausted cascade from reading as a blocked server,
// the app's cue to rotate; true also means a newer session owns the state now.
func (s *Service) holdIfNetworkSilent(ctx context.Context, profile state.Profile, err error) bool {
	if !errors.Is(err, ErrTransportExhausted) || hostNetworkUnreachable(err) || s.upstreamHoldActive() {
		return false
	}
	held := s.holdForSilentNetwork(ctx, profile, "every transport failed")
	if s.sessionMoved(profile.ID) {
		return true
	}
	if !held {
		return false
	}
	if !s.machine.CompareAndSet([]state.DaemonState{state.StateConnected, state.StateError}, state.StateError, offlineHoldDetail) {
		s.clearUpstreamHold()
	}
	return true
}

// tickUpstreamHold runs the hold for one health tick and reports whether it owned
// the tick; nothing else in the health check may run while it does.
func (s *Service) tickUpstreamHold(ctx context.Context) bool {
	s.recoveryMu.Lock()
	heldID, until, probeAt := s.upstreamHoldProfile, s.upstreamHoldUntil, s.upstreamProbeAt
	s.recoveryMu.Unlock()
	if heldID == "" {
		return false
	}
	current, _ := s.machine.Get()
	profile, ok := s.getCurrentProfile()
	if !ok || profile.ID != heldID || (current != state.StateConnected && current != state.StateError) {
		s.clearUpstreamHold()
		return false
	}
	// A lock torn down under the session is a leak; hand the tick back so the
	// health check's own kill-switch guard sees it.
	if !s.killSwitch.Active() {
		s.clearUpstreamHold()
		return false
	}
	now := time.Now()
	if !now.Before(until) {
		s.clearUpstreamHold()
		s.reachBaseline.forget(s.currentNetworkKey())
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("the network is still silent after %s; trying every transport", upstreamHoldLimit))
		s.attemptSessionRebuild(ctx, profile, "tunnel stopped carrying traffic")
		return true
	}
	if now.Before(probeAt) || s.noPhysicalRoute() {
		return true
	}
	verdict, summary := s.reachVerdictFor(ctx, profile)
	if s.sessionMoved(heldID) {
		s.clearUpstreamHold()
		return true
	}
	if verdict == reachOffline {
		s.scheduleUpstreamProbe()
		return true
	}
	s.clearUpstreamHold()
	if verdict == reachOnline {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("the network is back (%s); rebuilding the tunnel", summary))
	} else {
		s.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("the hub's routes changed (%s); trying every transport", summary))
	}
	s.attemptSessionRebuild(ctx, profile, "tunnel stopped carrying traffic")
	return true
}

// refreshReachBaseline proves, at most once per refresh window per network, which
// routes reach the hub here, so a later silence on them means something.
func (s *Service) refreshReachBaseline(network string, profile state.Profile) {
	if s.reachProbe == nil || network == "" || s.reachBaseline.fresh(network) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*reach.Timeout)
		defer cancel()
		s.reachVerdictFor(ctx, profile)
	}()
}
