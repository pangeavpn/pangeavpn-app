//go:build darwin || linux || windows

package wg

import (
	"context"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestPreflight_IPv4ConfigPasses(t *testing.T) {
	manager := &wireGuardGoManager{}

	profile := state.WireGuardProfile{
		TunnelName: "wg-test",
		ConfigText: `[Interface]
PrivateKey = YWJjZGVmZw==
Address = 10.7.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = eHl6MTIzNDU=
Endpoint = 127.0.0.1:51820
AllowedIPs = 0.0.0.0/0
`,
	}

	if err := manager.Preflight(context.Background(), profile); err != nil {
		t.Fatalf("expected preflight success, got: %v", err)
	}
}

func TestPreflight_RejectsIPv6InterfaceAddress(t *testing.T) {
	manager := &wireGuardGoManager{}

	profile := state.WireGuardProfile{
		TunnelName: "wg-test",
		ConfigText: `[Interface]
PrivateKey = YWJjZGVmZw==
Address = fd00::2/128

[Peer]
PublicKey = eHl6MTIzNDU=
Endpoint = 127.0.0.1:51820
AllowedIPs = 0.0.0.0/0
`,
	}

	err := manager.Preflight(context.Background(), profile)
	if err == nil {
		t.Fatal("expected IPv6 interface address rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ipv6 [interface] address") {
		t.Fatalf("expected IPv6 address error, got: %v", err)
	}
}

func TestPreflight_RejectsIPv6AllowedIPs(t *testing.T) {
	manager := &wireGuardGoManager{}

	profile := state.WireGuardProfile{
		TunnelName: "wg-test",
		ConfigText: `[Interface]
PrivateKey = YWJjZGVmZw==
Address = 10.7.0.2/32

[Peer]
PublicKey = eHl6MTIzNDU=
Endpoint = 127.0.0.1:51820
AllowedIPs = ::/0
`,
	}

	err := manager.Preflight(context.Background(), profile)
	if err == nil {
		t.Fatal("expected IPv6 AllowedIPs rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ipv6 allowedips") {
		t.Fatalf("expected IPv6 AllowedIPs error, got: %v", err)
	}
}

func TestPreflight_RejectsIPv6ProfileDNS(t *testing.T) {
	manager := &wireGuardGoManager{}

	profile := state.WireGuardProfile{
		TunnelName: "wg-test",
		ConfigText: `[Interface]
PrivateKey = YWJjZGVmZw==
Address = 10.7.0.2/32

[Peer]
PublicKey = eHl6MTIzNDU=
Endpoint = 127.0.0.1:51820
AllowedIPs = 0.0.0.0/0
`,
		DNS: []string{"2606:4700:4700::1111"},
	}

	err := manager.Preflight(context.Background(), profile)
	if err == nil {
		t.Fatal("expected IPv6 DNS rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ipv6 dns server") {
		t.Fatalf("expected IPv6 DNS error, got: %v", err)
	}
}

func TestResolveEndpointRoutes_IPv4OnlyFromLiterals(t *testing.T) {
	routes := resolveEndpointRoutes(context.Background(), []string{"198.51.100.10", "2001:db8::1"})
	if len(routes) != 1 {
		t.Fatalf("expected exactly one IPv4 endpoint route, got %d: %#v", len(routes), routes)
	}
	if routes[0].family != "inet" || routes[0].destination != "198.51.100.10" {
		t.Fatalf("unexpected route: %#v", routes[0])
	}
}
