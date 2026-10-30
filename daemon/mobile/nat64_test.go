package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"sync"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// wellKnownNAT64 synthesizes under 64:ff9b::/96, as iOS does on a DNS64 network,
// and records every literal it was asked about.
type wellKnownNAT64 struct {
	mu    sync.Mutex
	asked []string
}

func (n *wellKnownNAT64) synthesize(literal string) string {
	n.mu.Lock()
	n.asked = append(n.asked, literal)
	n.mu.Unlock()
	v4, err := netip.ParseAddr(literal)
	if err != nil || !v4.Is4() {
		return literal
	}
	raw := [16]byte{0x00, 0x64, 0xff, 0x9b}
	b := v4.As4()
	copy(raw[12:], b[:])
	return netip.AddrFrom16(raw).String()
}

func (n *wellKnownNAT64) questions() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.asked...)
}

func noNAT64(literal string) string { return literal }

// everyTransportProfile names a distinct IPv4 address per transport, the way
// a hub's per-transport remoteIp does.
func everyTransportProfile() *state.Profile {
	return &state.Profile{
		ID:    "auto-lon-1",
		Cloak: state.CloakProfile{RemoteHost: "203.0.113.10", RemotePort: 443, ServerName: "www.example.com"},
		Naive: &state.NaiveProfile{RemoteHost: "203.0.113.15", RemotePort: 443, ServerName: "naive.example.net"},
		Reality: &state.RealityProfile{RemoteHost: "203.0.113.11", RemotePort: 8443,
			ServerName: "www.cloudflare.com"},
		Hysteria2: &state.Hysteria2Profile{RemoteHost: "203.0.113.12", RemotePort: 8444,
			ServerName: "lon1.example.net", RemotePorts: []string{"20000:20010"}},
		Shadowsocks: &state.ShadowsocksProfile{RemoteHost: "203.0.113.13", RemotePort: 8388,
			TargetHost: "127.0.0.1", TargetPort: 51820},
		AnyTLS: &state.AnyTLSProfile{RemoteHost: "203.0.113.16", RemotePort: 8445,
			ServerName: "anytls.example.net", TargetHost: "127.0.0.1"},
		Snowflake: &state.SnowflakeProfile{BrokerURL: "https://broker.example"},
		Hop:       &state.HopProfile{SingBoxPort: 51900},
	}
}

// cloneProfile deep-copies through JSON, so a later comparison sees any write
// that reached the original through a shared pointer.
func cloneProfile(t *testing.T, p *state.Profile) *state.Profile {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("clone profile: %v", err)
	}
	var out state.Profile
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("clone profile: %v", err)
	}
	return &out
}

func TestSynthesizedProfileMapsEveryIPv4Endpoint(t *testing.T) {
	nat64 := &wellKnownNAT64{}
	got, changed := synthesizedProfile(everyTransportProfile(), nat64.synthesize)

	if !changed {
		t.Fatal("changed = false, want true when every endpoint was synthesized")
	}
	hosts := map[string][2]string{
		"cloak":       {got.Cloak.RemoteHost, "64:ff9b::cb00:710a"},
		"naive":       {got.Naive.RemoteHost, "64:ff9b::cb00:710f"},
		"reality":     {got.Reality.RemoteHost, "64:ff9b::cb00:710b"},
		"hysteria2":   {got.Hysteria2.RemoteHost, "64:ff9b::cb00:710c"},
		"shadowsocks": {got.Shadowsocks.RemoteHost, "64:ff9b::cb00:710d"},
		"anytls":      {got.AnyTLS.RemoteHost, "64:ff9b::cb00:7110"},
	}
	for kind, pair := range hosts {
		if pair[0] != pair[1] {
			t.Errorf("%s remoteHost = %q, want %q", kind, pair[0], pair[1])
		}
	}
	if got.Shadowsocks.TargetHost != "127.0.0.1" || got.AnyTLS.TargetHost != "127.0.0.1" {
		t.Errorf("target hosts = %q, %q; the node's own loopback must stay as it is",
			got.Shadowsocks.TargetHost, got.AnyTLS.TargetHost)
	}
}

