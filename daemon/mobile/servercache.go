package mobile

// Ports apps/desktop/src/shared/cachedServers.ts: the last good region list,
// so a launch with the hub blocked can still provision.

import (
	"bytes"
	"encoding/json"
	"strings"
)

// keyServers holds transport credentials, so it lives in the SecretStore.
const keyServers = "servers"

type cachedServers struct {
	Owner   string            `json:"owner"`
	Servers []json.RawMessage `json:"servers"`
}

// encodeServerCache refuses an empty list: it must never replace a good one.
func encodeServerCache(servers []serverInfo, owner string) (string, bool) {
	if len(servers) == 0 {
		return "", false
	}
	raw := make([]json.RawMessage, 0, len(servers))
	for _, s := range servers {
		b, err := json.Marshal(s)
		if err != nil {
			return "", false
		}
		raw = append(raw, b)
	}
	b, err := json.Marshal(cachedServers{Owner: owner, Servers: raw})
	if err != nil {
		return "", false
	}
	return string(b), true
}

// restoreCachedServers validates each entry and dedupes by id, first wins.
// Another session's list is ignored, as it carries another device's keys.
func restoreCachedServers(raw, owner string) []serverInfo {
	var blob cachedServers
	if raw == "" || json.Unmarshal([]byte(raw), &blob) != nil || blob.Owner != owner {
		return nil
	}
	out := make([]serverInfo, 0, len(blob.Servers))
	seen := map[string]bool{}
	for _, entry := range blob.Servers {
		s, ok := isCachedServer(entry)
		if !ok || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out
}

// isCachedServer checks only what buildProfile needs; a typed decode already
// refuses a transport block that is not an object or null.
func isCachedServer(raw json.RawMessage) (serverInfo, bool) {
	var s serverInfo
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) || json.Unmarshal(raw, &s) != nil {
		return serverInfo{}, false
	}
	for _, required := range []string{s.ID, s.Name, s.Region, s.Country, s.Cloak.RemoteHost, s.Cloak.UID, s.Cloak.PublicKey} {
		if strings.TrimSpace(required) == "" {
			return serverInfo{}, false
		}
	}
	return s, true
}
