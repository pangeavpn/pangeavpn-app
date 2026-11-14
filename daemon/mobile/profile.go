package mobile

// Ports the profile construction in apps/desktop/src/main/pangeaApiClient.ts.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"strconv"
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
		AnyTLS:      buildAnyTLS(server.AnyTLS, nodeIP),
		TUIC:        buildTUIC(server.TUIC, nodeIP),
		CDN:         buildCDN(server.CDN),
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
	return &state.NaiveProfile{
		LocalPort:  0,
		RemoteHost: naiveDialHost(info, host, nodeIP),
		RemotePort: info.RemotePort,
		Username:   info.Username,
		Password:   info.Password,
		// nodeIP is the last resort, so the SNI is never blank.
		ServerName: firstNonBlank(info.ServerName, host, nodeIP),
	}
}

// naiveDialHost picks the address to dial: a naive-specific address first,
// then remoteHost when it is already one, then the shared node.
func naiveDialHost(info *naiveInfo, host, nodeIP string) string {
	if perTransport := strings.TrimSpace(info.RemoteIP); perTransport != "" && isIPLiteral(perTransport) {
		return perTransport
	}
	if isIPLiteral(host) {
		return host
	}
	return firstNonBlank(nodeIP, host)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
		RemotePorts:  info.RemotePorts,
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

// buildAnyTLS ports apps/desktop/src/shared/anytlsProfile.ts: dial an address,
// never a domain, and never send an address as the SNI.
func buildAnyTLS(info *anytlsInfo, nodeIP string) *state.AnyTLSProfile {
	if info == nil {
		return nil
	}
	host := strings.TrimSpace(info.RemoteHost)
	dial := nodeIP
	if remoteIP := strings.TrimSpace(info.RemoteIP); isIPLiteral(remoteIP) {
		dial = remoteIP
	} else if isIPLiteral(host) {
		dial = host
	}
	serverName := strings.TrimSpace(info.ServerName)
	if serverName == "" && !isIPLiteral(host) {
		serverName = host
	}
	return &state.AnyTLSProfile{
		LocalPort:  0,
		RemoteHost: dial,
		RemotePort: info.RemotePort,
		Password:   info.Password,
		ServerName: serverName,
		Insecure:   info.Insecure,
		PinSHA256:  info.PinSHA256,
		TargetHost: info.TargetHost,
		TargetPort: info.TargetPort,
	}
}

// buildTUIC ports apps/desktop/src/shared/tuicProfile.ts, the same dial and SNI rules as AnyTLS.
func buildTUIC(info *tuicInfo, nodeIP string) *state.TUICProfile {
	if info == nil {
		return nil
	}
	host := strings.TrimSpace(info.RemoteHost)
	dial := nodeIP
	if remoteIP := strings.TrimSpace(info.RemoteIP); isIPLiteral(remoteIP) {
		dial = remoteIP
	} else if isIPLiteral(host) {
		dial = host
	}
	serverName := strings.TrimSpace(info.ServerName)
	if serverName == "" && !isIPLiteral(host) {
		serverName = host
	}
	return &state.TUICProfile{
		RemoteHost: dial,
		RemotePort: info.RemotePort,
		UUID:       info.UUID,
		Password:   info.Password,
		ServerName: serverName,
		PinSHA256:  info.PinSHA256,
		TargetHost: info.TargetHost,
		TargetPort: info.TargetPort,
	}
}

// buildCDN ports apps/desktop/src/shared/cdnProfile.ts. It never falls back to
// the node's address: hiding that address is the transport's whole point.
func buildCDN(info *cdnInfo) *state.CDNProfile {
	if info == nil {
		return nil
	}
	host := strings.TrimSpace(info.RemoteHost)
	dial := host
	if remoteIP := strings.TrimSpace(info.RemoteIP); isIPLiteral(remoteIP) {
		dial = remoteIP
	}
	serverName := strings.TrimSpace(info.ServerName)
	if serverName == "" && !isIPLiteral(host) {
		serverName = host
	}
	return &state.CDNProfile{
		RemoteHost: dial,
		RemotePort: info.RemotePort,
		UUID:       info.UUID,
		ServerName: serverName,
		Path:       info.Path,
		TargetHost: info.TargetHost,
		TargetPort: info.TargetPort,
	}
}

// tectonicDefaultPort is the node port when the hub's node names none.
const tectonicDefaultPort = 443

// decodeTectonic reads register's raw tectonic block. A malformed one costs only
// Tectonic, never the registration it rode in on.
func decodeTectonic(raw json.RawMessage) (*tectonicInfo, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	var info tectonicInfo
	if err := json.Unmarshal(trimmed, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// buildTectonic ports apps/desktop/src/shared/tectonicProfile.ts. It dials an IP,
// never a domain: a hostname node falls back to nodeIP, else Tectonic is dropped.
// tectonicExtraNodes are the node's other addresses, each with a port, IP
// literals only: a hostname would be resolved outside the tunnel. Lanes
// spread one per address, so these also have to be protected per socket,
// which they are, since every lane's socket is.
func tectonicExtraNodes(addrs []string, defaultPort int) []string {
	var out []string
	for _, a := range addrs {
		host, port := splitTectonicNode(strings.TrimSpace(a))
		if !isIPLiteral(host) {
			continue
		}
		if port == 0 {
			port = defaultPort
		}
		out = append(out, net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
	}
	return out
}

func buildTectonic(info *tectonicInfo, nodeIP string) *state.TectonicProfile {
	if info == nil {
		return nil
	}
	keyID, keyErr := base64.StdEncoding.DecodeString(strings.TrimSpace(info.KeyID))
	deviceKey, devErr := base64.StdEncoding.DecodeString(strings.TrimSpace(info.DeviceKey))
	if keyErr != nil || devErr != nil || len(keyID) == 0 || len(deviceKey) == 0 {
		return nil
	}
	host, port := splitTectonicNode(strings.TrimSpace(info.Node))
	if !isIPLiteral(host) {
		host = strings.TrimSpace(nodeIP)
	}
	if !isIPLiteral(host) {
		return nil
	}
	if port == 0 {
		port = tectonicDefaultPort
	}
	out := &state.TectonicProfile{
		Node:        net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)),
		Nodes:       tectonicExtraNodes(info.Nodes, port),
		CoverDomain: strings.TrimSpace(info.CoverDomain),
		KeyID:       keyID,
		DeviceKey:   deviceKey,
		Pins:        info.Pins,
		BorrowedSNI: info.BorrowedSNI,
	}
	if info.CDN != nil {
		if cdnHost := strings.TrimSpace(info.CDN.Host); cdnHost != "" {
			out.CDN = &state.TectonicCDNRoute{Host: cdnHost, Path: info.CDN.Path, Edge: strings.TrimSpace(info.CDN.Edge)}
		}
	}
	// Passed on as the hub sent it, like desktop; the daemon parses it at start.
	if shaping := bytes.TrimSpace(info.Shaping); len(shaping) > 0 && string(shaping) != "null" && string(shaping) != "[]" {
		out.Shaping = append(json.RawMessage(nil), shaping...)
	}
	return out
}

// splitTectonicNode splits "host", "host:port", "[v6]" or "[v6]:port"; a bare
// IPv6 literal has no port. An unusable port reads as none.
func splitTectonicNode(node string) (string, int) {
	if isIPv6Literal(node) {
		return strings.Trim(node, "[]"), 0
	}
	host, rawPort, err := net.SplitHostPort(node)
	if err != nil {
		return node, 0
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return host, 0
	}
	return host, port
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