func TestSynthesizedProfileAsksOncePerAddress(t *testing.T) {
	p := everyTransportProfile()
	p.Reality.RemoteHost = p.Cloak.RemoteHost
	p.Hysteria2.RemoteHost = p.Cloak.RemoteHost
	nat64 := &wellKnownNAT64{}

	synthesizedProfile(p, nat64.synthesize)

	seen := map[string]int{}
	for _, literal := range nat64.questions() {
		seen[literal]++
	}
	if seen["203.0.113.10"] != 1 {
		t.Fatalf("asked about the shared address %d times, want once: %v", seen["203.0.113.10"], nat64.questions())
	}
}

func TestSynthesizedProfileLeavesIPv6AndNamesAlone(t *testing.T) {
	p := &state.Profile{
		Cloak:       state.CloakProfile{RemoteHost: "2001:db8::10"},
		Reality:     &state.RealityProfile{RemoteHost: "[2001:db8::11]"},
		Hysteria2:   &state.Hysteria2Profile{RemoteHost: "lon1.example.net"},
		Shadowsocks: &state.ShadowsocksProfile{RemoteHost: "64:ff9b::cb00:710d"},
		AnyTLS:      &state.AnyTLSProfile{RemoteHost: ""},
		Naive:       &state.NaiveProfile{RemoteHost: "naive.example.net"},
	}
	want := cloneProfile(t, p)
	nat64 := &wellKnownNAT64{}

	got, changed := synthesizedProfile(p, nat64.synthesize)

	if changed {
		t.Fatal("changed = true, want false when no endpoint is an IPv4 address")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want it unchanged %+v", got, want)
	}
	if asked := nat64.questions(); len(asked) != 0 {
		t.Fatalf("asked the synthesizer about %v, want nothing", asked)
	}
}

func TestSynthesizedProfileLeavesLocalAddressesAlone(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.7", "192.168.1.20", "172.16.0.1", "169.254.1.1", "0.0.0.0"} {
		p := &state.Profile{Cloak: state.CloakProfile{RemoteHost: host}}
		nat64 := &wellKnownNAT64{}
		got, changed := synthesizedProfile(p, nat64.synthesize)
		if changed || got.Cloak.RemoteHost != host || len(nat64.questions()) != 0 {
			t.Errorf("%s: got %q changed=%v asked=%v, want it left alone", host, got.Cloak.RemoteHost, changed, nat64.questions())
		}
	}
}

func TestSynthesizedProfileKeepsTheNameTLSPresents(t *testing.T) {
	p := everyTransportProfile()
	p.Cloak.ServerName = ""
	p.Reality.ServerName = ""
	p.Hysteria2.ServerName = ""
	p.AnyTLS.ServerName = " "
	p.Naive.ServerName = ""

	got, _ := synthesizedProfile(p, (&wellKnownNAT64{}).synthesize)

	// These three fall back to the dialed host, so the original address must stay the name.
	if got.Hysteria2.ServerName != "203.0.113.12" {
		t.Errorf("hysteria2 serverName = %q, want the original 203.0.113.12", got.Hysteria2.ServerName)
	}
	if got.AnyTLS.ServerName != "203.0.113.16" {
		t.Errorf("anytls serverName = %q, want the original 203.0.113.16", got.AnyTLS.ServerName)
	}
	if got.Naive.ServerName != "203.0.113.15" {
		t.Errorf("naive serverName = %q, want the original 203.0.113.15", got.Naive.ServerName)
	}
	// Cloak and REALITY fall back to a cover name, which an address would replace.
	if got.Cloak.ServerName != "" || got.Reality.ServerName != "" {
		t.Errorf("cloak/reality serverName = %q/%q, want both left empty", got.Cloak.ServerName, got.Reality.ServerName)
	}
}

