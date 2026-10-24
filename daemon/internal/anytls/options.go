package anytls

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const outboundTag = "anytls-out"

// Where the AnyTLS server relays decoded packets: the node's own WireGuard
// listener. Overridable because the node's outbound ACL decides what it may reach.
const (
	defaultTargetHost = "127.0.0.1"
	defaultTargetPort = state.DefaultWireGuardPort
)

// ClientMetadata is the cmdSettings `client=` value, AnyTLS's User-Agent, seen
// only by the node inside TLS. Mobile overrides it.
var ClientMetadata = "pangeavpn-desktop"

// utlsFingerprint is the uTLS ClientHello presented when the build carries
// uTLS, so the handshake looks like a browser's rather than Go's.
const utlsFingerprint = "chrome"

func validateProfile(profile state.AnyTLSProfile) error {
	if strings.TrimSpace(profile.RemoteHost) == "" {
		return errors.New("anytls remoteHost is required")
	}
	if profile.RemotePort <= 0 || profile.RemotePort > 65535 {
		return errors.New("anytls remotePort must be > 0 and <= 65535")
	}
	if profile.Password == "" {
		return errors.New("anytls password is required")
	}
	if profile.LocalPort < 0 || profile.LocalPort > 65535 {
		return errors.New("anytls localPort must be >= 0 and <= 65535")
	}
	if profile.TargetPort < 0 || profile.TargetPort > 65535 {
		return errors.New("anytls targetPort must be >= 0 and <= 65535")
	}
	// Insecure without a pin lets an on-path attacker terminate TLS unnoticed.
	if profile.Insecure && profile.PinSHA256 == "" {
		return errors.New("anytls insecure requires pinSha256 to be set")
	}
	if profile.PinSHA256 != "" {
		if _, err := decodePin(profile.PinSHA256); err != nil {
			return err
		}
	}
	return nil
}

// decodePin rejects anything but a base64 32-byte SPKI digest, so a typo fails
// here rather than as an unexplained handshake failure.
func decodePin(pin string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pin))
	if err != nil {
		return nil, fmt.Errorf("anytls pinSha256 is not standard base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("anytls pinSha256 decodes to %d bytes, need exactly 32", len(raw))
	}
	return raw, nil
}

// ProtectPath, when set, is the unix socket sing-box's dialer hands each new
// outbound fd to. Mobile points it at VpnService.protect(); desktop leaves it "".
var ProtectPath string

// buildOutboundOptions returns the pointer type protocol/anytls registers for
// C.TypeAnyTLS. TLS is mandatory for the outbound, so it is always enabled.
func buildOutboundOptions(profile state.AnyTLSProfile) (*option.AnyTLSOutboundOptions, error) {
	tlsOptions := &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: serverNameOrDefault(profile),
		Insecure:   profile.Insecure,
	}
	if profile.PinSHA256 != "" {
		pin, err := decodePin(profile.PinSHA256)
		if err != nil {
			return nil, err
		}
		tlsOptions.CertificatePublicKeySHA256 = badoption.Listable[[]byte]{pin}
	}
	if utlsAvailable {
		tlsOptions.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: utlsFingerprint}
	}
	return &option.AnyTLSOutboundOptions{
		DialerOptions: option.DialerOptions{AbstractDialerOptions: option.AbstractDialerOptions{ProtectPath: ProtectPath}},
		ServerOptions: option.ServerOptions{
			Server:     strings.TrimSpace(profile.RemoteHost),
			ServerPort: uint16(profile.RemotePort),
		},
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
		Password:                    profile.Password,
		ClientMetadata:              ClientMetadata,
		// The session-pool knobs stay at sing-anytls's defaults: WireGuard is
		// one long-lived stream, so idle sessions only exist between restarts.
	}, nil
}

// serverNameOrDefault is the SNI and certificate name: the profile's own when
// the hub named one, otherwise the host being dialed.
func serverNameOrDefault(profile state.AnyTLSProfile) string {
	if name := strings.TrimSpace(profile.ServerName); name != "" {
		return name
	}
	return strings.TrimSpace(profile.RemoteHost)
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
