package mobile

// Ports the profile construction in apps/desktop/src/main/pangeaApiClient.ts.

import (
	"net"
	"strings"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// buildProfile maps one hub server entry onto the daemon profile the cascade
// consumes. Optional blocks stay nil when the hub omits them.
func buildProfile(server *serverInfo) *state.Profile {
	nodeIP := server.Cloak.RemoteHost
	return &state.Profile{
		ID:   "auto-" + server.ID,
		Name: server.Name,
		Cloak: state.CloakProfile{
			LocalPort:        0,
			RemoteHost:       nodeIP,
			RemotePort:       443,
			UID:              server.Cloak.UID,
			PublicKey:        server.Cloak.PublicKey,
			EncryptionMethod: "plain",
			ServerName:       server.Cloak.ServerName,
		},
		Naive:       buildNaive(server.Naive, nodeIP),
		Reality:     buildReality(server.Reality, nodeIP),
		Hysteria2:   buildHysteria2(server.Hysteria2, nodeIP),
		Shadowsocks: buildShadowsocks(server.Shadowsocks, nodeIP),
		Snowflake:   buildSnowflake(server.Snowflake),
	}
}

// endpointHost prefers the per-transport IP the hub named. Dialing a domain
// would leak the node to a third-party resolver, so we never resolve one.
func endpointHost(remoteIP, nodeIP string) string {
	if trimmed := strings.TrimSpace(remoteIP); trimmed != "" {
		return trimmed
	}
	return nodeIP
}

// buildNaive ports apps/desktop/src/shared/naiveEndpoint.ts. The engine maps
// serverName onto remoteHost itself, so splitting them keeps DNS out of it.
func buildNaive(info *naiveInfo, nodeIP string) *state.NaiveProfile {
	if info == nil {
		return nil
	}
	host := strings.TrimSpace(info.RemoteHost)
	serverName := host
	if named := strings.TrimSpace(info.ServerName); named != "" {
		serverName = named
	}
	return &state.NaiveProfile{
		LocalPort:  0,
		RemoteHost: naiveDialHost(info, host, nodeIP),
		RemotePort: info.RemotePort,
		Username:   info.Username,
		Password:   info.Password,
		ServerName: serverName,
	}
}

// naiveDialHost picks the address to dial: a naive-specific one first, then
// remoteHost when it is already a literal, then the shared node.
func naiveDialHost(info *naiveInfo, host, nodeIP string) string {
	if perTransport := strings.TrimSpace(info.RemoteIP); perTransport != "" {
		return perTransport
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		return host
	}
	if trimmed := strings.TrimSpace(nodeIP); trimmed != "" {
		return trimmed
	}
	return host
}

func buildReality(info *realityInfo, nodeIP string) *state.RealityProfile {
	if info == nil {
		return nil
	}
	return &state.RealityProfile{
		LocalPort:  0,
		RemoteHost: endpointHost(info.RemoteIP, nodeIP),
		RemotePort: info.RemotePort,
		UUID:       info.UUID,
		PublicKey:  info.PublicKey,
		ShortID:    info.ShortID,
		Flow:       info.Flow,
		ServerName: info.ServerName,
	}
}

func buildHysteria2(info *hysteria2Info, nodeIP string) *state.Hysteria2Profile {
	if info == nil {
		return nil
	}
	// The hub's own domain is the SNI it would have been used for anyway, so
	// it stands in when no explicit serverName is named.
	serverName := info.ServerName
	if serverName == "" {
		serverName = info.RemoteHost
	}
	return &state.Hysteria2Profile{
		LocalPort:    0,
		RemoteHost:   endpointHost(info.RemoteIP, nodeIP),
		RemotePort:   info.RemotePort,
		Password:     info.Password,
		ObfsPassword: info.ObfsPassword,
		ServerName:   serverName,
		PinSHA256:    info.PinSHA256,
	}
}

func buildShadowsocks(info *shadowsocksInfo, nodeIP string) *state.ShadowsocksProfile {
	if info == nil {
		return nil
	}
	// TargetHost/TargetPort stay zero unless the hub named them, so the
	// transport applies its own 127.0.0.1:51820 default.
	return &state.ShadowsocksProfile{
		LocalPort:  0,
		RemoteHost: endpointHost(info.RemoteIP, nodeIP),
		RemotePort: info.RemotePort,
		Method:     info.Method,
		Password:   info.Password,
		TargetHost: info.TargetHost,
		TargetPort: info.TargetPort,
		UDPOverTCP: info.UDPOverTCP,
	}
}

func buildSnowflake(info *snowflakeInfo) *state.SnowflakeProfile {
	if info == nil {
		return nil
	}
	return &state.SnowflakeProfile{
		LocalPort:         0,
		BrokerURL:         info.BrokerURL,
		BridgeFingerprint: info.BridgeFingerprint,
		FrontDomains:      info.FrontDomains,
		AmpCacheURL:       info.AmpCacheURL,
		ICEServers:        info.ICEServers,
	}
}

