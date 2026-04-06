//go:build darwin || linux || windows

package wg

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
)

// wgConfigToUAPI converts a stripped wg-quick INI config (containing only
// WireGuard-native keys: PrivateKey, ListenPort, PublicKey, PresharedKey,
// Endpoint, AllowedIPs, PersistentKeepalive) into the UAPI format accepted
// by device.IpcSet.
//
// Keys are converted from base64 to hex. AllowedIPs CSV values are split into
// separate allowed_ip= lines. The first [Peer] block emits replace_peers=true,
// and each peer emits replace_allowed_ips=true before its allowed IPs.
func wgConfigToUAPI(wgConfig string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(wgConfig))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	var out []string
	section := ""
	firstPeer := true

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "peer" {
				if firstPeer {
					out = append(out, "replace_peers=true")
					firstPeer = false
				}
				// Each peer block boundary is signalled by public_key=,
				// so we don't emit a separator line here.
			}
			continue
		}

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		// Strip inline comments.
		for i, r := range value {
			if r == '#' || r == ';' {
				value = strings.TrimSpace(value[:i])
				break
			}
		}

		uapiLine, err := convertKV(section, key, value)
		if err != nil {
			return "", fmt.Errorf("uapi convert %s.%s: %w", section, key, err)
		}
		if uapiLine != "" {
			out = append(out, uapiLine)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("parse wg config for uapi: %w", err)
	}

	return strings.Join(out, "\n") + "\n", nil
}

func convertKV(section, key, value string) (string, error) {
	lower := strings.ToLower(key)

	switch section {
	case "interface":
		switch lower {
		case "privatekey":
			h, err := base64ToHex(value)
			if err != nil {
				return "", err
			}
			return "private_key=" + h, nil
		case "listenport":
			return "listen_port=" + value, nil
		case "fwmark":
			return "fwmark=" + value, nil
		}
	case "peer":
		switch lower {
		case "publickey":
			h, err := base64ToHex(value)
			if err != nil {
				return "", err
			}
			return "public_key=" + h + "\nreplace_allowed_ips=true", nil
		case "presharedkey":
			h, err := base64ToHex(value)
			if err != nil {
				return "", err
			}
			return "preshared_key=" + h, nil
		case "endpoint":
			return "endpoint=" + value, nil
		case "allowedips":
			return expandAllowedIPs(value)
		case "persistentkeepalive":
			return "persistent_keepalive_interval=" + value, nil
		}
	}

	return "", nil
}

func expandAllowedIPs(csv string) (string, error) {
	parts := strings.Split(csv, ",")
	var lines []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			prefix, err := netip.ParsePrefix(trimmed)
			if err != nil {
				return "", fmt.Errorf("invalid AllowedIPs entry %q: %w", trimmed, err)
			}
			if !prefix.Addr().Is4() {
				return "", fmt.Errorf("IPv6 AllowedIPs entry is not supported: %s", trimmed)
			}
			lines = append(lines, "allowed_ip="+prefix.Masked().String())
		}
	}
	return strings.Join(lines, "\n"), nil
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
			for _, part := range strings.Split(value, ",") {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					prefix, parseErr := netip.ParsePrefix(trimmed)
					if parseErr != nil {
						return nil, fmt.Errorf("invalid AllowedIPs entry %q: %w", trimmed, parseErr)
					}
					if !prefix.Addr().Is4() {
						return nil, fmt.Errorf("IPv6 AllowedIPs entry is not supported: %s", trimmed)
					}
					allowed = append(allowed, prefix.Masked().String())
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse wg config for allowed-ips: %w", err)
	}

	return allowed, nil
}
