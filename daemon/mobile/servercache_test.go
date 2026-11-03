package mobile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func cachedServerJSON(overrides map[string]any) json.RawMessage {
	s := map[string]any{
		"id": "fra-1", "name": "Frankfurt", "region": "eu-central", "country": "DE",
		"cloak": map[string]any{"remoteHost": "1.2.3.4", "uid": "uid-1", "publicKey": "pk-1"},
	}
	for key, value := range overrides {
		if value == nil {
			delete(s, key)
			continue
		}
		s[key] = value
	}
	b, _ := json.Marshal(s)
	return b
}

func TestIsCachedServerPortsTheDesktopRules(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"well formed", cachedServerJSON(nil), true},
		{"missing id", cachedServerJSON(map[string]any{"id": nil}), false},
		{"missing name", cachedServerJSON(map[string]any{"name": nil}), false},
		{"missing region", cachedServerJSON(map[string]any{"region": nil}), false},
		{"missing country", cachedServerJSON(map[string]any{"country": nil}), false},
		{"blank id", cachedServerJSON(map[string]any{"id": "   "}), false},
		{"empty name", cachedServerJSON(map[string]any{"name": ""}), false},
		{"no cloak", cachedServerJSON(map[string]any{"cloak": nil}), false},
		{"cloak string", cachedServerJSON(map[string]any{"cloak": "nope"}), false},
		{"cloak without host", cachedServerJSON(map[string]any{"cloak": map[string]any{"uid": "u", "publicKey": "p"}}), false},
		{"cloak without uid", cachedServerJSON(map[string]any{"cloak": map[string]any{"remoteHost": "h", "publicKey": "p"}}), false},
		{"cloak without key", cachedServerJSON(map[string]any{"cloak": map[string]any{"remoteHost": "h", "uid": "u"}}), false},
		{"partial reality block", cachedServerJSON(map[string]any{"reality": map[string]any{"remotePort": 443}}), true},
		{"empty shadowsocks block", cachedServerJSON(map[string]any{"shadowsocks": map[string]any{}}), true},
		{"naive string", cachedServerJSON(map[string]any{"naive": "nope"}), false},
		{"snowflake array", cachedServerJSON(map[string]any{"snowflake": []any{}}), false},
		{"null naive", json.RawMessage(strings.Replace(string(cachedServerJSON(nil)), `{`, `{"naive":null,`, 1)), true},
		{"null", json.RawMessage(`null`), false},
		{"string", json.RawMessage(`"fra-1"`), false},
		{"array", json.RawMessage(`[` + string(cachedServerJSON(nil)) + `]`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := isCachedServer(tc.raw); got != tc.want {
				t.Fatalf("isCachedServer(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRestoreCachedServersValidatesAndDedupes(t *testing.T) {
	list := []json.RawMessage{
		cachedServerJSON(map[string]any{"id": "b"}),
		json.RawMessage(`"junk"`),
		cachedServerJSON(map[string]any{"id": "a"}),
		cachedServerJSON(map[string]any{"id": "b", "name": "Duplicate"}),
		cachedServerJSON(map[string]any{"cloak": nil}),
	}
	blob, _ := json.Marshal(map[string]any{"owner": "o", "servers": list})

	restored := restoreCachedServers(string(blob), "o")
	var ids []string
	for _, s := range restored {
		ids = append(ids, s.ID)
	}
	if !reflect.DeepEqual(ids, []string{"b", "a"}) {
		t.Fatalf("ids = %v, want [b a]", ids)
	}
	if restored[0].Name != "Frankfurt" {
		t.Fatalf("the first entry for an id must win, got %q", restored[0].Name)
	}

	for name, raw := range map[string]string{
		"absent":          "",
		"not json":        "{nope",
		"bare array":      "[" + string(list[0]) + "]",
		"servers object":  `{"owner":"o","servers":{}}`,
		"another session": string(blob),
	} {
		owner := "o"
		if name == "another session" {
			owner = "x"
		}
		if got := restoreCachedServers(raw, owner); len(got) != 0 {
			t.Errorf("%s: restored %d servers", name, len(got))
		}
	}
}

func TestServerCacheRoundTripsEveryTransport(t *testing.T) {
	load := 40
	servers := []serverInfo{serverWithEveryTransport(t), {ID: "lon", Name: "London", Region: "eu-west", Country: "GB", Load: &load,
		Cloak: cloakInfo{RemoteHost: "5.6.7.8", UID: "u2", PublicKey: "pk2"}}}
	raw, ok := encodeServerCache(servers, "o")
	if !ok {
		t.Fatal("encode failed")
	}
	if got := restoreCachedServers(raw, "o"); !reflect.DeepEqual(got, servers) {
		t.Fatalf("round trip lost data:\n got %+v\nwant %+v", got, servers)
	}
	if _, ok := encodeServerCache(nil, "o"); ok {
		t.Fatal("an empty list must not replace the last good one")
	}
}
