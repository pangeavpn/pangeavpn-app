package api

import (
	"context"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// directWireGuardProfile is a profile that can be connected either way: over
// cloak through the loopback bridge its config text points at, or straight to
// the node named by DirectEndpoint.
func directWireGuardProfile() state.Profile {
	return state.Profile{
		ID:    "p1",
		Name:  "p1",
		Cloak: state.CloakProfile{RemoteHost: "203.0.113.10", RemotePort: 443, LocalPort: 51821},
		WireGuard: state.WireGuardProfile{
			TunnelName:     "pangea0",
			ConfigText:     "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint = 127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
			DirectEndpoint: "203.0.113.10:51820",
		},
	}
}

func TestConnect_PreferredTransportWireGuard_RunsNoTransportAndDialsTheNode(t *testing.T) {
	profile := directWireGuardProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "wireguard"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if cloak.startCalled || naive.startCalled {
		t.Fatal("no transport should be started when wireguard is selected")
	}
	wgMgr.mu.Lock()
	config := wgMgr.lastStartConfig
	wgMgr.mu.Unlock()
	if !strings.Contains(config, "Endpoint = 203.0.113.10:51820") {
		t.Fatalf("wireguard was not repointed at the node; config:\n%s", config)
	}
	if strings.Contains(config, "127.0.0.1") {
		t.Fatalf("loopback bridge endpoint survived the rewrite; config:\n%s", config)
	}
	if got := svc.Status(context.Background()).ActiveTransport; got != "wireguard" {
		t.Fatalf("ActiveTransport = %q, want wireguard", got)
	}
}

func TestConnect_PreferredTransportWireGuard_ErrorsWithoutADirectEndpoint(t *testing.T) {
	profile := directWireGuardProfile()
	profile.WireGuard.DirectEndpoint = ""
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, &fakeKillSwitch{}, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "wireguard"})
	if err == nil {
		t.Fatal("expected Connect to fail — the profile names no wireguard endpoint")
	}
	if cloak.startCalled {
		t.Fatal("cloak must not be started as a fallback for a failed wireguard request")
	}
	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	if wgMgr.startCount != 0 {
		t.Fatalf("wireguard started %d times, want 0", wgMgr.startCount)
	}
}

func TestConnect_PreferredTransportWireGuard_RejectsAMalformedEndpoint(t *testing.T) {
	profile := directWireGuardProfile()
	profile.WireGuard.DirectEndpoint = "203.0.113.10"
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "wireguard"})
	if err == nil {
		t.Fatal("expected Connect to fail — the endpoint carries no port")
	}
}

// Auto mode must never reach for the direct method, however well it would work:
// a bare WireGuard handshake is the easiest thing in the cascade to fingerprint.
func TestConnect_AutoNeverFallsBackToDirectWireGuard(t *testing.T) {
	profile := directWireGuardProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	mem := &fakeTransportMemory{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, &fakeKillSwitch{}, profile)
	svc.transportMemory = mem
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := svc.Status(context.Background()).ActiveTransport; got != "cloak" {
		t.Fatalf("ActiveTransport = %q, want cloak", got)
	}
	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	if !strings.Contains(wgMgr.lastStartConfig, "127.0.0.1") {
		t.Fatalf("auto mode left the loopback bridge behind; config:\n%s", wgMgr.lastStartConfig)
	}
}

// A direct session is remembered for nothing: it proves no transport works on
// this network, and auto mode would only be slowed down by looking for it.
func TestConnect_DirectWireGuardIsNotRememberedForTheNetwork(t *testing.T) {
	profile := directWireGuardProfile()
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)
	mem := &fakeTransportMemory{}
	svc.transportMemory = mem
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "wireguard"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got, ok := mem.Lookup("wifi-home"); ok {
		t.Fatalf("remembered %q for this network, want nothing", got)
	}
}

// The health check must not read "no transport process" as a broken session:
// there is no process to find, and a direct tunnel is judged by WireGuard alone.
func TestHealthCheck_DirectWireGuardSessionStaysConnected(t *testing.T) {
	profile := directWireGuardProfile()
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, ks, profile)
	svc.probeResolver = nil

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "wireguard"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	svc.runHealthCheck(context.Background())

	if got, detail := svc.machine.Get(); got != state.StateConnected {
		t.Fatalf("state = %q (%s), want CONNECTED", got, detail)
	}
}

func TestRewriteWireGuardEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		endpoint string
		want     string
		replaced bool
	}{
		{
			name:     "loopback bridge is repointed at the node",
			config:   "[Peer]\nEndpoint = 127.0.0.1:51821\nAllowedIPs = 0.0.0.0/0\n",
			endpoint: "203.0.113.10:51820",
			want:     "[Peer]\nEndpoint = 203.0.113.10:51820\nAllowedIPs = 0.0.0.0/0\n",
			replaced: true,
		},
		{
			name:     "spacing-free form is handled too",
			config:   "[Peer]\nEndpoint=127.0.0.1:51821\n",
			endpoint: "203.0.113.10:51820",
			want:     "[Peer]\nEndpoint=203.0.113.10:51820\n",
			replaced: true,
		},
		{
			name:     "no endpoint line to rewrite",
			config:   "[Peer]\nAllowedIPs = 0.0.0.0/0\n",
			endpoint: "203.0.113.10:51820",
			want:     "[Peer]\nAllowedIPs = 0.0.0.0/0\n",
			replaced: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, replaced := rewriteWireGuardEndpoint(tt.config, tt.endpoint)
			if replaced != tt.replaced {
				t.Fatalf("replaced = %v, want %v", replaced, tt.replaced)
			}
			if got != tt.want {
				t.Fatalf("config =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// The adopt path is what a connect finds when a tunnel from an earlier daemon
// run is still up: a direct session must not have Cloak started underneath it.
func TestConnect_AdoptsRunningTunnelWithoutCloakWhenWireGuardIsSelected(t *testing.T) {
	profile := directWireGuardProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{running: true}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, &fakeKillSwitch{}, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "wireguard"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if cloak.startCalled {
		t.Fatal("cloak was started under an adopted direct session")
	}
	if got := svc.Status(context.Background()).ActiveTransport; got != "wireguard" {
		t.Fatalf("ActiveTransport = %q, want wireguard", got)
	}
}
