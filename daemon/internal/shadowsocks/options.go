package shadowsocks

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sagernet/sing-box/option"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const outboundTag = "shadowsocks-out"

// defaultMethod matches the nodes' public listeners. SS-2022 resists the active
// probing that broke AEAD-2017; its keys are base64 PSKs of an exact length.
const defaultMethod = "2022-blake3-aes-128-gcm"

// Where the SS server relays decoded packets: the node's own WireGuard
// listener. Overridable because the node's outbound ACL decides what it may reach.
const (
	defaultTargetHost = "127.0.0.1"
	defaultTargetPort = 51820
)

// supportedMethods covers sing-shadowsocks2's shadowaead and shadowaead_2022
// families; the legacy shadowstream ciphers are unauthenticated, so they are out.
var supportedMethods = []string{
	"aes-128-gcm",
	"aes-192-gcm",
	"aes-256-gcm",
	"chacha20-ietf-poly1305",
	"xchacha20-ietf-poly1305",
	"2022-blake3-aes-128-gcm",
	"2022-blake3-aes-256-gcm",
	"2022-blake3-chacha20-poly1305",
}

// keySaltLengths mirrors shadowaead_2022: those methods take a base64 PSK of
// exactly this many bytes, not a passphrase. AEAD-2017 methods are absent.
var keySaltLengths = map[string]int{
	"2022-blake3-aes-128-gcm":       16,
	"2022-blake3-aes-256-gcm":       32,
	"2022-blake3-chacha20-poly1305": 32,
}

// validateKeyMaterial rejects a malformed SS-2022 PSK here, where the message
// names the fix, rather than at dial time as an opaque outbound-create failure.
func validateKeyMaterial(method, password string) error {
	keyLen, ok := keySaltLengths[method]
	if !ok {
		return nil
	}
	keys := strings.Split(password, ":")
	// An iPSK chain needs EIH, which the chacha20 variant does not implement.
	if len(keys) > 1 && method == "2022-blake3-chacha20-poly1305" {
		return fmt.Errorf("shadowsocks method %s takes a single key, got %d", method, len(keys))
	}
	for i, key := range keys {
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			return fmt.Errorf("shadowsocks %s key %d is not standard base64: %w", method, i+1, err)
		}
		if len(raw) != keyLen {
			return fmt.Errorf("shadowsocks %s key %d decodes to %d bytes, need exactly %d (openssl rand -base64 %d)", method, i+1, len(raw), keyLen, keyLen)
		}
	}
	return nil
}

func validateProfile(profile state.ShadowsocksProfile) error {
	if strings.TrimSpace(profile.RemoteHost) == "" {
		return errors.New("shadowsocks remoteHost is required")
	}
	if profile.RemotePort <= 0 || profile.RemotePort > 65535 {
		return errors.New("shadowsocks remotePort must be > 0 and <= 65535")
	}
	if profile.Password == "" {
		return errors.New("shadowsocks password is required")
	}
	if profile.LocalPort < 0 || profile.LocalPort > 65535 {
		return errors.New("shadowsocks localPort must be >= 0 and <= 65535")
	}
	if profile.TargetPort < 0 || profile.TargetPort > 65535 {
		return errors.New("shadowsocks targetPort must be >= 0 and <= 65535")
	}
	if method := strings.TrimSpace(profile.Method); method != "" && !slices.Contains(supportedMethods, method) {
		return fmt.Errorf("shadowsocks method %q is not supported (allowed: %s)", method, strings.Join(supportedMethods, ", "))
	}
	return validateKeyMaterial(methodOrDefault(profile.Method), profile.Password)
}

// buildOutboundOptions returns the pointer type protocol/shadowsocks
// registers for C.TypeShadowsocks.
func buildOutboundOptions(profile state.ShadowsocksProfile) *option.ShadowsocksOutboundOptions {
	opts := &option.ShadowsocksOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     strings.TrimSpace(profile.RemoteHost),
			ServerPort: uint16(profile.RemotePort),
		},
		Method:   methodOrDefault(profile.Method),
		Password: profile.Password,
	}
	// Multiplex stays nil: WireGuard is one long-lived flow, so smux only adds
	// framing overhead and a second fingerprintable layer.
	if profile.UDPOverTCP {
		opts.UDPOverTCP = &option.UDPOverTCPOptions{Enabled: true}
	}
	return opts
}

func methodOrDefault(method string) string {
	if method = strings.TrimSpace(method); method != "" {
		return method
	}
	return defaultMethod
}

func targetHostOrDefault(host string) string {
	if host = strings.TrimSpace(host); host != "" {
		return host
	}
	return defaultTargetHost
}

func targetPortOrDefault(port int) int {
	if port > 0 {
		return port
	}
	return defaultTargetPort
}
