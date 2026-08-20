//go:build darwin || linux || windows

package wg

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type kv struct {
	key   string
	value string
}

// wgConfigToUAPI converts a stripped wg-quick INI config (containing only
// WireGuard-native keys: PrivateKey, ListenPort, PublicKey, PresharedKey,
// Endpoint, AllowedIPs, PersistentKeepalive) into the UAPI format accepted
// by device.IpcSet.
//
// Keys are converted from base64 to hex. AllowedIPs CSV values are split into
// separate allowed_ip= lines. The first [Peer] block emits replace_peers=true,
// public_key= is always emitted first within a peer regardless of source
// order (IpcSetOperation stays in device-config mode until it sees one).
func wgConfigToUAPI(wgConfig string) (string, error) {
	ifaceKV, peers, err := parseUAPISource(wgConfig)
	if err != nil {
		return "", err
	}

	var out []string
	privateKeySeen := false
	for _, e := range ifaceKV {
		line, err := convertInterfaceKV(e.key, e.value)
		if err != nil {
			return "", fmt.Errorf("uapi convert interface.%s: %w", e.key, err)
		}
		if strings.HasPrefix(line, "private_key=") {
			privateKeySeen = true
		}
		out = append(out, line)
	}
	if !privateKeySeen {
		return "", fmt.Errorf("uapi convert: config has no PrivateKey")
	}

	if len(peers) > 0 {
		out = append(out, "replace_peers=true")
	}
	for i, peer := range peers {
		lines, err := convertPeerKVs(peer)
		if err != nil {
			return "", fmt.Errorf("uapi convert peer[%d]: %w", i, err)
		}
		out = append(out, lines...)
	}

	return strings.Join(out, "\n") + "\n", nil
}

// parseUAPISource walks the stripped config and groups key/value pairs by
// section, starting a new peer group on every [Peer] header.
func parseUAPISource(wgConfig string) ([]kv, [][]kv, error) {
	scanner := bufio.NewScanner(strings.NewReader(wgConfig))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	var ifaceKV []kv
	var peers [][]kv
	section := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "peer" {
				peers = append(peers, nil)
			}
			continue
		}

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		for i, r := range value {
			if r == '#' || r == ';' {
				value = strings.TrimSpace(value[:i])
				break
			}
		}

		switch section {
		case "interface":
			ifaceKV = append(ifaceKV, kv{key, value})
		case "peer":
			peers[len(peers)-1] = append(peers[len(peers)-1], kv{key, value})
		default:
			return nil, nil, fmt.Errorf("uapi convert: unrecognized section %q", section)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("parse wg config for uapi: %w", err)
	}

	return ifaceKV, peers, nil
}

func convertInterfaceKV(key, value string) (string, error) {
	switch strings.ToLower(key) {
	case "privatekey":
		h, err := base64ToHex(value)
		if err != nil {
			return "", err
		}
		return "private_key=" + h, nil
	case "listenport":
		n, err := parseUAPIUint(value, 16)
		if err != nil {
			return "", fmt.Errorf("invalid ListenPort %q: %w", value, err)
		}
		return "listen_port=" + n, nil
	case "fwmark":
		n, err := parseUAPIUint(value, 32)
		if err != nil {
			return "", fmt.Errorf("invalid FWMark %q: %w", value, err)
		}
		return "fwmark=" + n, nil
	default:
		return "", fmt.Errorf("unrecognized Interface key %q", key)
	}
}

// convertPeerKVs emits public_key= first (see wgConfigToUAPI doc comment),
// followed by the peer's other fields in their original source order.
func convertPeerKVs(peer []kv) ([]string, error) {
	publicKeyIdx := -1
	for i, e := range peer {
		if strings.EqualFold(e.key, "publickey") {
			publicKeyIdx = i
			break
		}
	}
	if publicKeyIdx < 0 {
		return nil, fmt.Errorf("uapi convert: peer has no PublicKey")
	}

	h, err := base64ToHex(peer[publicKeyIdx].value)
	if err != nil {
		return nil, err
	}
	out := []string{"public_key=" + h, "replace_allowed_ips=true"}

	for i, e := range peer {
		if i == publicKeyIdx {
			continue
		}
		lines, err := convertPeerFieldKV(e.key, e.value)
		if err != nil {
			return nil, fmt.Errorf("uapi convert peer.%s: %w", e.key, err)
		}
		out = append(out, lines...)
	}
	return out, nil
}

