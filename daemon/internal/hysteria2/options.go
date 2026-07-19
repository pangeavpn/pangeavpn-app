package hysteria2

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

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
	if profile.RemotePort <= 0 {
		return errors.New("hysteria2 remotePort must be > 0")
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
	return nil
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
				Type: C.TypeHysteria2,
				Tag:  hysteria2OutboundTag,
				Options: &option.Hysteria2OutboundOptions{
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
				},
			},
		},
	}, nil
}

func loopbackAddr() *badoption.Addr {
	addr := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	return &addr
}
