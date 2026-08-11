package transport

import (
	"context"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// StartFn starts one transport and rebinds the WireGuard endpoint to its
// loopback port. Carrying a handshake over it is the caller's half.
type StartFn func(ctx context.Context, profile *state.Profile, wireGuardProfile *state.WireGuardProfile) error

// Candidate is one rung of the cascade.
type Candidate struct {
	Kind  string
	Start StartFn
}

// Availability is why a platform can or cannot attempt a transport. Three
// cases, because each produces a different user-facing error.
type Availability int

const (
	Available Availability = iota
	// NotConfigured: the profile carries no credentials for it.
	NotConfigured
	// Unavailable: the platform gates it off (release flag, or cannot link it).
	Unavailable
)

// Starter reports whether a platform can attempt a transport for a profile.
// Platforms differ legitimately: only desktop can link NaiveProxy.
type Starter interface {
	StarterFor(profile *state.Profile, kind string) (StartFn, Availability)
}

// Memory looks up the transport last known to work on a network.
type Memory interface {
	Lookup(networkKey string) (string, bool)
}

// AutoCascadeOrder is the auto-mode fallback order. Consecutive rungs hide in
// deliberately different ways, so one block rarely stops the next attempt.
var AutoCascadeOrder = []string{"cloak", "reality", "shadowsocks", "hysteria2", "naive", "snowflake"}

// knownKinds keeps an explicit request for something else an "unknown"
// error rather than silently reporting it as unconfigured.
var knownKinds = map[string]struct{}{
	"cloak": {}, "reality": {}, "shadowsocks": {},
	"hysteria2": {}, "naive": {}, "snowflake": {},
}

// IsAuto reports whether a selection means "walk the cascade".
func IsAuto(preferred string) bool {
	return preferred == "" || preferred == "auto"
}

// Select returns the ordered transports to attempt. Auto walks
// AutoCascadeOrder; a named transport yields one candidate or an error.
func Select(profile *state.Profile, preferred string, s Starter) ([]Candidate, error) {
	if IsAuto(preferred) {
		return autoCascade(profile, s), nil
	}
	if _, ok := knownKinds[preferred]; !ok {
		return nil, fmt.Errorf("unknown transport %q", preferred)
	}
	start, availability := s.StarterFor(profile, preferred)
	switch availability {
	case Available:
		return []Candidate{{Kind: preferred, Start: start}}, nil
	case Unavailable:
		return nil, fmt.Errorf("%s transport is temporarily unavailable", preferred)
	default:
		return nil, fmt.Errorf("%s transport requested but this profile has no %s configuration", preferred, preferred)
	}
}

// autoCascade keeps only the transports this platform and profile can attempt.
func autoCascade(profile *state.Profile, s Starter) []Candidate {
	candidates := make([]Candidate, 0, len(AutoCascadeOrder))
	for _, kind := range AutoCascadeOrder {
		if start, availability := s.StarterFor(profile, kind); availability == Available {
			candidates = append(candidates, Candidate{Kind: kind, Start: start})
		}
	}
	return candidates
}

// ReorderByMemory promotes the transport that last worked on this network to
// the front, in auto mode only. Returns the promoted kind, empty if none.
func ReorderByMemory(candidates []Candidate, preferred, networkKey string, m Memory) ([]Candidate, string) {
	if m == nil || len(candidates) < 2 || !IsAuto(preferred) {
		return candidates, ""
	}
	remembered, ok := m.Lookup(networkKey)
	if !ok {
		return candidates, ""
	}
	idx := -1
	for i, candidate := range candidates {
		if candidate.Kind == remembered {
			idx = i
			break
		}
	}
	if idx <= 0 {
		// Not configured for this profile, or already first.
		return candidates, ""
	}
	reordered := make([]Candidate, 0, len(candidates))
	reordered = append(reordered, candidates[idx])
	reordered = append(reordered, candidates[:idx]...)
	reordered = append(reordered, candidates[idx+1:]...)
	return reordered, remembered
}
