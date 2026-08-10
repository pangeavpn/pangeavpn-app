package mobile

import "testing"

func TestNormalizeFrontedEndpoint(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"plain host", "relay.example.com", "relay.example.com", true},
		{"lowercased", "Relay.Example.COM", "relay.example.com", true},
		{"trimmed", "  relay.example.com  ", "relay.example.com", true},
		{"hyphenated label", "edge-1.example.com", "edge-1.example.com", true},
		{"bare label", "relay", "", false},
		{"empty", "", "", false},
		{"scheme rejected", "https://relay.example.com", "", false},
		{"path rejected", "relay.example.com/v1/secure", "", false},
		{"port rejected", "relay.example.com:8443", "", false},
		{"leading hyphen", "-relay.example.com", "", false},
		{"underscore", "relay_1.example.com", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeFrontedEndpoint(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestNormalizeFrontedEndpointRejectsOverlongHost(t *testing.T) {
	long := ""
	for len(long) < 300 {
		long += "abcdefgh."
	}
	if _, ok := normalizeFrontedEndpoint(long + "com"); ok {
		t.Fatal("an over-length hostname should be rejected")
	}
}

func TestRestoreFrontedEndpointsDropsBadAndDuplicates(t *testing.T) {
	got := restoreFrontedEndpoints([]string{"a.example.com", "bad", "a.example.com", "b.example.com"})
	want := []string{"a.example.com", "b.example.com"}
	if !sameStrings(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A hub that stopped naming relays is likelier a rollback than an instruction
// to discard the only addresses that still work.
func TestMergeFrontedEndpointsKeepsCacheOnEmptyAdvertisement(t *testing.T) {
	current := []string{"a.example.com"}
	if got := mergeFrontedEndpoints(current, nil); got != nil {
		t.Fatalf("got %v, want the cache left alone", got)
	}
	if got := mergeFrontedEndpoints(current, []string{"nope", ""}); got != nil {
		t.Fatalf("got %v, want the cache left alone", got)
	}
}

func TestMergeFrontedEndpointsKeepsTheWorkingLeaderInFront(t *testing.T) {
	current := []string{"b.example.com", "a.example.com"}
	got := mergeFrontedEndpoints(current, []string{"a.example.com", "b.example.com", "c.example.com"})
	want := []string{"b.example.com", "a.example.com", "c.example.com"}
	if !sameStrings(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMergeFrontedEndpointsReportsNoChange(t *testing.T) {
	current := []string{"a.example.com", "b.example.com"}
	if got := mergeFrontedEndpoints(current, []string{"a.example.com", "b.example.com"}); got != nil {
		t.Fatalf("got %v, want nil so the caller skips a write", got)
	}
}

func TestPromoteFrontedEndpoint(t *testing.T) {
	list := []string{"a.example.com", "b.example.com", "c.example.com"}
	got := promoteFrontedEndpoint(list, 2)
	want := []string{"c.example.com", "a.example.com", "b.example.com"}
	if !sameStrings(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !sameStrings(list, []string{"a.example.com", "b.example.com", "c.example.com"}) {
		t.Fatalf("the input must not be mutated, got %v", list)
	}
	if promoteFrontedEndpoint(list, 0) != nil {
		t.Fatal("promoting the leader should report no change")
	}
	if promoteFrontedEndpoint(list, 9) != nil {
		t.Fatal("an out-of-range index should report no change")
	}
}
