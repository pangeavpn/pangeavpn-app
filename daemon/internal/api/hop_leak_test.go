package api

import (
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// The exit node's address must never reach the kill-switch permit list or the
// WireGuard bypass list. The client dials only the entry; permitting the exit
// would punch a hole for a host nothing connects to, and exclude it from
// AllowedIPs so traffic aimed at it would leave in the clear.
const (
	entryIP = "192.0.2.10"
	exitIP  = "198.51.100.20"
)

func multihopProfile() state.Profile {
	profile := state.Profile{
		ID:                   "auto-us-east-1",
		Cloak:                state.CloakProfile{LocalPort: 51820, RemoteHost: entryIP, RemotePort: 443},
		Naive:                &state.NaiveProfile{RemoteHost: entryIP, RemotePort: 8443},
		Reality:              &state.RealityProfile{RemoteHost: entryIP, RemotePort: 8444},
		Hysteria2:            &state.Hysteria2Profile{RemoteHost: entryIP, RemotePort: 443},
		Shadowsocks:          &state.ShadowsocksProfile{RemoteHost: entryIP, RemotePort: 8488},
		TransportEndpointIPs: []string{entryIP},
		Hop: &state.HopProfile{
			SingBoxPort: 51831, CloakProxyMethod: "wireguard-via-us-east-1",
			NaiveBridgePort: 9002, EntryRegion: "eu-west-1", ExitRegion: "us-east-1",
		},
		WireGuard: state.WireGuardProfile{
			// The peer endpoint is always loopback: the transport carries it.
			ConfigText: "[Peer]\nEndpoint = 127.0.0.1:51820\n",
		},
	}
	return state.ApplyHop(profile)
}

func TestKillSwitchPermitsExcludeTheExitNode(t *testing.T) {
	permits := killSwitchPermits(multihopProfile())

	if len(permits) == 0 {
		t.Fatal("no permits produced; the entry must still be reachable")
	}
	for _, permit := range permits {
		if strings.Contains(permit, exitIP) {
			t.Errorf("kill switch permits the exit node %q: %v", exitIP, permits)
		}
	}
	if !containsHost(permits, entryIP) {
		t.Errorf("kill switch does not permit the entry node %q: %v", entryIP, permits)
	}
}

func TestBypassHostsExcludeTheExitNode(t *testing.T) {
	bypass := withTransportBypassHosts(multihopProfile()).BypassHosts

	for _, host := range bypass {
		if strings.Contains(host, exitIP) {
			t.Errorf("bypass list routes the exit node %q outside the tunnel: %v", exitIP, bypass)
		}
	}
	if !containsHost(bypass, entryIP) {
		t.Errorf("bypass list omits the entry node %q, so its dial would recurse: %v", entryIP, bypass)
	}
}

// The WireGuard peer endpoint stays on loopback under multihop: the exit is
// reached through the transport, never dialled directly.
func TestMultihopWireGuardEndpointStaysLoopback(t *testing.T) {
	config := multihopProfile().WireGuard.ConfigText
	if strings.Contains(config, exitIP) {
		t.Errorf("wireguard config names the exit node directly:\n%s", config)
	}
	if !strings.Contains(config, "Endpoint = 127.0.0.1") {
		t.Errorf("wireguard peer endpoint is not loopback:\n%s", config)
	}
}

func containsHost(hosts []string, want string) bool {
	for _, host := range hosts {
		if host == want {
			return true
		}
	}
	return false
}
