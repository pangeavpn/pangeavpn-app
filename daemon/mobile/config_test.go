package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeMTU(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{"in range", 1400, 1400},
		{"lower bound", mtuMin, mtuMin},
		{"upper bound", mtuMax, mtuMax},
		{"below IPv6 minimum", 1279, mtuDefault},
		{"above wg-quick ceiling", 1421, mtuDefault},
		{"unset", 0, mtuDefault},
		{"negative", -1, mtuDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMTU(tt.value); got != tt.want {
				t.Fatalf("normalizeMTU(%d) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestNormalizeCustomDNS(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"plain pair", []string{"1.1.1.1", "8.8.8.8"}, []string{"1.1.1.1", "8.8.8.8"}},
		{"comma separated in one entry", []string{"1.1.1.1, 8.8.8.8"}, []string{"1.1.1.1", "8.8.8.8"}},
		{"whitespace separated", []string{"1.1.1.1\t8.8.4.4"}, []string{"1.1.1.1", "8.8.4.4"}},
		{"deduplicates", []string{"1.1.1.1", "1.1.1.1"}, []string{"1.1.1.1"}},
		{"drops octets over 255", []string{"256.1.1.1", "1.1.1.1"}, []string{"1.1.1.1"}},
		{"drops short form", []string{"1.1.1"}, []string{}},
		{"drops IPv6", []string{"2001:4860:4860::8888"}, []string{}},
		{"drops hostnames", []string{"dns.example.com"}, []string{}},
		{"drops resolvers behind the tunnel", []string{"10.0.0.1", "192.168.1.1", "172.20.0.1", "100.64.0.1",
			"169.254.1.1", "0.1.2.3", "224.0.0.1", "255.255.255.255", "9.9.9.9"}, []string{"9.9.9.9"}},
		{"keeps loopback for a local proxy", []string{"127.0.0.1"}, []string{"127.0.0.1"}},
		{"keeps the edges of private ranges", []string{"172.15.255.255", "172.32.0.1"}, []string{"172.15.255.255", "172.32.0.1"}},
		{"empty", []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCustomDNS(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestNormalizeCustomDNSRejectsOversizedOctet(t *testing.T) {
	if got := normalizeCustomDNS([]string{"1.1.1.1000"}); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

func TestResolveDNSPrefersCustom(t *testing.T) {
	got := resolveDNS("10.0.0.1, 10.0.0.2", []string{"1.1.1.1"})
	if len(got) != 1 || got[0] != "1.1.1.1" {
		t.Fatalf("got %v, want the custom server", got)
	}
}

func TestResolveDNSFallsBackToServerAssigned(t *testing.T) {
	got := resolveDNS("10.0.0.1, 10.0.0.2", nil)
	if len(got) != 2 || got[0] != "10.0.0.1" || got[1] != "10.0.0.2" {
		t.Fatalf("got %v, want the hub-assigned pair", got)
	}
}

func TestDecodeConfigDefaultsWhenEmpty(t *testing.T) {
	got := decodeConfig("")
	if got.PreferredTransport != "auto" || got.MTU != mtuDefault {
		t.Fatalf("got %+v, want defaults", got)
	}
	if !got.HubMethods.DirectIP || !got.HubMethods.Shadowsocks || !got.HubMethods.Fronted {
		t.Fatalf("hub methods should default on, got %+v", got.HubMethods)
	}
	if got.HubMethods.Normal {
		t.Fatal("the cleartext-SNI method must default off")
	}
}

// Settings are a convenience; a corrupt blob must never stop a connect.
func TestDecodeConfigFallsBackOnGarbage(t *testing.T) {
	got := decodeConfig("{not json")
	if got.PreferredTransport != "auto" || got.MTU != mtuDefault {
		t.Fatalf("got %+v, want defaults", got)
	}
}

func TestDecodeConfigSanitizesStoredValues(t *testing.T) {
	got := decodeConfig(`{"preferredTransport":"telepathy","mtu":9000,"customDns":["1.1.1.1","nope"]}`)
	if got.PreferredTransport != "auto" {
		t.Fatalf("unknown transport %q should fall back to auto", got.PreferredTransport)
	}
	if got.MTU != mtuDefault {
		t.Fatalf("out-of-range mtu %d should fall back", got.MTU)
	}
	if len(got.CustomDNS) != 1 || got.CustomDNS[0] != "1.1.1.1" {
		t.Fatalf("got %v, want only the valid server", got.CustomDNS)
	}
}

// NaiveProxy needs cgo and Snowflake cannot be protect()ed, so a pin on either,
// even one an older build persisted, must fall back to the cascade.
func TestDecodeConfigRejectsTransportsAndroidCannotRun(t *testing.T) {
	for _, kind := range []string{"naive", "snowflake"} {
		got := decodeConfig(`{"preferredTransport":"` + kind + `"}`)
		if got.PreferredTransport != "auto" {
			t.Fatalf("%s should not be selectable, got %q", kind, got.PreferredTransport)
		}
	}
}

func TestDecodeConfigKeepsEveryValidTransport(t *testing.T) {
	for _, kind := range []string{"auto", "cloak", "reality", "shadowsocks", "hysteria2"} {
		got := decodeConfig(`{"preferredTransport":"` + kind + `"}`)
		if got.PreferredTransport != kind {
			t.Fatalf("%s was rewritten to %q", kind, got.PreferredTransport)
		}
	}
}

func TestEncodeConfigRoundTrips(t *testing.T) {
	original := config{
		PreferredTransport: "reality",
		CustomDNS:          []string{"9.9.9.9"},
		MTU:                1400,
		AllowLAN:           true,
		AutoConnect:        true,
		LastServerID:       "lon-1",
		HubMethods:         defaultHubMethods(),
	}
	encoded, err := encodeConfig(original)
	if err != nil {
		t.Fatalf("encodeConfig: %v", err)
	}
	if !strings.Contains(encoded, `"preferredTransport":"reality"`) {
		t.Fatalf("encoded blob missing transport: %s", encoded)
	}

	decoded := decodeConfig(encoded)
	if decoded.PreferredTransport != "reality" || decoded.MTU != 1400 {
		t.Fatalf("round trip lost values: %+v", decoded)
	}
	if !decoded.AllowLAN || !decoded.AutoConnect || decoded.LastServerID != "lon-1" {
		t.Fatalf("round trip lost values: %+v", decoded)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		t.Fatalf("encoded blob is not valid JSON: %v", err)
	}
}

// Off by default would mean an install upgraded into this build never uses it.
func TestDecodeConfigDefaultsDeadDropOn(t *testing.T) {
	if !decodeConfig("").DeadDrop {
		t.Fatal("dead drop should default on")
	}
	if !decodeConfig(`{"mtu":1380}`).DeadDrop {
		t.Fatal("a blob written before the setting existed should get the default")
	}
	if decodeConfig(`{"deadDrop":false}`).DeadDrop {
		t.Fatal("an explicit off must survive")
	}
}

// Kotlin's encoder omits fields equal to their defaults, so a blob it sends
// must read back with those defaults rather than zero values.
func TestDecodeConfigFillsWhatKotlinOmits(t *testing.T) {
	got := decodeConfig(`{"mtu":1400,"hubMethods":{"normal":true}}`)
	if !got.DeadDrop || !got.HubMethods.Reality || !got.HubMethods.DirectIP || !got.HubMethods.Normal {
		t.Fatalf("got %+v, want omitted fields at their defaults", got)
	}
	if off := decodeConfig(`{"deadDrop":false,"hubMethods":{"reality":false}}`); off.DeadDrop || off.HubMethods.Reality {
		t.Fatalf("got %+v, want the explicit offs kept", off)
	}
}
