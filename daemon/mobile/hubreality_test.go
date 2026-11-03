package mobile

import "testing"

func realityCreds(host string) hubRealityCreds {
	return hubRealityCreds{
		RemoteHost: host,
		RemotePort: 443,
		UUID:       "cf550715-b9c8-4a58-a610-dc5cc73e36f4",
		PublicKey:  "8rifnTuJS517L1ysYdVaSvCntor5nkC3dn1XGqWMYlg",
		ShortID:    "355adc938875db2a",
		ServerName: "swdist.apple.com",
	}
}

func TestHubRealityCredsValidation(t *testing.T) {
	mutate := func(edit func(*hubRealityCreds)) hubRealityCreds {
		c := realityCreds("95.179.239.1")
		edit(&c)
		return c
	}
	tests := []struct {
		name  string
		creds hubRealityCreds
		want  bool
	}{
		{"complete", realityCreds("95.179.239.1"), true},
		{"domain host needs DNS", realityCreds("node.example.com"), false},
		{"leading-zero octet", realityCreds("95.179.239.01"), false},
		{"port zero", mutate(func(c *hubRealityCreds) { c.RemotePort = 0 }), false},
		{"bad uuid", mutate(func(c *hubRealityCreds) { c.UUID = "not-a-uuid" }), false},
		{"short public key", mutate(func(c *hubRealityCreds) { c.PublicKey = "abc" }), false},
		{"padded base64 key", mutate(func(c *hubRealityCreds) { c.PublicKey = "8rifnTuJS517L1ysYdVaSvCntor5nkC3dn1XGqWMYl=" }), false},
		{"odd short id", mutate(func(c *hubRealityCreds) { c.ShortID = "abc" }), false},
		{"long short id", mutate(func(c *hubRealityCreds) { c.ShortID = "00112233445566778899" }), false},
		{"empty short id", mutate(func(c *hubRealityCreds) { c.ShortID = "" }), false},
		{"bare-label sni", mutate(func(c *hubRealityCreds) { c.ServerName = "localhost" }), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.creds.valid(); got != tt.want {
				t.Fatalf("valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRestoreHubRealityNormalizes(t *testing.T) {
	padded := realityCreds(" 95.179.239.1 ")
	padded.UUID = "CF550715-B9C8-4A58-A610-DC5CC73E36F4"
	padded.ShortID = "355ADC938875DB2A"
	padded.ServerName = "SWDIST.apple.com"

	got := restoreHubReality([]hubRealityCreds{padded, realityCreds("95.179.239.1")})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want the duplicate folded away", len(got))
	}
	if got[0] != realityCreds("95.179.239.1") {
		t.Fatalf("got %+v, want the canonical form", got[0])
	}
}

func TestSeedHubRealityFallsBackToTheShippedNode(t *testing.T) {
	got := seedHubReality(nil)
	if len(got) == 0 || len(got) != len(defaultHubReality) {
		t.Fatalf("got %d entries, want the shipped nodes", len(got))
	}
	for _, c := range got {
		if !c.valid() {
			t.Fatalf("shipped node %+v fails validation", c)
		}
	}
}

func TestMergeAdvertisedHubRealityKeepsTheLeader(t *testing.T) {
	current := []hubRealityCreds{realityCreds("192.0.2.2")}
	got := mergeAdvertisedHubReality(current, []hubRealityCreds{realityCreds("192.0.2.1"), realityCreds("192.0.2.2")})
	if len(got) != 2 || got[0].RemoteHost != "192.0.2.2" {
		t.Fatalf("got %+v, want the last-good node first", got)
	}
	if mergeAdvertisedHubReality(current, nil) != nil {
		t.Fatal("an empty advertisement must leave the cache alone")
	}
}
