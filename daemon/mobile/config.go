package mobile

// Mobile settings, persisted through the host SecretStore. Ports the
// validation in apps/desktop/src/shared/{mtu,dns}.ts.

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"runtime"
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
// are independent switches, matching the desktop toggles.
type config struct {
	PreferredTransport string     `json:"preferredTransport"`
	CustomDNS          []string   `json:"customDns"`
	MTU                int        `json:"mtu"`
	AllowLAN           bool       `json:"allowLan"`
	AutoConnect        bool       `json:"autoConnect"`
	LastServerID       string     `json:"lastServerId"`
	HubMethods         hubMethods `json:"hubMethods"`
	// DeadDrop lets the hub search fetch signed replacement addresses once
	// every method is out of ones that work.
	DeadDrop bool `json:"deadDrop"`
	// PostQuantum offers an ML-KEM key at the next Prepare; the exit node mixes
	// the agreed secret into WireGuard as the peer's pre-shared key.
	PostQuantum bool `json:"postQuantum"`
}

// defaultConfig mirrors the desktop defaults: cascade on, hub methods all
// enabled, no custom DNS.
func defaultConfig() config {
	return config{
		PreferredTransport: "auto",
		MTU:                mtuDefault,
		HubMethods:         defaultHubMethods(),
		DeadDrop:           true,
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

// loopbackDNSAllowed: Android and desktop can run a local DNS proxy, but no iOS
// app can keep one listening, so there a loopback resolver fails every lookup.
const loopbackDNSAllowed = runtime.GOOS != "ios"

// normalizeCustomDNS keeps only well-formed, de-duplicated IPv4 literals. The
// WireGuard data plane takes IPv4 DNS only.
func normalizeCustomDNS(values []string) []string {
	return normalizeCustomDNSFor(values, loopbackDNSAllowed)
}

func normalizeCustomDNSFor(values []string, allowLoopback bool) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		for _, candidate := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			address, ok := parseIPv4(candidate)
			if !ok || unroutableDNS(address) || (!allowLoopback && loopbackDNS(address)) {
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

// unroutableDNSRanges can never hold a resolver behind the tunnel. Loopback is
// judged per platform by loopbackDNSAllowed.
var unroutableDNSRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/3"),
}

func unroutableDNS(address string) bool {
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return true
	}
	for _, prefix := range unroutableDNSRanges {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// loopbackDNS covers all of 127.0.0.0/8; ::1 never gets here, since only IPv4 is kept.
func loopbackDNS(address string) bool {
	ip, err := netip.ParseAddr(address)
	return err != nil || ip.IsLoopback()
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
	case "auto", "cloak", "reality", "shadowsocks", "hysteria2", "anytls":
		return true
	default:
		return false
	}
}

// decodeConfig reads a stored blob, falling back to defaults on anything
// unreadable — settings are a convenience, never a reason to fail to connect.
func decodeConfig(raw string) config {
	parsed, _ := parseConfig(raw)
	return parsed
}

// parseConfig is decodeConfig that also reports a blob it could not read.
func parseConfig(raw string) (config, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultConfig(), nil
	}
	parsed := defaultConfig()
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return defaultConfig(), fmt.Errorf("decode stored config: %w", err)
	}
	return parsed.sanitize(), nil
}

func encodeConfig(c config) (string, error) {
	b, err := json.Marshal(c.sanitize())
	if err != nil {
		return "", err
	}
	return string(b), nil
}
