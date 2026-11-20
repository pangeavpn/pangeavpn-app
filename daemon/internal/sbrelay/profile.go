package sbrelay

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func NewTUIC(logs *state.LogStore) *Manager[state.TUICProfile] {
	return &Manager[state.TUICProfile]{name: "tuic", source: state.SourceTUIC, logs: logs, build: buildTUIC}
}

// NewCDN is VLESS over HTTPUpgrade through the CDN edge.
func NewCDN(logs *state.LogStore) *Manager[state.CDNProfile] {
	return &Manager[state.CDNProfile]{name: "cdn", source: state.SourceCDN, logs: logs, build: cdnBuilder("cdn", cdnVLESS)}
}

// NewCDNWS is VMess over a WebSocket through the same kind of edge: past the upgrade a
// filter that decrypts TLS reads only VMess ciphertext.
func NewCDNWS(logs *state.LogStore) *Manager[state.CDNProfile] {
	return &Manager[state.CDNProfile]{name: "cdnws", source: state.SourceCDNWS, logs: logs, build: cdnBuilder("cdnws", cdnVMess)}
}

type cdnProtocol int

const (
	cdnVLESS cdnProtocol = iota
	cdnVMess
)

// cdnBuilder dials over ECH when the profile carries a config, and plainly if that fails:
// a network may block the CDN's ECH name while still passing this hostname.
func cdnBuilder(name string, protocol cdnProtocol) func(state.CDNProfile) (spec, error) {
	return func(p state.CDNProfile) (spec, error) {
		plain, err := buildCDN(name, protocol, p, nil)
		if err != nil || p.ECHConfig == "" {
			return plain, err
		}
		ech, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p.ECHConfig))
		if err != nil || len(ech) < 2 || int(ech[0])<<8|int(ech[1]) != len(ech)-2 {
			return spec{}, fmt.Errorf("%s echConfig must be a base64 ECHConfigList", name)
		}
		sp, err := buildCDN(name, protocol, p, ech)
		if err != nil {
			return spec{}, err
		}
		sp.fallback = &plain
		return sp, nil
	}
}

func buildTUIC(p state.TUICProfile) (spec, error) {
	host := strings.TrimSpace(p.RemoteHost)
	if err := validatePorts("tuic", host, p.RemotePort, p.LocalPort, p.TargetPort); err != nil {
		return spec{}, err
	}
	if p.UUID == "" || p.Password == "" {
		return spec{}, errors.New("tuic uuid and password are required")
	}
	if p.Insecure && p.PinSHA256 == "" {
		return spec{}, errors.New("tuic insecure requires pinSha256 to be set")
	}
	serverName := strings.TrimSpace(p.ServerName)
	if serverName == "" {
		serverName = host
	}
	// The node's listener advertises h3, so the handshake reads as HTTP/3.
	tls := &option.OutboundTLSOptions{Enabled: true, ServerName: serverName, Insecure: p.Insecure, ALPN: badoption.Listable[string]{"h3"}}
	if p.PinSHA256 != "" {
		pin, err := decodePin(p.PinSHA256)
		if err != nil {
			return spec{}, err
		}
		tls.CertificatePublicKeySHA256 = badoption.Listable[[]byte]{pin}
	}
	return spec{
		outbound: option.Outbound{Type: C.TypeTUIC, Tag: outboundTag, Options: &option.TUICOutboundOptions{
			DialerOptions:               protectedDialer(),
			ServerOptions:               option.ServerOptions{Server: host, ServerPort: uint16(p.RemotePort)},
			UUID:                        p.UUID,
			Password:                    p.Password,
			CongestionControl:           "bbr",
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
		}},
		remote:     net.JoinHostPort(host, strconv.Itoa(p.RemotePort)),
		targetHost: targetHost(p.TargetHost),
		targetPort: targetPort(p.TargetPort),
		localPort:  p.LocalPort,
	}, nil
}

