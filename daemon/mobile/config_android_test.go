//go:build android

package mobile

import (
	"reflect"
	"testing"
)

func TestAndroidKeepsLoopbackDNSForALocalProxy(t *testing.T) {
	got := decodeConfig(`{"customDns":["127.0.0.1","9.9.9.9"]}`).CustomDNS
	if want := []string{"127.0.0.1", "9.9.9.9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("custom DNS = %v, want %v", got, want)
	}
}
