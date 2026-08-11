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

func (c hubShadowsocksCreds) valid() bool {
	return strings.TrimSpace(c.RemoteHost) != "" &&
		c.RemotePort > 0 && c.RemotePort <= 65535 &&
		strings.TrimSpace(c.Method) != "" &&
		c.Password != ""
}

func (c hubShadowsocksCreds) same(other hubShadowsocksCreds) bool {
	return c.RemoteHost == other.RemoteHost && c.RemotePort == other.RemotePort &&
		c.Method == other.Method && c.Password == other.Password
}

// restoreCachedCreds validates and deduplicates, preserving order.
func restoreCachedCreds(stored []hubShadowsocksCreds) []hubShadowsocksCreds {
	out := make([]hubShadowsocksCreds, 0, len(stored))
	for _, candidate := range stored {
		if !candidate.valid() || containsCreds(out, candidate) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// mergeAdvertisedCreds caches every node the hub named. Caching only one means
// a rotation past that node locks the client out of its last remaining path.
func mergeAdvertisedCreds(current, advertised []hubShadowsocksCreds) []hubShadowsocksCreds {
	next := restoreCachedCreds(advertised)
	if len(next) == 0 {
		return nil
	}
	if len(current) > 0 {
		if at := indexOfCreds(next, current[0]); at > 0 {
			item := next[at]
			rest := append(next[:at:at], next[at+1:]...)
			next = append([]hubShadowsocksCreds{item}, rest...)
		}
	}
	if sameCredsList(next, current) {
		return nil
	}
	return next
}

// promoteCreds moves the entry that just worked to the front.
func promoteCreds(list []hubShadowsocksCreds, index int) []hubShadowsocksCreds {
	if index <= 0 || index >= len(list) {
		return nil
	}
	copied := append([]hubShadowsocksCreds(nil), list...)
	item := copied[index]
	rest := append(copied[:index:index], copied[index+1:]...)
	return append([]hubShadowsocksCreds{item}, rest...)
}

func containsCreds(list []hubShadowsocksCreds, value hubShadowsocksCreds) bool {
	return indexOfCreds(list, value) >= 0
}

func indexOfCreds(list []hubShadowsocksCreds, value hubShadowsocksCreds) int {
	for i, item := range list {
		if item.same(value) {
			return i
		}
	}
	return -1
}

func sameCredsList(a, b []hubShadowsocksCreds) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].same(b[i]) {
			return false
		}
	}
	return true
}
