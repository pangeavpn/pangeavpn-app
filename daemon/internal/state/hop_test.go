package state

import "testing"

func fullProfile() Profile {
	return Profile{
		ID:          "auto-eu-west-1",
		Cloak:       CloakProfile{RemoteHost: "192.0.2.10", RemotePort: 443},
		Naive:       &NaiveProfile{RemoteHost: "naive.example", RemotePort: 8443},
		Reality:     &RealityProfile{RemoteHost: "192.0.2.10", RemotePort: 8444},
		Hysteria2:   &Hysteria2Profile{RemoteHost: "192.0.2.10", RemotePort: 443},
		Shadowsocks: &ShadowsocksProfile{RemoteHost: "192.0.2.10", RemotePort: 8488},
	}
}

// A single-hop profile must resolve to exactly the destinations the daemon
// used before multihop existed. This is the regression guard for every
// existing user: if it fails, shipping multihop changed single-hop routing.
func TestApplyHopSingleHopKeepsTodaysTargets(t *testing.T) {
	got := ApplyHop(fullProfile())

	if got.Cloak.ProxyMethod != "wireguard" {
		t.Errorf("cloak proxyMethod = %q, want wireguard", got.Cloak.ProxyMethod)
	}
	if got.Reality.TargetPort != 51820 {
		t.Errorf("reality targetPort = %d, want 51820", got.Reality.TargetPort)
	}
	if got.Hysteria2.TargetPort != 51820 {
		t.Errorf("hysteria2 targetPort = %d, want 51820", got.Hysteria2.TargetPort)
	}
	if got.Shadowsocks.TargetPort != 51820 {
		t.Errorf("shadowsocks targetPort = %d, want 51820", got.Shadowsocks.TargetPort)
	}
	if got.Naive.BridgePort != 9000 {
		t.Errorf("naive bridgePort = %d, want 9000", got.Naive.BridgePort)
	}
	if got.IsMultihop() {
		t.Error("IsMultihop() = true for a profile with no hop")
	}
}

func TestApplyHopRoutesEveryTransportToTheEntryHopPort(t *testing.T) {
	profile := fullProfile()
	profile.Hop = &HopProfile{
		SingBoxPort:      51831,
		CloakProxyMethod: "wireguard-via-us-east-1",
		NaiveBridgePort:  9002,
		EntryRegion:      "eu-west-1",
		ExitRegion:       "us-east-1",
	}

	got := ApplyHop(profile)

	if got.Cloak.ProxyMethod != "wireguard-via-us-east-1" {
		t.Errorf("cloak proxyMethod = %q", got.Cloak.ProxyMethod)
	}
	for name, port := range map[string]int{
		"reality":     got.Reality.TargetPort,
		"hysteria2":   got.Hysteria2.TargetPort,
		"shadowsocks": got.Shadowsocks.TargetPort,
	} {
		if port != 51831 {
			t.Errorf("%s targetPort = %d, want 51831", name, port)
		}
	}
	if got.Naive.BridgePort != 9002 {
		t.Errorf("naive bridgePort = %d, want 9002", got.Naive.BridgePort)
	}
	// Shadowsocks is the one transport carrying a target *host*; the hop port
	// is a loopback service on the entry, never the exit's public address.
	if got.Shadowsocks.TargetHost != "127.0.0.1" {
		t.Errorf("shadowsocks targetHost = %q, want 127.0.0.1", got.Shadowsocks.TargetHost)
	}
	if !got.IsMultihop() || got.EntryRegion() != "eu-west-1" || got.ExitRegion() != "us-east-1" {
		t.Errorf("hop labels not surfaced: %v/%v", got.EntryRegion(), got.ExitRegion())
	}
}

// ApplyHop must not write through the caller's pointers: the stored config is
// shared, and a mutated copy would leak one session's hop into the next.
func TestApplyHopDoesNotMutateInput(t *testing.T) {
	profile := fullProfile()
	profile.Hop = &HopProfile{
		SingBoxPort: 51831, CloakProxyMethod: "wireguard-via-x",
		NaiveBridgePort: 9002, EntryRegion: "a", ExitRegion: "b",
	}

	_ = ApplyHop(profile)

	if profile.Reality.TargetPort != 0 || profile.Hysteria2.TargetPort != 0 ||
		profile.Shadowsocks.TargetPort != 0 || profile.Naive.BridgePort != 0 ||
		profile.Cloak.ProxyMethod != "" {
		t.Error("ApplyHop mutated the profile it was given")
	}
}

// A hop missing any selector would leave that transport on its default and
// egress at the entry node — the guarantee silently dropped. Reject instead.
func TestValidateHopRejectsPartialHops(t *testing.T) {
	complete := HopProfile{
		SingBoxPort: 51831, CloakProxyMethod: "wireguard-via-x",
		NaiveBridgePort: 9002, EntryRegion: "a", ExitRegion: "b",
	}

	tests := map[string]func(*HopProfile){
		"no singBoxPort":      func(h *HopProfile) { h.SingBoxPort = 0 },
		"no cloakProxyMethod": func(h *HopProfile) { h.CloakProxyMethod = "" },
		"no naiveBridgePort":  func(h *HopProfile) { h.NaiveBridgePort = 0 },
		"no entryRegion":      func(h *HopProfile) { h.EntryRegion = "" },
		"no exitRegion":       func(h *HopProfile) { h.ExitRegion = "" },
		"same region":         func(h *HopProfile) { h.ExitRegion = h.EntryRegion },
	}

	for name, breakIt := range tests {
		t.Run(name, func(t *testing.T) {
			hop := complete
			breakIt(&hop)
			profile := fullProfile()
			profile.Hop = &hop
			if err := ValidateHop(profile); err == nil {
				t.Fatal("ValidateHop accepted an incomplete hop")
			}
		})
	}

	profile := fullProfile()
	profile.Hop = &complete
	if err := ValidateHop(profile); err != nil {
		t.Fatalf("ValidateHop rejected a complete hop: %v", err)
	}
	if err := ValidateHop(fullProfile()); err != nil {
		t.Fatalf("ValidateHop rejected a single-hop profile: %v", err)
	}
}

// naiveBridgePort is only required when the profile actually has naive.
func TestValidateHopIgnoresNaiveBridgePortWithoutNaive(t *testing.T) {
	profile := fullProfile()
	profile.Naive = nil
	profile.Hop = &HopProfile{
		SingBoxPort: 51831, CloakProxyMethod: "wireguard-via-x",
		EntryRegion: "a", ExitRegion: "b",
	}
	if err := ValidateHop(profile); err != nil {
		t.Fatalf("ValidateHop required naiveBridgePort with no naive profile: %v", err)
	}
}
