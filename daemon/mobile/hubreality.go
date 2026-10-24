package mobile

// Control-plane REALITY credentials: a node user pinned to the hub on 443.
// Ports apps/desktop/src/shared/hubRealityCreds.ts.

import (
	"regexp"
	"strings"
)

type hubRealityCreds struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	UUID       string `json:"uuid"`
	// PublicKey is X25519, base64url without padding.
	PublicKey string `json:"publicKey"`
	ShortID   string `json:"shortId"`
	// ServerName is the cover SNI the node's REALITY inbound answers to.
	ServerName string `json:"serverName"`
}

// defaultHubReality is shipped so an install that has never reached the hub
// still has this route in, like defaultHubShadowsocks.
var defaultHubReality = []hubRealityCreds{{
	RemoteHost: "95.179.239.1",
	RemotePort: 443,
	UUID:       "cf550715-b9c8-4a58-a610-dc5cc73e36f4",
	PublicKey:  "8rifnTuJS517L1ysYdVaSvCntor5nkC3dn1XGqWMYlg",
	ShortID:    "355adc938875db2a",
	ServerName: "swdist.apple.com",
}}

var (
	realityUUID      = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	realityPublicKey = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	realityShortID   = regexp.MustCompile(`(?i)^(?:[0-9a-f]{2}){1,8}$`)
)

// valid accepts an IP literal only: a name would need DNS first, which the
// path exists to avoid.
func (c hubRealityCreds) valid() bool {
	_, sniOK := normalizeFrontedEndpoint(c.ServerName)
	return isIPv4Literal(strings.TrimSpace(c.RemoteHost)) &&
		c.RemotePort > 0 && c.RemotePort <= 65535 &&
		realityUUID.MatchString(strings.TrimSpace(c.UUID)) &&
		realityPublicKey.MatchString(strings.TrimSpace(c.PublicKey)) &&
		realityShortID.MatchString(strings.TrimSpace(c.ShortID)) &&
		sniOK
}

func (c hubRealityCreds) normalized() hubRealityCreds {
	serverName, _ := normalizeFrontedEndpoint(c.ServerName)
	return hubRealityCreds{
		RemoteHost: strings.TrimSpace(c.RemoteHost),
		RemotePort: c.RemotePort,
		UUID:       strings.ToLower(strings.TrimSpace(c.UUID)),
		PublicKey:  strings.TrimSpace(c.PublicKey),
		ShortID:    strings.ToLower(strings.TrimSpace(c.ShortID)),
		ServerName: serverName,
	}
}

var realityCredKind = credKind[hubRealityCreds]{
	valid:     hubRealityCreds.valid,
	normalize: hubRealityCreds.normalized,
	same:      func(a, b hubRealityCreds) bool { return a == b },
}

func restoreHubReality(stored []hubRealityCreds) []hubRealityCreds {
	return restoreCached(realityCredKind, stored)
}

func mergeAdvertisedHubReality(current, advertised []hubRealityCreds) []hubRealityCreds {
	return mergeAdvertised(realityCredKind, current, advertised)
}

func seedHubReality(stored []hubRealityCreds) []hubRealityCreds {
	return seedCached(realityCredKind, stored, defaultHubReality)
}
