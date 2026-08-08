package shadowsocks

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sagernet/sing-box/option"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const outboundTag = "shadowsocks-out"

// defaultMethod matches the nodes' existing ssserver cipher. SS-2022 needs
// base64 key material of an exact length, so switching is a provisioning change.
const defaultMethod = "chacha20-ietf-poly1305"

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

func validateProfile(profile state.ShadowsocksProfile) error {
	if strings.TrimSpace(profile.RemoteHost) == "" {
		return errors.New("shadowsocks remoteHost is required")
	}
	if profile.RemotePort <= 0 {
		return errors.New("shadowsocks remotePort must be > 0")
	}
	if profile.Password == "" {
		return errors.New("shadowsocks password is required")
	}
	if profile.LocalPort < 0 {
		return errors.New("shadowsocks localPort must be >= 0")
	}
	if profile.TargetPort < 0 {
		return errors.New("shadowsocks targetPort must be >= 0")
	}
	if method := strings.TrimSpace(profile.Method); method != "" && !slices.Contains(supportedMethods, method) {
		return fmt.Errorf("shadowsocks method %q is not supported (allowed: %s)", method, strings.Join(supportedMethods, ", "))
	}
	return nil
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
