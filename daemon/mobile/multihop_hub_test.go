//go:build android || ios

package mobile

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// hopServers returns an exit and a relay-capable entry, the entry carrying every transport.
func hopServers(t *testing.T) (exit, entry serverInfo) {
	t.Helper()
	entry = *decodeServer(t, hubServerJSON)
	entry.Multihop = true
	exit = serverInfo{ID: "fra-1", Name: "Frankfurt", Multihop: true, Cloak: cloakInfo{RemoteHost: "198.51.100.1"}}
	return exit, entry
}

func TestPrepareViaBuildsAHopProfile(t *testing.T) {
	exit, entry := hopServers(t)
	h := signedInWith(t, exit, entry)
	answerRegister(h, map[string]any{"hop": fullHop()})

	raw, err := PrepareVia("fra-1", "lon-1")
	if err != nil {
		t.Fatalf("PrepareVia: %v", err)
	}
	body := registerBody(t, h)
	if string(body["region"]) != `"fra-1"` || string(body["entryRegion"]) != `"lon-1"` {
		t.Fatalf("register body = %v, want exit fra-1 via entry lon-1", body)
	}

	p := preparedNow()
	profile := p.profile
	switch {
	case profile.Hop == nil || *profile.Hop != *fullHop():
		t.Fatalf("hop = %+v, want the hub's", profile.Hop)
	case profile.Cloak.ProxyMethod != "wireguard-via-fra-1" || profile.Cloak.RemoteHost != "203.0.113.10":
		t.Fatalf("cloak = %+v, want the entry dialled and the exit requested", profile.Cloak)
	case profile.Reality.TargetPort != 40001 || profile.Shadowsocks.TargetHost != "127.0.0.1":
		t.Fatalf("reality %+v shadowsocks %+v, want the entry's hop port", profile.Reality, profile.Shadowsocks)
	case !bytes.Equal(p.serverPubKeyRaw, testServerPub):
		t.Fatal("the peer is not the exit's key from the register answer")
	case p.serverID != "fra-1" || p.serverName != "Frankfurt" || p.entryServerID != "lon-1" || p.entryServerName != "London":
		t.Fatalf("prepared %s/%s via %s/%s, want Frankfurt via London", p.serverID, p.serverName, p.entryServerID, p.entryServerName)
	}

	var cfg tunnelConfigJSON
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("decode tunnel config: %v", err)
	}
	if cfg.ServerID != "fra-1" || cfg.ServerName != "Frankfurt" || cfg.EntryServerID != "lon-1" || cfg.EntryServerName != "London" {
		t.Fatalf("tunnel config = %+v, want Frankfurt via London", cfg)
	}
}

func TestPrepareViaRejectsBadEntriesBeforeTheHub(t *testing.T) {
	exit, entry := hopServers(t)
	relayless := entry
	relayless.ID, relayless.Multihop = "nyc-1", false
	tests := []struct {
		name, entryID, want string
	}{
		{"not a relay", "nyc-1", "nyc-1 cannot relay multihop"},
		{"the exit itself", "fra-1", "must be different servers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := signedInWith(t, exit, entry, relayless)
			_, err := PrepareVia("fra-1", tt.entryID)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("PrepareVia = %v, want %q", err, tt.want)
			}
			if got := h.requests(); len(got) != 0 {
				t.Fatalf("hub saw %+v before the entry was checked", got)
			}
		})
	}
}

func TestPrepareViaRefusesAPartialHop(t *testing.T) {
	exit, entry := hopServers(t)
	h := signedInWith(t, exit, entry)
	previous := &preparedTunnel{serverID: "old"}
	stagePrepared(previous)
	partial := fullHop()
	partial.SingBoxPort = 0
	answerRegister(h, map[string]any{"hop": partial})

	if _, err := PrepareVia("fra-1", "lon-1"); err == nil || !strings.Contains(err.Error(), "singBoxPort") {
		t.Fatalf("PrepareVia = %v, want a partial-hop refusal", err)
	}
	if p := preparedNow(); p != previous {
		t.Fatal("a refused hop replaced the prepared config")
	}
}

func TestPrepareViaRequiresTheHop(t *testing.T) {
	exit, entry := hopServers(t)
	h := signedInWith(t, exit, entry)
	answerRegister(h, nil)

	if _, err := PrepareVia("fra-1", "lon-1"); err == nil || !strings.Contains(err.Error(), "multihop route") {
		t.Fatalf("PrepareVia = %v, want a missing-hop refusal", err)
	}
	if p := preparedNow(); p != nil {
		t.Fatalf("prepared %+v without a hop", p)
	}
}

func TestPrepareViaSurfacesHopCodes(t *testing.T) {
	exit, entry := hopServers(t)
	h := signedInWith(t, exit, entry)
	h.handle("/api/register", func(fakeRequest) (int, any) {
		return http.StatusBadRequest, map[string]string{
			"error": "Entry region does not relay multihop yet: lon-1", "code": "ENTRY_REGION_NO_MULTIHOP",
		}
	})

	if _, err := PrepareVia("fra-1", "lon-1"); hubErrorCode(err) != "ENTRY_REGION_NO_MULTIHOP" {
		t.Fatalf("PrepareVia = %v, want the hub's ENTRY_REGION_NO_MULTIHOP", err)
	}
}
