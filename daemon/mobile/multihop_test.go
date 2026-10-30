package mobile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestRegionKeyOf(t *testing.T) {
	for id, want := range map[string]string{
		"eu-west-1": "eu-west",
		"eu-west":   "eu-west",
		"ap-2-10":   "ap-2",
		"lon-":      "lon-",
		"":          "",
	} {
		if got := regionKeyOf(id); got != want {
			t.Errorf("regionKeyOf(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestMultihopEntriesSkipsTheExitRegion(t *testing.T) {
	load := func(n int) *int { return &n }
	servers := []serverInfo{
		{ID: "eu-west-1", Multihop: true, Load: load(1)},
		{ID: "eu-west-2", Multihop: true, Load: load(5)},
		{ID: "us-east-1", Multihop: true, Load: load(50)},
		{ID: "ap-south-1", Multihop: true},
		{ID: "de-1", Load: load(1)},
		{ID: "us-west-1", Multihop: true, Load: load(50)},
		{ID: "sg-1", Multihop: true, Load: load(10)},
	}
	want := []string{"sg-1", "us-east-1", "us-west-1", "ap-south-1"}
	if got := multihopEntries(servers, "eu-west-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := multihopEntries(nil, "eu-west-1"); got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty, non-nil list", got)
	}
}

func TestRouteServersNamesWhatIsMissing(t *testing.T) {
	srvs := []serverInfo{{ID: "fra-1"}, {ID: "lon-1"}}
	tests := []struct {
		name, exit, entry, wantExit, wantEntry, missing string
	}{
		{"single hop", "fra-1", "", "fra-1", "", ""},
		{"two hops", "fra-1", "lon-1", "fra-1", "lon-1", ""},
		{"unknown exit", "nyc-1", "lon-1", "", "", "nyc-1"},
		{"unknown entry", "fra-1", "nyc-1", "", "", "nyc-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exit, entry, missing := routeServers(srvs, tt.exit, tt.entry)
			id := func(s *serverInfo) string {
				if s == nil {
					return ""
				}
				return s.ID
			}
			if id(exit) != tt.wantExit || id(entry) != tt.wantEntry || missing != tt.missing {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", id(exit), id(entry), missing, tt.wantExit, tt.wantEntry, tt.missing)
			}
		})
	}
}

func TestValidateHopEntry(t *testing.T) {
	tests := []struct {
		name    string
		entry   serverInfo
		wantErr string
	}{
		{"the exit itself", serverInfo{ID: "fra-1", Multihop: true}, "different"},
		{"not a relay", serverInfo{ID: "lon-1"}, "lon-1 cannot relay multihop"},
		{"a relay", serverInfo{ID: "lon-1", Multihop: true}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHopEntry("fra-1", &tt.entry)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("got %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func fullHop() *state.HopProfile {
	return &state.HopProfile{
		SingBoxPort: 40001, CloakProxyMethod: "wireguard-via-fra-1", NaiveBridgePort: 40002,
		EntryRegion: "lon-1", ExitRegion: "fra-1",
	}
}

func TestHopProfileRelaysThroughTheEntry(t *testing.T) {
	entry := decodeServer(t, hubServerJSON)
	exit := &serverInfo{ID: "fra-1", Name: "Frankfurt", Cloak: cloakInfo{RemoteHost: "198.51.100.1"}}

	got, err := hopProfile(exit, entry, fullHop())
	if err != nil {
		t.Fatalf("hopProfile: %v", err)
	}
	if got.ID != "auto-fra-1-via-lon-1" || got.Hop == nil || *got.Hop != *fullHop() {
		t.Fatalf("got id %q hop %+v, want the exit-via-entry id and the hub's hop", got.ID, got.Hop)
	}
	if got.Cloak.RemoteHost != "203.0.113.10" || got.Cloak.ProxyMethod != "wireguard-via-fra-1" {
		t.Fatalf("cloak = %+v, want it to dial the entry and ask for the exit", got.Cloak)
	}
	if got.Reality.TargetPort != 40001 || got.Hysteria2.TargetPort != 40001 {
		t.Fatalf("sing-box targets = %d/%d, want the hop port", got.Reality.TargetPort, got.Hysteria2.TargetPort)
	}
	if ss := got.Shadowsocks; ss.TargetHost != "127.0.0.1" || ss.TargetPort != 40001 {
		t.Fatalf("shadowsocks target = %s:%d, want the entry's loopback hop port", ss.TargetHost, ss.TargetPort)
	}
	if got.Naive.BridgePort != 40002 {
		t.Fatalf("naive bridge = %d, want 40002", got.Naive.BridgePort)
	}
	if got.Snowflake != nil {
		t.Fatal("a hop kept snowflake, which the hub never routes through an entry")
	}
}

func TestHopProfileRefusesAHopForAnotherRoute(t *testing.T) {
	entry := decodeServer(t, hubServerJSON)
	exit := &serverInfo{ID: "fra-1", Name: "Frankfurt"}
	wrongEntry, wrongExit, swapped := fullHop(), fullHop(), fullHop()
	wrongEntry.EntryRegion = "par-1"
	wrongExit.ExitRegion = "ams-1"
	swapped.EntryRegion, swapped.ExitRegion = "fra-1", "lon-1"
	for name, hop := range map[string]*state.HopProfile{"another entry": wrongEntry, "another exit": wrongExit, "swapped": swapped} {
		t.Run(name, func(t *testing.T) {
			got, err := hopProfile(exit, entry, hop)
			if err == nil || !strings.Contains(err.Error(), "does not match") {
				t.Fatalf("got %+v, %v, want a mismatch refusal", got, err)
			}
		})
	}
}

func TestHopProfileRefusesAPartialHop(t *testing.T) {
	partial := fullHop()
	partial.SingBoxPort = 0
	entry := decodeServer(t, hubServerJSON)
	exit := &serverInfo{ID: "fra-1"}
	for name, hop := range map[string]*state.HopProfile{"no hop": nil, "no singBoxPort": partial} {
		t.Run(name, func(t *testing.T) {
			if got, err := hopProfile(exit, entry, hop); err == nil {
				t.Fatalf("got %+v, want an error", got)
			}
		})
	}
}
