package wg

import (
	"net/netip"
	"strings"
	"testing"
)

func TestTransformWGConfigInclude(t *testing.T) {
	edge := []netip.Prefix{netip.MustParsePrefix("104.21.24.200/32")}
	config := "[Interface]\nAddress = 10.10.1.3/32\n\n[Peer]\nPublicKey = k\nAllowedIPs = 0.0.0.0/2, 105.0.0.0/8 # carved\nEndpoint = 127.0.0.1:51820\n"

	got, err := TransformWGConfigInclude(config, edge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "AllowedIPs = 0.0.0.0/2, 105.0.0.0/8, 104.21.24.200/32") {
		t.Fatalf("carved edge not put back:\n%s", got)
	}

	// Already covered: the line is left alone, so the route count does not grow.
	covered := "[Peer]\nAllowedIPs = 0.0.0.0/0\n"
	if got, _ := TransformWGConfigInclude(covered, edge); got != covered {
		t.Fatalf("covered config changed:\n%s", got)
	}

	// A v6-only peer is not a full tunnel; nothing is added to it.
	v6 := "[Peer]\nAllowedIPs = ::/0\n"
	if got, _ := TransformWGConfigInclude(v6, edge); strings.Contains(got, "104.21.24.200") {
		t.Fatalf("added an IPv4 edge to a v6-only peer:\n%s", got)
	}

	// Only the [Peer] AllowedIPs: an [Interface] key of the same name is untouched.
	if _, err := TransformWGConfigInclude("[Peer]\nAllowedIPs = nonsense\n", edge); err == nil {
		t.Fatal("want an error for an unparseable AllowedIPs entry")
	}
}
