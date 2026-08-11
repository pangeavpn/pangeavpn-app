package transport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// fakeStarter reports a fixed availability per kind, standing in for a
// platform's real gating rules.
type fakeStarter map[string]transport.Availability

func (f fakeStarter) StarterFor(_ *state.Profile, kind string) (transport.StartFn, transport.Availability) {
	availability, ok := f[kind]
	if !ok {
		return nil, transport.NotConfigured
	}
	if availability != transport.Available {
		return nil, availability
	}
	return func(context.Context, *state.Profile, *state.WireGuardProfile) error { return nil }, transport.Available
}

// allAvailable is a starter where every known transport can be attempted.
func allAvailable() fakeStarter {
	return fakeStarter{
		"cloak": transport.Available, "reality": transport.Available,
		"shadowsocks": transport.Available, "hysteria2": transport.Available,
		"naive": transport.Available, "snowflake": transport.Available,
	}
}

func kinds(candidates []transport.Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.Kind)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSelectAutoFollowsCascadeOrder(t *testing.T) {
	got, err := transport.Select(&state.Profile{}, "auto", allAvailable())
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !equal(kinds(got), transport.AutoCascadeOrder) {
		t.Fatalf("got %v, want %v", kinds(got), transport.AutoCascadeOrder)
	}
}

func TestSelectAutoTreatsEmptyStringAsAuto(t *testing.T) {
	got, err := transport.Select(&state.Profile{}, "", allAvailable())
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !equal(kinds(got), transport.AutoCascadeOrder) {
		t.Fatalf("got %v, want full cascade", kinds(got))
	}
}

func TestSelectAutoSkipsUnavailableAndKeepsOrder(t *testing.T) {
	// Mobile's real shape: no naive, no snowflake.
	starter := fakeStarter{
		"cloak": transport.Available, "reality": transport.Available,
		"shadowsocks": transport.Available, "hysteria2": transport.Available,
		"naive": transport.Unavailable, "snowflake": transport.NotConfigured,
	}
	got, err := transport.Select(&state.Profile{}, "auto", starter)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	want := []string{"cloak", "reality", "shadowsocks", "hysteria2"}
	if !equal(kinds(got), want) {
		t.Fatalf("got %v, want %v", kinds(got), want)
	}
}

func TestSelectAutoNeverErrorsWhenNothingIsAvailable(t *testing.T) {
	got, err := transport.Select(&state.Profile{}, "auto", fakeStarter{})
	if err != nil {
		t.Fatalf("auto mode should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want no candidates", kinds(got))
	}
}

func TestSelectNamedTransport(t *testing.T) {
	got, err := transport.Select(&state.Profile{}, "reality", allAvailable())
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !equal(kinds(got), []string{"reality"}) {
		t.Fatalf("got %v, want [reality]", kinds(got))
	}
	if got[0].Start == nil {
		t.Fatal("candidate has no start func")
	}
}

func TestSelectNamedErrors(t *testing.T) {
	starter := fakeStarter{
		"cloak":     transport.Available,
		"reality":   transport.NotConfigured,
		"snowflake": transport.Unavailable,
	}
	tests := []struct {
		name      string
		preferred string
		want      string
	}{
		{"not configured", "reality", "reality transport requested but this profile has no reality configuration"},
		{"platform gated", "snowflake", "snowflake transport is temporarily unavailable"},
		{"unknown kind", "wireguard", `unknown transport "wireguard"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := transport.Select(&state.Profile{}, tt.preferred, starter)
			if err == nil {
				t.Fatal("expected an error")
			}
			if err.Error() != tt.want {
				t.Fatalf("got %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// fakeMemory returns a fixed remembered transport.
type fakeMemory struct {
	remembered string
}

func (m fakeMemory) Lookup(string) (string, bool) {
	if m.remembered == "" {
		return "", false
	}
	return m.remembered, true
}

func candidatesFor(kindList ...string) []transport.Candidate {
	out := make([]transport.Candidate, 0, len(kindList))
	for _, k := range kindList {
		out = append(out, transport.Candidate{Kind: k, Start: func(context.Context, *state.Profile, *state.WireGuardProfile) error {
			return errors.New("unused")
		}})
	}
	return out
}

func TestReorderByMemory(t *testing.T) {
	tests := []struct {
		name         string
		candidates   []string
		preferred    string
		remembered   string
		memory       transport.Memory
		want         []string
		wantPromoted string
	}{
		{
			name:         "promotes remembered transport",
			candidates:   []string{"cloak", "reality", "shadowsocks"},
			preferred:    "auto",
			remembered:   "shadowsocks",
			want:         []string{"shadowsocks", "cloak", "reality"},
			wantPromoted: "shadowsocks",
		},
		{
			name:       "already first is left alone",
			candidates: []string{"cloak", "reality"},
			preferred:  "auto",
			remembered: "cloak",
			want:       []string{"cloak", "reality"},
		},
		{
			name:       "remembered transport not in candidates",
			candidates: []string{"cloak", "reality"},
			preferred:  "auto",
			remembered: "naive",
			want:       []string{"cloak", "reality"},
		},
		{
			name:       "nothing remembered",
			candidates: []string{"cloak", "reality"},
			preferred:  "auto",
			remembered: "",
			want:       []string{"cloak", "reality"},
		},
		{
			name:       "named transport is never reordered",
			candidates: []string{"cloak", "reality"},
			preferred:  "reality",
			remembered: "reality",
			want:       []string{"cloak", "reality"},
		},
		{
			name:       "single candidate",
			candidates: []string{"cloak"},
			preferred:  "auto",
			remembered: "cloak",
			want:       []string{"cloak"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, promoted := transport.ReorderByMemory(
				candidatesFor(tt.candidates...), tt.preferred, "wifi-abc", fakeMemory{tt.remembered})
			if !equal(kinds(got), tt.want) {
				t.Fatalf("got %v, want %v", kinds(got), tt.want)
			}
			if promoted != tt.wantPromoted {
				t.Fatalf("promoted %q, want %q", promoted, tt.wantPromoted)
			}
		})
	}
}

func TestReorderByMemoryNilMemoryIsANoop(t *testing.T) {
	got, promoted := transport.ReorderByMemory(candidatesFor("cloak", "reality"), "auto", "wifi", nil)
	if !equal(kinds(got), []string{"cloak", "reality"}) {
		t.Fatalf("got %v, want unchanged", kinds(got))
	}
	if promoted != "" {
		t.Fatalf("promoted %q, want none", promoted)
	}
}

func TestIsAuto(t *testing.T) {
	for _, preferred := range []string{"", "auto"} {
		if !transport.IsAuto(preferred) {
			t.Fatalf("IsAuto(%q) = false, want true", preferred)
		}
	}
	for _, preferred := range []string{"cloak", "reality", "AUTO"} {
		if transport.IsAuto(preferred) {
			t.Fatalf("IsAuto(%q) = true, want false", preferred)
		}
	}
}
