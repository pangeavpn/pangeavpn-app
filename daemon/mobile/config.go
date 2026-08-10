package mobile

// Mobile settings, persisted through the host SecretStore. Ports the
// validation in apps/desktop/src/shared/{mtu,dns}.ts.

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	// mtuMin is IPv6's minimum link MTU; below it IPv6 breaks outright.
	mtuMin = 1280
	// mtuMax is wg-quick's ceiling for a 1500-byte underlay.
	mtuMax = 1420
	// mtuDefault is the conservative value used before the setting existed.
	mtuDefault = 1380
)

// config is the mobile equivalent of the desktop settings surface. Hub methods
// are independent switches, matching desktop's four toggles.
type config struct {
	PreferredTransport string     `json:"preferredTransport"`
	CustomDNS          []string   `json:"customDns"`
	MTU                int        `json:"mtu"`
	AllowLAN           bool       `json:"allowLan"`
	AutoConnect        bool       `json:"autoConnect"`
	LastServerID       string     `json:"lastServerId"`
	HubMethods         hubMethods `json:"hubMethods"`
}

// defaultConfig mirrors the desktop defaults: cascade on, hub methods all
// enabled, no custom DNS.
func defaultConfig() config {
	return config{
		PreferredTransport: "auto",
		MTU:                mtuDefault,
		HubMethods:         defaultHubMethods(),
	}
}

// normalizeMTU range-checks an MTU from untrusted input, falling back to the
// default rather than failing a connect.
func normalizeMTU(value int) int {
	if value < mtuMin || value > mtuMax {
		return mtuDefault
	}
	return value
}

// normalizeCustomDNS keeps only well-formed, de-duplicated IPv4 literals. The
// WireGuard data plane takes IPv4 DNS only.
func normalizeCustomDNS(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		for _, candidate := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			address, ok := parseIPv4(candidate)
			if !ok {
				continue
			}
			if _, dup := seen[address]; dup {
				continue
			}
			seen[address] = struct{}{}
			normalized = append(normalized, address)
		}
	}
	return normalized
}

// parseIPv4 accepts dotted-quad only, rejecting the shorthand forms net.ParseIP
// tolerates so a typo cannot silently become a different server.
func parseIPv4(value string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 4 {
		return "", false
	}
	octets := make([]string, 0, 4)
	for _, part := range parts {
		if part == "" || len(part) > 3 {
			return "", false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return "", false
		}
		octets = append(octets, strconv.Itoa(n))
	}
	return strings.Join(octets, "."), true
}

// resolveDNS prefers the user's servers, falling back to what the hub assigned.
func resolveDNS(serverDNS string, custom []string) []string {
	if len(custom) > 0 {
		return append([]string(nil), custom...)
	}
	var servers []string
	for _, part := range strings.Split(serverDNS, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			servers = append(servers, trimmed)
		}
	}
	return servers
}

// sanitize applies every field rule, so a hand-edited or partial blob can never
// produce an unusable tunnel.
func (c config) sanitize() config {
	out := c
	out.MTU = normalizeMTU(c.MTU)
	out.CustomDNS = normalizeCustomDNS(c.CustomDNS)
	out.HubMethods = c.HubMethods.normalize()
	if !isKnownTransportChoice(out.PreferredTransport) {
		out.PreferredTransport = "auto"
	}
	return out
}

func isKnownTransportChoice(kind string) bool {
	switch kind {
	case "auto", "cloak", "reality", "shadowsocks", "hysteria2", "snowflake":
		return true
	default:
		return false
	}
}

// decodeConfig reads a stored blob, falling back to defaults on anything
// unreadable — settings are a convenience, never a reason to fail to connect.
func decodeConfig(raw string) config {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultConfig()
	}
	parsed := defaultConfig()
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return defaultConfig()
	}
	return parsed.sanitize()
}

func encodeConfig(c config) (string, error) {
	b, err := json.Marshal(c.sanitize())
	if err != nil {
		return "", err
	}
	return string(b), nil
}
