//go:build ios

package mobile

import (
	"reflect"
	"testing"
)

func TestIOSDropsLoopbackDNS(t *testing.T) {
	got := decodeConfig(`{"customDns":["127.0.0.1","127.0.0.53","9.9.9.9"]}`).CustomDNS
	if want := []string{"9.9.9.9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("custom DNS = %v, want %v", got, want)
	}
	initForTest(t, nil)
	stored, err := SetConfig(`{"customDns":["127.0.0.1"]}`)
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if cfg := decodeConfig(stored); len(cfg.CustomDNS) != 0 {
		t.Fatalf("SetConfig kept loopback DNS %v", cfg.CustomDNS)
	}
}
