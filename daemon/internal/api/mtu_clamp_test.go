package api

import "testing"

const wgConfig = "[Interface]\nPrivateKey = x\nAddress = 10.10.1.3/32\nMTU = 1380\nDNS = 1.1.1.1\n\n[Peer]\nEndpoint = 127.0.0.1:51820\n"

func TestClampWireGuardMTU_LowersAnOversizedMTU(t *testing.T) {
	got := clampWireGuardMTU(wgConfig, shadowsocksMaxMTU)
	if !contains(got, "MTU = 1280") {
		t.Fatalf("MTU was not clamped:\n%s", got)
	}
	if contains(got, "MTU = 1380") {
		t.Fatal("the original MTU is still present")
	}
	for _, keep := range []string{"PrivateKey = x", "Address = 10.10.1.3/32", "Endpoint = 127.0.0.1:51820", "DNS = 1.1.1.1"} {
		if !contains(got, keep) {
			t.Errorf("clamping dropped %q", keep)
		}
	}
}

func TestClampWireGuardMTU_LeavesALowerMTUAlone(t *testing.T) {
	cfg := "[Interface]\nMTU = 1200\n"
	if got := clampWireGuardMTU(cfg, shadowsocksMaxMTU); got != cfg {
		t.Fatalf("a lower MTU was rewritten: %q", got)
	}
}

func TestClampWireGuardMTU_NoMTULineIsUnchanged(t *testing.T) {
	cfg := "[Interface]\nPrivateKey = x\n[Peer]\nEndpoint = 127.0.0.1:51820\n"
	if got := clampWireGuardMTU(cfg, shadowsocksMaxMTU); got != cfg {
		t.Fatalf("config without an MTU line was modified: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
