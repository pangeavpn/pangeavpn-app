//go:build windows

package platform

import (
	"strings"
	"testing"
)

func TestParseOutboundPolicy_Standard(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard format",
			input:    "Firewall Policy                          BlockInbound,AllowOutbound\r\n",
			expected: "AllowOutbound",
		},
		{
			name:     "block outbound",
			input:    "Firewall Policy                          BlockInbound,BlockOutbound\r\n",
			expected: "BlockOutbound",
		},
		{
			name:     "with other lines",
			input:    "Ok.\r\n\r\nDomain Profile Settings:\r\n---\r\nFirewall Policy                          BlockInbound,AllowOutbound\r\n",
			expected: "AllowOutbound",
		},
		{
			name:     "empty output",
			input:    "",
			expected: "",
		},
		{
			name:     "allow inbound",
			input:    "Firewall Policy                          AllowInbound,BlockOutbound\r\n",
			expected: "BlockOutbound",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOutboundPolicy(tc.input)
			if got != tc.expected {
				t.Errorf("parseOutboundPolicy(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestParseInboundPolicy_Standard(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard format",
			input:    "Firewall Policy                          BlockInbound,AllowOutbound\r\n",
			expected: "BlockInbound",
		},
		{
			name:     "allow inbound",
			input:    "Firewall Policy                          AllowInbound,BlockOutbound\r\n",
			expected: "AllowInbound",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseInboundPolicy(tc.input)
			if got != tc.expected {
				t.Errorf("parseInboundPolicy(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestRuleNames(t *testing.T) {
	// Verify all rule names have the expected prefix.
	rules := []string{
		ksRuleLoopback,
		ksRuleLegacyLoopbackV6,
		ksRuleEndpoint,
		ksRuleLegacyTunnel,
		ksRuleTunnelOutbound,
		ksRuleTunnelInbound,
		ksRuleDHCP,
	}
	for _, name := range rules {
		if len(name) <= len(ksRulePrefix) {
			t.Errorf("rule name %q is too short", name)
		}
		if name[:len(ksRulePrefix)] != ksRulePrefix {
			t.Errorf("rule name %q doesn't have prefix %q", name, ksRulePrefix)
		}
	}
}

func TestBuildTunnelRuleScript_IPv4Only(t *testing.T) {
	script := buildTunnelRuleScript("WireGuard")
	if script == "" {
		t.Fatal("expected non-empty powershell script")
	}
	if got := "-Name 'PangeaVPN-KS-Tunnel-Out'"; !strings.Contains(script, got) {
		t.Fatalf("expected explicit outbound firewall rule name %q in script: %s", got, script)
	}
	if got := "-Name 'PangeaVPN-KS-Tunnel-In'"; !strings.Contains(script, got) {
		t.Fatalf("expected explicit inbound firewall rule name %q in script: %s", got, script)
	}
	if got := "-Profile Any"; !strings.Contains(script, got) {
		t.Fatalf("expected explicit profile scope %q in script: %s", got, script)
	}
	if got := "-Direction Outbound"; !strings.Contains(script, got) {
		t.Fatalf("expected outbound direction %q in script: %s", got, script)
	}
	if got := "-Direction Inbound"; !strings.Contains(script, got) {
		t.Fatalf("expected inbound direction %q in script: %s", got, script)
	}
	if got := "0.0.0.0-255.255.255.255"; !strings.Contains(script, got) {
		t.Fatalf("expected IPv4 RemoteAddress scope %q in script: %s", got, script)
	}
}
