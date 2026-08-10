package mobile

// Ports the profile construction in apps/desktop/src/main/pangeaApiClient.ts.
// NaiveProxy is absent: its engine is cgo and does not link on Android.

import (
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

