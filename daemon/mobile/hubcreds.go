package mobile

// Control-plane Shadowsocks credentials. Ports
// apps/desktop/src/shared/hubShadowsocksCreds.ts.

import "strings"

type hubShadowsocksCreds struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Method     string `json:"method"`
	Password   string `json:"password"`
}

// defaultHubShadowsocks are shipped nodes, so an install that has never reached
// the hub still has this route in. Each listener relays only to the hub on 443.
var defaultHubShadowsocks = []hubShadowsocksCreds{
	{RemoteHost: "192.248.175.17", RemotePort: 8489, Method: "2022-blake3-aes-128-gcm", Password: "tUnJ/XLnK31LxBHhimZP5g=="},
	{RemoteHost: "64.176.205.92", RemotePort: 8489, Method: "2022-blake3-aes-128-gcm", Password: "RsMy+zj1BTQVj+jTa8ZPfA=="},
	{RemoteHost: "136.244.108.254", RemotePort: 8489, Method: "2022-blake3-aes-128-gcm", Password: "MIASyWggp3VO21RKPCB5cA=="},
	{RemoteHost: "136.244.66.46", RemotePort: 8489, Method: "2022-blake3-aes-128-gcm", Password: "YySUjWAvg9bDSTEYA0WdEw=="},
	{RemoteHost: "208.76.222.40", RemotePort: 8489, Method: "2022-blake3-aes-128-gcm", Password: "NulRaTBF6QqLx1dRaM1F3w=="},
}

// supportedShadowsocksMethods are the AEAD families the engine accepts; the
// legacy stream ciphers are unauthenticated and rejected there too.
var supportedShadowsocksMethods = map[string]struct{}{
	"aes-128-gcm":                   {},
	"aes-192-gcm":                   {},
	"aes-256-gcm":                   {},
	"chacha20-ietf-poly1305":        {},
	"xchacha20-ietf-poly1305":       {},
	"2022-blake3-aes-128-gcm":       {},
	"2022-blake3-aes-256-gcm":       {},
	"2022-blake3-chacha20-poly1305": {},
}

func (c hubShadowsocksCreds) valid() bool {
	_, supported := supportedShadowsocksMethods[strings.TrimSpace(c.Method)]
	return strings.TrimSpace(c.RemoteHost) != "" &&
		c.RemotePort > 0 && c.RemotePort <= 65535 &&
		supported &&
		strings.TrimSpace(c.Password) != ""
}

func (c hubShadowsocksCreds) normalized() hubShadowsocksCreds {
	return hubShadowsocksCreds{
		RemoteHost: strings.TrimSpace(c.RemoteHost),
		RemotePort: c.RemotePort,
		Method:     strings.TrimSpace(c.Method),
		Password:   strings.TrimSpace(c.Password),
	}
}

var shadowsocksCredKind = credKind[hubShadowsocksCreds]{
	valid:     hubShadowsocksCreds.valid,
	normalize: hubShadowsocksCreds.normalized,
	same:      func(a, b hubShadowsocksCreds) bool { return a == b },
}

func restoreCachedCreds(stored []hubShadowsocksCreds) []hubShadowsocksCreds {
	return restoreCached(shadowsocksCredKind, stored)
}

func mergeAdvertisedCreds(current, advertised []hubShadowsocksCreds) []hubShadowsocksCreds {
	return mergeAdvertised(shadowsocksCredKind, current, advertised)
}

func promoteCreds(list []hubShadowsocksCreds, index int) []hubShadowsocksCreds {
	return promoteEntry(list, index)
}

func seedHubShadowsocks(stored []hubShadowsocksCreds) []hubShadowsocksCreds {
	return seedCached(shadowsocksCredKind, stored, defaultHubShadowsocks)
}
