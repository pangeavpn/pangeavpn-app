package mobile

import "testing"

func creds(host string, port int) hubShadowsocksCreds {
	return hubShadowsocksCreds{RemoteHost: host, RemotePort: port, Method: "2022-blake3-aes-128-gcm", Password: "pw"}
}

func TestHubShadowsocksCredsValidation(t *testing.T) {
	tests := []struct {
		name  string
		creds hubShadowsocksCreds
		want  bool
	}{
		{"complete", creds("1.2.3.4", 8388), true},
		{"blank host", creds("   ", 8388), false},
		{"port zero", creds("1.2.3.4", 0), false},
		{"port too high", creds("1.2.3.4", 70000), false},
		{"negative port", creds("1.2.3.4", -1), false},
		{"no method", hubShadowsocksCreds{RemoteHost: "1.2.3.4", RemotePort: 8388, Password: "pw"}, false},
		{"no password", hubShadowsocksCreds{RemoteHost: "1.2.3.4", RemotePort: 8388, Method: "m"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.creds.valid(); got != tt.want {
				t.Fatalf("valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRestoreCachedCredsDropsBadAndDuplicates(t *testing.T) {
	got := restoreCachedCreds([]hubShadowsocksCreds{
		creds("1.2.3.4", 8388), creds("", 8388), creds("1.2.3.4", 8388), creds("5.6.7.8", 8389),
	})
	if len(got) != 2 || got[0].RemoteHost != "1.2.3.4" || got[1].RemoteHost != "5.6.7.8" {
		t.Fatalf("got %+v, want two distinct valid entries", got)
	}
}

// Caching one node means a rotation past it locks the client out entirely.
func TestMergeAdvertisedCredsKeepsEveryNode(t *testing.T) {
	got := mergeAdvertisedCreds(nil, []hubShadowsocksCreds{
		creds("1.2.3.4", 8388), creds("5.6.7.8", 8389), creds("9.9.9.9", 8390),
	})
	if len(got) != 3 {
		t.Fatalf("got %d entries, want all 3 cached", len(got))
	}
}

func TestMergeAdvertisedCredsKeepsCacheOnEmptyAdvertisement(t *testing.T) {
	current := []hubShadowsocksCreds{creds("1.2.3.4", 8388)}
	if got := mergeAdvertisedCreds(current, nil); got != nil {
		t.Fatalf("got %+v, want the cache left alone", got)
	}
	if got := mergeAdvertisedCreds(current, []hubShadowsocksCreds{creds("", 0)}); got != nil {
		t.Fatalf("got %+v, want the cache left alone", got)
	}
}

func TestMergeAdvertisedCredsKeepsTheWorkingLeaderInFront(t *testing.T) {
	current := []hubShadowsocksCreds{creds("5.6.7.8", 8389)}
	got := mergeAdvertisedCreds(current, []hubShadowsocksCreds{creds("1.2.3.4", 8388), creds("5.6.7.8", 8389)})
	if len(got) != 2 || got[0].RemoteHost != "5.6.7.8" {
		t.Fatalf("got %+v, want the last-good node first", got)
	}
}

func TestMergeAdvertisedCredsReportsNoChange(t *testing.T) {
	current := []hubShadowsocksCreds{creds("1.2.3.4", 8388)}
	if got := mergeAdvertisedCreds(current, []hubShadowsocksCreds{creds("1.2.3.4", 8388)}); got != nil {
		t.Fatalf("got %+v, want nil so the caller skips a write", got)
	}
}

func TestPromoteCreds(t *testing.T) {
	list := []hubShadowsocksCreds{creds("a", 1), creds("b", 2), creds("c", 3)}
	got := promoteCreds(list, 2)
	if len(got) != 3 || got[0].RemoteHost != "c" || got[1].RemoteHost != "a" {
		t.Fatalf("got %+v, want c promoted", got)
	}
	if list[0].RemoteHost != "a" {
		t.Fatal("the input must not be mutated")
	}
	if promoteCreds(list, 0) != nil || promoteCreds(list, 9) != nil {
		t.Fatal("leader and out-of-range should report no change")
	}
}