func TestSynthesizedProfileKeepsExplicitNames(t *testing.T) {
	got, _ := synthesizedProfile(everyTransportProfile(), (&wellKnownNAT64{}).synthesize)

	names := map[string][2]string{
		"cloak":     {got.Cloak.ServerName, "www.example.com"},
		"naive":     {got.Naive.ServerName, "naive.example.net"},
		"reality":   {got.Reality.ServerName, "www.cloudflare.com"},
		"hysteria2": {got.Hysteria2.ServerName, "lon1.example.net"},
		"anytls":    {got.AnyTLS.ServerName, "anytls.example.net"},
	}
	for kind, pair := range names {
		if pair[0] != pair[1] {
			t.Errorf("%s serverName = %q, want %q", kind, pair[0], pair[1])
		}
	}
}

func TestSynthesizedProfileNeverWritesTheOriginal(t *testing.T) {
	p := everyTransportProfile()
	p.Hysteria2.ServerName = ""
	p.AnyTLS.ServerName = ""
	want := cloneProfile(t, p)

	got, _ := synthesizedProfile(p, (&wellKnownNAT64{}).synthesize)

	if !reflect.DeepEqual(p, want) {
		t.Fatalf("original became %+v, want it untouched %+v", p, want)
	}
	if got == p || got.Naive == p.Naive || got.Reality == p.Reality || got.Hysteria2 == p.Hysteria2 ||
		got.Shadowsocks == p.Shadowsocks || got.AnyTLS == p.AnyTLS {
		t.Fatal("the result shares a transport block with the original")
	}
	got.Hysteria2.RemotePorts[0] = "1:2"
	if p.Hysteria2.RemotePorts[0] != "20000:20010" {
		t.Fatal("the result shares hysteria2's port ranges with the original")
	}
}

func TestSynthesizedProfileCarriesEveryOtherField(t *testing.T) {
	p := everyTransportProfile()
	got, _ := synthesizedProfile(p, noNAT64)

	if !reflect.DeepEqual(got, cloneProfile(t, p)) {
		t.Fatalf("got %+v, want a field-for-field copy of %+v", got, p)
	}
}

func TestSynthesizedProfileHandlesMissingBlocks(t *testing.T) {
	if got, changed := synthesizedProfile(nil, noNAT64); got != nil || changed {
		t.Fatalf("nil profile = %v, %v; want nil, false", got, changed)
	}
	p := &state.Profile{Cloak: state.CloakProfile{RemoteHost: "203.0.113.10"}}

	got, changed := synthesizedProfile(p, (&wellKnownNAT64{}).synthesize)

	if !changed || got.Cloak.RemoteHost != "64:ff9b::cb00:710a" {
		t.Fatalf("cloak-only = %q changed=%v, want it synthesized", got.Cloak.RemoteHost, changed)
	}
	if got.Naive != nil || got.Reality != nil || got.Hysteria2 != nil || got.Shadowsocks != nil ||
		got.AnyTLS != nil || got.Snowflake != nil || got.Hop != nil {
		t.Fatalf("got %+v, want every absent block still nil", got)
	}
}

func TestSynthesizeHostDistrustsTheSynthesizer(t *testing.T) {
	for _, answer := range []string{"", "not an address", "203.0.113.99", "::ffff:203.0.113.10", "::1", "::", "fe80::1%en0"} {
		got := synthesizeHost(func(string) string { return answer }, "203.0.113.10")
		if got != "203.0.113.10" {
			t.Errorf("synthesizer answered %q: got %q, want the literal kept", answer, got)
		}
	}
}

func TestSynthesizeHostCanonicalisesTheAnswer(t *testing.T) {
	got := synthesizeHost(func(string) string { return "64:FF9B::203.0.113.10" }, "203.0.113.10")
	if got != "64:ff9b::cb00:710a" {
		t.Fatalf("got %q, want the canonical 64:ff9b::cb00:710a", got)
	}
}