func buildCDN(name string, protocol cdnProtocol, p state.CDNProfile, ech []byte) (spec, error) {
	host := strings.TrimSpace(p.RemoteHost)
	remotePort := p.RemotePort
	if remotePort == 0 {
		remotePort = 443
	}
	if err := validatePorts(name, host, remotePort, p.LocalPort, p.TargetPort); err != nil {
		return spec{}, err
	}
	if p.UUID == "" {
		return spec{}, fmt.Errorf("%s uuid is required", name)
	}
	// The edge routes on SNI and Host, so the proxied hostname is never optional.
	serverName := strings.TrimSpace(p.ServerName)
	if serverName == "" || net.ParseIP(serverName) != nil {
		return spec{}, fmt.Errorf("%s serverName must be the CDN-proxied hostname", name)
	}
	path := p.Path
	if path == "" {
		path = "/"
	}
	tls := &option.OutboundTLSOptions{Enabled: true, ServerName: serverName}
	if ech != nil {
		// Go's own TLS: sing-box redials with the edge's retry configs only for crypto/tls.
		config := pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: ech})
		tls.ECH = &option.OutboundECHOptions{Enabled: true, Config: badoption.Listable[string]{string(config)}}
	} else if utlsAvailable {
		tls.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "chrome"}
	}
	server := option.ServerOptions{Server: host, ServerPort: uint16(remotePort)}
	tlsOptions := option.OutboundTLSOptionsContainer{TLS: tls}

	var outbound option.Outbound
	var dialer *option.DialerOptions
	var dialAt *option.ServerOptions
	via := "VLESS over HTTPUpgrade"
	switch protocol {
	case cdnVLESS:
		vless := &option.VLESSOutboundOptions{
			DialerOptions:               protectedDialer(),
			ServerOptions:               server,
			UUID:                        p.UUID,
			OutboundTLSOptionsContainer: tlsOptions,
			Transport: &option.V2RayTransportOptions{
				Type:               C.V2RayTransportTypeHTTPUpgrade,
				HTTPUpgradeOptions: option.V2RayHTTPUpgradeOptions{Host: serverName, Path: path},
			},
		}
		outbound, dialer, dialAt = option.Outbound{Type: C.TypeVLESS, Tag: outboundTag, Options: vless}, &vless.DialerOptions, &vless.ServerOptions
	case cdnVMess:
		// Everything after the upgrade is VMess ciphertext, so a filter that decrypts TLS reads nothing.
		vmess := &option.VMessOutboundOptions{
			DialerOptions:               protectedDialer(),
			ServerOptions:               server,
			UUID:                        p.UUID,
			Security:                    "auto",
			GlobalPadding:               true,
			AuthenticatedLength:         true,
			OutboundTLSOptionsContainer: tlsOptions,
			Transport: &option.V2RayTransportOptions{
				Type:             C.V2RayTransportTypeWebsocket,
				WebsocketOptions: option.V2RayWebsocketOptions{Path: path, Headers: browserHeaders(serverName)},
			},
		}
		outbound, dialer, dialAt = option.Outbound{Type: C.TypeVMess, Tag: outboundTag, Options: vmess}, &vmess.DialerOptions, &vmess.ServerOptions
		via = "VMess over WebSocket"
	default:
		return spec{}, fmt.Errorf("%s: unknown protocol %d", name, protocol)
	}

	if ech != nil {
		via += " with ECH"
	}
	extraRoots := parseRoots(p.ExtraRootsPEM)
	remote := net.JoinHostPort(host, strconv.Itoa(remotePort)) + " (" + serverName + "), " + via
	if len(extraRoots) > 0 {
		remote += fmt.Sprintf(", trusting %d of the user's own roots", len(extraRoots))
	}
	probe := &option.DirectOutboundOptions{DialerOptions: protectedDialer()}
	refusal := &refusalProbe{tag: probeTag, at: dialAt, serverName: serverName, path: path, roots: extraRoots, ech: ech, websocket: protocol == cdnVMess}
	return spec{
		outbound:   outbound,
		extra:      []option.Outbound{{Type: C.TypeDirect, Tag: probeTag, Options: probe}},
		remote:     remote,
		targetHost: targetHost(p.TargetHost),
		targetPort: targetPort(p.TargetPort),
		localPort:  p.LocalPort,
		annotate:   refusal.annotate,
		extraRoots: extraRoots,
		pinHost:    host,
		applyPin: func(pin Pin) []option.Outbound {
			dialAt.Server = pin.Server
			*dialer = pinnedDialer(pin)
			if pin.Dial == nil {
				probe.DialerOptions = *dialer
				return nil
			}
			// A direct outbound can't take a detour, so the probe dials the pin's own outbound.
			refusal.tag = pinnedTag
			return []option.Outbound{{Type: pinnedType, Tag: pinnedTag, Options: &pinnedOptions{Dial: pin.Dial}}}
		},
	}, nil
}

// browserUserAgent matches the Chrome release that sing-box's uTLS "chrome" fingerprint copies.
const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

// browserHeaders are what Chrome adds to a WebSocket upgrade; the library writes the rest.
func browserHeaders(serverName string) badoption.HTTPHeader {
	return badoption.HTTPHeader{
		"Host":            {serverName},
		"User-Agent":      {browserUserAgent},
		"Origin":          {"https://" + serverName},
		"Pragma":          {"no-cache"},
		"Cache-Control":   {"no-cache"},
		"Accept-Encoding": {"gzip, deflate, br, zstd"},
		"Accept-Language": {"en-US,en;q=0.9"},
	}
}

// pinnedDialer binds the edge connection to the physical NIC: the edge is a shared CDN
// address, so it never gets a route around the tunnel that every app would follow.
func pinnedDialer(pin Pin) option.DialerOptions {
	if pin.Dial != nil {
		return option.DialerOptions{Detour: pinnedTag}
	}
	return option.DialerOptions{AbstractDialerOptions: option.AbstractDialerOptions{
		BindInterface: pin.Interface,
		RoutingMark:   option.FwMark(pin.Mark),
	}}
}

// parseRoots keeps the certificates Go can parse, re-encoded one per entry: one
// unparseable entry in a user's store must not stop sing-box loading the rest.
func parseRoots(bundle string) []string {
	var roots []string
	rest := []byte(bundle)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return roots
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			continue
		}
		roots = append(roots, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})))
	}
}

func validatePorts(name, host string, remotePort, localPort, targetPort int) error {
	if host == "" {
		return fmt.Errorf("%s remoteHost is required", name)
	}
	if remotePort <= 0 || remotePort > 65535 {
		return fmt.Errorf("%s remotePort must be > 0 and <= 65535", name)
	}
	if localPort < 0 || localPort > 65535 || targetPort < 0 || targetPort > 65535 {
		return fmt.Errorf("%s localPort and targetPort must be within 0-65535", name)
	}
	return nil
}

func decodePin(pin string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pin))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("pinSha256 must be a base64 32-byte SPKI digest")
	}
	return raw, nil
}

func protectedDialer() option.DialerOptions {
	return option.DialerOptions{AbstractDialerOptions: option.AbstractDialerOptions{ProtectPath: ProtectPath}}
}

func targetHost(host string) string {
	if host = strings.TrimSpace(host); host != "" {
		return host
	}
	return "127.0.0.1"
}

func targetPort(port int) int {
	if port > 0 {
		return port
	}
	return state.DefaultWireGuardPort
}
