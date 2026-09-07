package hysteria2

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// hysteria2HopInterval is how often the client rotates to another port in
// RemotePorts. Matches Hysteria's own default.
const hysteria2HopInterval = 30 * time.Second

const (
	mixedInboundTag      = "hysteria2-mixed-in"
	hysteria2OutboundTag = "hysteria2-out"
	obfsTypeSalamander   = "salamander"
)

// relayDestination is the fixed SOCKS5 UDP ASSOCIATE destination the bridge
// requests through the tunnel. It is intentionally not part of
// state.Hysteria2Profile: same convention as Cloak (ProxyMethod=wireguard)
// and NaiveProxy — the real WireGuard endpoint is a server-side deployment
// detail, not client config. A correctly deployed Hysteria2 server either
// runs WireGuard co-located on this loopback port (the common case, in
// which this address is simply correct) or overrides the destination via
// its own route config, ignoring whatever the client requests. Same-package
// tests reassign this to point at a fixed test destination.
var relayDestination = "127.0.0.1:51820"

func validateProfile(profile state.Hysteria2Profile) error {
	if strings.TrimSpace(profile.RemoteHost) == "" {
		return errors.New("hysteria2 remoteHost is required")
	}
	if profile.RemotePort <= 0 || profile.RemotePort > 65535 {
		return errors.New("hysteria2 remotePort must be between 1 and 65535")
	}
	if profile.Password == "" {
		return errors.New("hysteria2 password is required")
	}
	if profile.ObfsPassword == "" {
		return errors.New("hysteria2 obfsPassword is required")
	}
	if profile.LocalPort < 0 {
		return errors.New("hysteria2 localPort must be >= 0")
	}
	if profile.UpMbps < 0 {
		return errors.New("hysteria2 upMbps must be >= 0")
	}
	if profile.DownMbps < 0 {
		return errors.New("hysteria2 downMbps must be >= 0")
	}
	// Insecure without a pin lets an on-path attacker terminate TLS unnoticed.
	if profile.Insecure && profile.PinSHA256 == "" {
		return errors.New("hysteria2 insecure requires pinSha256 to be set")
	}
	for _, r := range profile.RemotePorts {
		if err := validatePortRange(r); err != nil {
			return fmt.Errorf("hysteria2 remotePorts: %w", err)
		}
	}
	return nil
}

// validatePortRange checks a single "start:end" entry, the format sing-quic's
// ParsePorts accepts. Both bounds must be valid ports with start <= end.
func validatePortRange(r string) error {
	start, end, ok := strings.Cut(r, ":")
	if !ok {
		return fmt.Errorf("bad port range %q (want start:end)", r)
	}
	lo, err := strconv.Atoi(start)
	if err != nil || lo < 1 || lo > 65535 {
		return fmt.Errorf("bad port range %q", r)
	}
	hi, err := strconv.Atoi(end)
	if err != nil || hi < lo || hi > 65535 {
		return fmt.Errorf("bad port range %q", r)
	}
	return nil
}

// profilesEqual reports whether two Hysteria2 profiles are identical. A plain
// == cannot be used since the struct now carries the RemotePorts slice.
func profilesEqual(a, b state.Hysteria2Profile) bool {
	return reflect.DeepEqual(a, b)
}

// buildClientOptions constructs the client-side box config: a loopback
// mixed inbound (bound to mixedPort, an internal implementation detail
// never exposed to WireGuard) fronting a hysteria2 outbound with Salamander
// obfuscation.
func buildClientOptions(profile state.Hysteria2Profile, mixedPort int) (option.Options, error) {
	tlsOptions := &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: profile.ServerName,
		Insecure:   profile.Insecure,
	}
	if profile.PinSHA256 != "" {
		pin, err := base64.StdEncoding.DecodeString(profile.PinSHA256)
		if err != nil {
			return option.Options{}, fmt.Errorf("hysteria2: decode pinSha256: %w", err)
		}
		tlsOptions.CertificatePublicKeySHA256 = badoption.Listable[[]byte]{pin}
	}

	hy2 := &option.Hysteria2OutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     profile.RemoteHost,
			ServerPort: uint16(profile.RemotePort),
		},
		UpMbps:   profile.UpMbps,
		DownMbps: profile.DownMbps,
		Obfs: &option.Hysteria2Obfs{
			Type:     obfsTypeSalamander,
			Password: profile.ObfsPassword,
		},
		Password:                    profile.Password,
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
	}
	if len(profile.RemotePorts) > 0 {
		hy2.ServerPorts = badoption.Listable[string](profile.RemotePorts)
		hy2.HopInterval = badoption.Duration(hysteria2HopInterval)
	}

	return option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  mixedInboundTag,
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     loopbackAddr(),
						ListenPort: uint16(mixedPort),
					},
				},
			},
		},
		Outbounds: []option.Outbound{
			{
				Type:    C.TypeHysteria2,
				Tag:     hysteria2OutboundTag,
				Options: hy2,
			},
		},
	}, nil
}

func loopbackAddr() *badoption.Addr {
	addr := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	return &addr
}