func TestSynthesizeAddressBracketsTheIPv6Host(t *testing.T) {
	nat64 := (&wellKnownNAT64{}).synthesize
	tests := map[string]string{
		"203.0.113.10:443":    "[64:ff9b::cb00:710a]:443",
		"127.0.0.1:8080":      "127.0.0.1:8080",
		"hub.example.com:443": "hub.example.com:443",
		"[2001:db8::1]:443":   "[2001:db8::1]:443",
		"no-port":             "no-port",
	}
	for in, want := range tests {
		if got := synthesizeAddress(nat64, in); got != want {
			t.Errorf("synthesizeAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

// dialRecorder stands in for the protected dialer, keeping the address asked for.
type dialRecorder struct{ addresses []string }

func (r *dialRecorder) dial(_ context.Context, _, address string) (net.Conn, error) {
	r.addresses = append(r.addresses, address)
	return nil, errors.New("recorded")
}

func TestNAT64DialDialsTheSynthesizedAddress(t *testing.T) {
	recorder := &dialRecorder{}
	nat64 := &wellKnownNAT64{}
	dial := nat64Dial(recorder.dial, nat64.synthesize)

	for _, address := range []string{"203.0.113.10:443", "127.0.0.1:1080", "hub.example.com:443"} {
		_, _ = dial(context.Background(), "tcp", address)
	}

	want := []string{"[64:ff9b::cb00:710a]:443", "127.0.0.1:1080", "hub.example.com:443"}
	if !reflect.DeepEqual(recorder.addresses, want) {
		t.Fatalf("dialed %v, want %v", recorder.addresses, want)
	}
	if asked := nat64.questions(); !reflect.DeepEqual(asked, []string{"203.0.113.10"}) {
		t.Fatalf("asked the synthesizer about %v, want only the public address", asked)
	}
}

func TestHubProxyProfilesDialTheSynthesizedNode(t *testing.T) {
	nat64 := (&wellKnownNAT64{}).synthesize
	creds := realityCreds("203.0.113.20")
	wantReality := state.RealityProfile{
		RemoteHost: "64:ff9b::cb00:7114", RemotePort: 443, UUID: creds.UUID,
		PublicKey: creds.PublicKey, ShortID: creds.ShortID, ServerName: "swdist.apple.com",
	}
	if got := creds.dialProfile(nat64); got != wantReality {
		t.Errorf("reality = %+v, want %+v", got, wantReality)
	}
	if got := creds.dialProfile(noNAT64).RemoteHost; got != "203.0.113.20" {
		t.Errorf("reality without NAT64 dials %q, want the node itself", got)
	}

	ss := hubShadowsocksCreds{RemoteHost: "203.0.113.21", RemotePort: 8489, Method: "2022-blake3-aes-128-gcm", Password: "pw"}
	wantSS := state.ShadowsocksProfile{
		RemoteHost: "64:ff9b::cb00:7115", RemotePort: 8489, Method: "2022-blake3-aes-128-gcm", Password: "pw",
	}
	if got := ss.dialProfile(nat64); got != wantSS {
		t.Errorf("shadowsocks = %+v, want %+v", got, wantSS)
	}
	if got := ss.dialProfile(noNAT64).RemoteHost; got != "203.0.113.21" {
		t.Errorf("shadowsocks without NAT64 dials %q, want the node itself", got)
	}
}

func TestURLHostBracketsIPv6(t *testing.T) {
	tests := map[string]string{
		"203.0.113.10":       "203.0.113.10",
		"hub.example.com":    "hub.example.com",
		"2001:db8::1":        "[2001:db8::1]",
		"[2001:db8::1]":      "[2001:db8::1]",
		"64:ff9b::cb00:710a": "[64:ff9b::cb00:710a]",
	}
	for in, want := range tests {
		if got := urlHost(in); got != want {
			t.Errorf("urlHost(%q) = %q, want %q", in, got, want)
		}
	}
}