func convertPeerFieldKV(key, value string) ([]string, error) {
	switch strings.ToLower(key) {
	case "presharedkey":
		h, err := base64ToHex(value)
		if err != nil {
			return nil, err
		}
		return []string{"preshared_key=" + h}, nil
	case "endpoint":
		return []string{"endpoint=" + value}, nil
	case "allowedips":
		lines, err := expandAllowedIPs(value)
		if err != nil {
			return nil, err
		}
		return lines, nil
	case "persistentkeepalive":
		n, err := parsePersistentKeepalive(value)
		if err != nil {
			return nil, fmt.Errorf("invalid PersistentKeepalive %q: %w", value, err)
		}
		return []string{"persistent_keepalive_interval=" + n}, nil
	default:
		return nil, fmt.Errorf("unrecognized Peer key %q", key)
	}
}

func parseUAPIUint(value string, bits int) (string, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 0, bits)
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(n, 10), nil
}

func parsePersistentKeepalive(value string) (string, error) {
	if strings.EqualFold(strings.TrimSpace(value), "off") {
		return "0", nil
	}
	return parseUAPIUint(value, 16)
}

func expandAllowedIPs(csv string) ([]string, error) {
	addrs, err := filterIPv4AllowedIPs(csv)
	if err != nil {
		return nil, err
	}
	lines := make([]string, len(addrs))
	for i, a := range addrs {
		lines[i] = "allowed_ip=" + a
	}
	return lines, nil
}

// filterIPv4AllowedIPs parses a comma-separated AllowedIPs value, drops any
// IPv6 entries (with a warning, since dual-stack peers are the norm), and
// errors only if that leaves an entry-bearing value with no IPv4 addresses.
func filterIPv4AllowedIPs(csv string) ([]string, error) {
	parts := strings.Split(csv, ",")
	var v4 []string
	sawEntry := false
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		sawEntry = true
		prefix, err := parseAllowedIPEntry(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid AllowedIPs entry %q: %w", trimmed, err)
		}
		if !prefix.Addr().Is4() {
			fmt.Fprintf(os.Stderr, "wg: skipping unsupported IPv6 AllowedIPs entry %q\n", trimmed)
			continue
		}
		v4 = append(v4, prefix.Masked().String())
	}
	if sawEntry && len(v4) == 0 {
		return nil, fmt.Errorf("IPv6 AllowedIPs entries are not supported without an IPv4 entry: %s", csv)
	}
	return v4, nil
}

// parseAllowedIPEntry accepts both CIDR notation and a bare address, which
// wg-quick treats as a /32 (IPv4) or /128 (IPv6) host route.
func parseAllowedIPEntry(entry string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		return prefix, nil
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func base64ToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", fmt.Errorf("decode base64 key: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// extractAllowedIPsFromConfig parses a stripped wg config and returns
// all AllowedIPs values across all peer sections.
func extractAllowedIPsFromConfig(wgConfig string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(wgConfig))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	section := ""
	var allowed []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}

		if section != "peer" {
			continue
		}

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		for i, r := range value {
			if r == '#' || r == ';' {
				value = strings.TrimSpace(value[:i])
				break
			}
		}

		if strings.EqualFold(key, "AllowedIPs") {
			v4, err := filterIPv4AllowedIPs(value)
			if err != nil {
				return nil, err
			}
			allowed = append(allowed, v4...)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse wg config for allowed-ips: %w", err)
	}

	return allowed, nil
}
