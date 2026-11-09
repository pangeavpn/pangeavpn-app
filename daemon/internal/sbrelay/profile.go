package sbrelay

import (
	"encoding/base64"
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

func NewCDN(logs *state.LogStore) *Manager[state.CDNProfile] {
	return &Manager[state.CDNProfile]{name: "cdn", source: state.SourceCDN, logs: logs, build: buildCDN}
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

func buildCDN(p state.CDNProfile) (spec, error) {
	host := strings.TrimSpace(p.RemoteHost)
	remotePort := p.RemotePort
	if remotePort == 0 {
		remotePort = 443
	}
	if err := validatePorts("cdn", host, remotePort, p.LocalPort, p.TargetPort); err != nil {
		return spec{}, err
	}
	if p.UUID == "" {
		return spec{}, errors.New("cdn uuid is required")
	}
	// The edge routes on SNI and Host, so the proxied hostname is never optional.
	serverName := strings.TrimSpace(p.ServerName)
	if serverName == "" || net.ParseIP(serverName) != nil {
		return spec{}, errors.New("cdn serverName must be the CDN-proxied hostname")
	}
	path := p.Path
	if path == "" {
		path = "/"
	}
	tls := &option.OutboundTLSOptions{Enabled: true, ServerName: serverName}
	if utlsAvailable {
		tls.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "chrome"}
	}
	return spec{
		outbound: option.Outbound{Type: C.TypeVLESS, Tag: outboundTag, Options: &option.VLESSOutboundOptions{
			DialerOptions:               protectedDialer(),
			ServerOptions:               option.ServerOptions{Server: host, ServerPort: uint16(remotePort)},
			UUID:                        p.UUID,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
			Transport: &option.V2RayTransportOptions{
				Type:               C.V2RayTransportTypeHTTPUpgrade,
				HTTPUpgradeOptions: option.V2RayHTTPUpgradeOptions{Host: serverName, Path: path},
			},
		}},
		remote:     net.JoinHostPort(host, strconv.Itoa(remotePort)) + " (" + serverName + ")",
		targetHost: targetHost(p.TargetHost),
		targetPort: targetPort(p.TargetPort),
		localPort:  p.LocalPort,
		annotate:   annotateCDN,
	}, nil
}

// annotateCDN names the usual cause of the bare EOF an edge returns when it
// can't reach the origin or the path/UUID is wrong.
func annotateCDN(err error) error {
	if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "unexpected status") {
		return fmt.Errorf("%w (the CDN edge answered but the node did not accept the upgrade; verify the path, UUID and the edge's origin settings)", err)
	}
	return err
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
