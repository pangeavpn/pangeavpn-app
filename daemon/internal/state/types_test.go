package state_test

import (
	"encoding/json"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestProfileNaiveIsOptionalAndOmittedWhenAbsent(t *testing.T) {
	p := state.Profile{
		ID:   "p1",
		Name: "Test",
		Cloak: state.CloakProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
		},
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if p.Naive != nil {
		t.Fatalf("Naive should default to nil, got %+v", p.Naive)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, present := decoded["naive"]; present {
		t.Fatalf("naive key should be omitted from JSON when nil, got: %s", b)
	}
}

func TestProfileNaivePresentWhenSet(t *testing.T) {
	p := state.Profile{
		ID:   "p1",
		Name: "Test",
		Naive: &state.NaiveProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
			Username:   "u",
			Password:   "p",
			ServerName: "www.bing.com",
			LocalPort:  0,
		},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, present := decoded["naive"]; !present {
		t.Fatalf("naive key should be present when set, got: %s", b)
	}
}

func TestStatusResponseCarriesActiveTransport(t *testing.T) {
	s := state.StatusResponse{
		State:           state.StateConnected,
		ActiveTransport: "naive",
		Naive:           state.TransportStatus{Running: true},
	}
	if s.ActiveTransport != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive", s.ActiveTransport)
	}
	if !s.Naive.Running {
		t.Fatalf("Naive.Running = false, want true")
	}
}
