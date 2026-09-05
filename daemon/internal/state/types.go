package state

type DaemonState string

const (
	StateDisconnected  DaemonState = "DISCONNECTED"
	StateConnecting    DaemonState = "CONNECTING"
	StateConnected     DaemonState = "CONNECTED"
	StateDisconnecting DaemonState = "DISCONNECTING"
	StateError         DaemonState = "ERROR"
)

type LogLevel string

const (
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
	LogDebug LogLevel = "debug"
)

type LogSource string

const (
	SourceDaemon      LogSource = "daemon"
	SourceCloak       LogSource = "cloak"
	SourceNaive       LogSource = "naive"
	SourceShadowsocks LogSource = "shadowsocks"
	SourceSnowflake   LogSource = "snowflake"
	SourceWireGuard   LogSource = "wireguard"
)

type LogEntry struct {
	TS     int64     `json:"ts"`
	Level  LogLevel  `json:"level"`
	Source LogSource `json:"source"`
	Msg    string    `json:"msg"`
}

type CloakStatus struct {
	Running bool `json:"running"`
	PID     *int `json:"pid"`
}

// TransportStatus is a transport-agnostic status snapshot mirrored to the
// client over the status API, parallel to CloakStatus/WireGuardStatus.
type TransportStatus struct {
	Running bool `json:"running"`
	PID     *int `json:"pid"`
}

type WireGuardStatus struct {
	Running  bool   `json:"running"`
	Detail   string `json:"detail"`
	BytesIn  int64  `json:"bytesIn"`
	BytesOut int64  `json:"bytesOut"`
	// LastHandshakeUnix is the most recent successful WireGuard handshake with
	// any peer, in Unix seconds; 0 means no handshake has completed yet. The
	// interface can be Running with no handshake (device up, peer unreached),
	// which is why connection readiness gates on this, not on Running alone.
	LastHandshakeUnix int64 `json:"lastHandshakeUnix"`
}

type StatusResponse struct {
	State  DaemonState `json:"state"`
	Detail string      `json:"detail"`
	// ActiveTransport is "cloak", "naive", "reality", "hysteria2", "shadowsocks",
	// "snowflake", "wireguard" (no transport at all), or "" when disconnected.
	ActiveTransport string `json:"activeTransport"`
	// ConnectingTransport is the candidate the cascade is trying right now,
	// "" outside a bring-up. Lets clients show "via X" while connecting.
	ConnectingTransport string          `json:"connectingTransport"`
	Cloak            CloakStatus     `json:"cloak"`
	Naive            TransportStatus `json:"naive"`
	Reality          TransportStatus `json:"reality"`
	Hysteria2        TransportStatus `json:"hysteria2"`
	Shadowsocks      TransportStatus `json:"shadowsocks"`
	Snowflake        TransportStatus `json:"snowflake"`
	WireGuard        WireGuardStatus `json:"wireguard"`
	KillSwitchActive bool            `json:"killSwitchActive"`
	// Reconnecting marks an ERROR the daemon is still working on: the session
	// dropped on its own and rebuilds are being retried on a backoff. Clients
	// show it as a connection in progress rather than a dead one.
	Reconnecting bool `json:"reconnecting"`
	// TransportsExhausted marks a session no transport gets traffic through here.
	// Clients rotate servers on it; the daemon cannot, a server being a profile.
	TransportsExhausted bool `json:"transportsExhausted"`
	// Offline marks a confident OS verdict of no internet (link physically down)
	// while a session is intended. Clients show "no internet" and hold, rather
	// than reading the retry backoff as a connection endlessly failing.
	Offline bool `json:"offline"`
}

type CloakProfile struct {
	LocalPort        int    `json:"localPort"`
	RemoteHost       string `json:"remoteHost"`
	RemotePort       int    `json:"remotePort"`
	UID              string `json:"uid"`
	PublicKey        string `json:"publicKey"`
	EncryptionMethod string `json:"encryptionMethod"`
	Password         string `json:"password"`
	// Cover SNI Cloak presents; empty => buildRawConfig defaults to www.microsoft.com.
	ServerName string `json:"serverName,omitempty"`
	// ProxyMethod is the server-side ProxyBook key naming where decoded
	// traffic goes. Empty => DefaultCloakProxyMethod. Set by ApplyHop.
	ProxyMethod string `json:"proxyMethod,omitempty"`
}

// NaiveProfile carries per-device NaiveProxy credentials, hub-provisioned
// the same way CloakProfile is. Nil on a Profile means no NaiveProxy
// fallback is configured for that profile.
type NaiveProfile struct {
	LocalPort  int    `json:"localPort"`
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	// ServerName is the cover SNI presented during the TLS handshake
	// (naive's --proxy host), analogous to CloakProfile.ServerName.
	ServerName string `json:"serverName,omitempty"`
	// BridgePort is the remote node's framed-UDP bridge this tunnel dials
	// through the CONNECT stream. Zero => DefaultNaiveBridgePort. Set by ApplyHop.
	BridgePort int `json:"bridgePort,omitempty"`
}

// RealityProfile carries per-device VLESS+REALITY credentials, hub-provisioned
// the same way NaiveProfile is. Nil on a Profile means no REALITY transport
// is configured for that profile.
type RealityProfile struct {
	LocalPort  int    `json:"localPort"`
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	UUID       string `json:"uuid"`
	// PublicKey is the REALITY server's X25519 public key, base64
	// RawURLEncoding (32 bytes decoded).
	PublicKey string `json:"publicKey"`
	// ShortID is the REALITY short ID, hex-encoded (<=8 bytes).
	ShortID string `json:"shortId"`
	Flow    string `json:"flow,omitempty"`
	// ServerName is the REALITY SNI / camouflage target hostname.
	ServerName string `json:"serverName,omitempty"`
	// TargetPort is the loopback port on the remote node that decoded UDP
	// is forwarded to (the node's local WireGuard listener). Defaults to
	// 51820 (WireGuard's standard port) when zero.
	TargetPort int `json:"targetPort,omitempty"`
}

// Hysteria2Profile carries per-device Hysteria2 (QUIC transport, Salamander
// obfuscation) credentials, hub-provisioned the same way NaiveProfile is.
// Nil on a Profile means no Hysteria2 transport is configured for that
// profile. The real destination this tunnel relays WireGuard traffic to
// (the node's WireGuard listener) is not carried here — same convention as
// Cloak/NaiveProxy: it is a fixed server-side detail, not client config.
type Hysteria2Profile struct {
	LocalPort  int    `json:"localPort"`
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	// ServerName is the SNI presented during the TLS handshake.
	ServerName string `json:"serverName,omitempty"`
	// Password is the Hysteria2 client auth password.
	Password string `json:"password"`
	// ObfsPassword is the Salamander QUIC obfuscator password.
	ObfsPassword string `json:"obfsPassword"`
	// UpMbps/DownMbps cap Hysteria2's bandwidth-aware congestion control;
	// 0 means unset (server default / no explicit cap).
	UpMbps   int `json:"upMbps,omitempty"`
	DownMbps int `json:"downMbps,omitempty"`
	// Insecure skips server certificate verification.
	Insecure bool `json:"insecure,omitempty"`
	// PinSHA256 is a base64-encoded SHA-256 hash of the server certificate's
	// public key; when set, the cert is pinned regardless of Insecure.
	PinSHA256 string `json:"pinSha256,omitempty"`
	// TargetPort is the loopback port on the remote node that decoded UDP is
	// forwarded to. Zero => DefaultWireGuardPort. Set by ApplyHop.
	TargetPort int `json:"targetPort,omitempty"`
}

// ShadowsocksProfile carries per-node Shadowsocks (AEAD or SS-2022) settings.
// Nil on a Profile means no Shadowsocks transport is configured.
type ShadowsocksProfile struct {
	LocalPort  int    `json:"localPort"`
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Method     string `json:"method"`
	Password   string `json:"password"`
	// Where the SS server relays decoded packets: the node's WireGuard listener.
	// Client config because the node's ACL scopes it. Default 127.0.0.1:51820.
	TargetHost string `json:"targetHost,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
	// UDPOverTCP tunnels WireGuard's UDP inside the SS TCP stream. Slower, but
	// the only thing that works where UDP is dropped outright.
	UDPOverTCP bool `json:"udpOverTcp,omitempty"`
}

// SnowflakeProfile carries per-device Tor Snowflake (WebRTC rendezvous)
// settings. Unlike Cloak/NaiveProxy/REALITY/Hysteria2, Snowflake has no
// single fixed remote host: rendezvous happens against a broker (optionally
// via domain fronting or an AMP cache), and the actual data-plane peer is a
// volunteer WebRTC proxy discovered dynamically per-session. Nil on a
// Profile means no Snowflake transport is configured for that profile.
type SnowflakeProfile struct {
	LocalPort int    `json:"localPort"`
	BrokerURL string `json:"brokerURL"`
	// FrontDomains are candidate domain-fronting SNI hosts tried during
	// rendezvous; empty means no fronting (direct broker connection).
	FrontDomains []string `json:"fronts,omitempty"`
	// AmpCacheURL, when set, routes rendezvous through an AMP cache instead
	// of a direct/fronted broker request.
	AmpCacheURL string `json:"ampCacheUrl,omitempty"`
	// ICEServers are STUN/TURN URLs used for WebRTC NAT traversal.
	ICEServers []string `json:"iceServers,omitempty"`
	// BridgeFingerprint identifies the Tor bridge the rendezvous connects to.
	BridgeFingerprint string `json:"bridgeFingerprint"`
	// KeepLocalAddresses retains local/loopback ICE candidates; only useful
	// for local testing, never set in production profiles.
	KeepLocalAddresses bool `json:"keepLocalAddresses,omitempty"`
}

type WireGuardProfile struct {
	ConfigText  string   `json:"configText"`
	TunnelName  string   `json:"tunnelName"`
	DNS         []string `json:"dns"`
	BypassHosts []string `json:"bypassHosts,omitempty"`
	// HubInTunnel keeps BypassHosts out of the routing bypass so hub traffic
	// goes through the tunnel; they stay kill-switch permitted either way.
	HubInTunnel bool `json:"hubInTunnel,omitempty"`
	// DirectEndpoint is the node's own WireGuard listener as host:port. ConfigText
	// always points at a loopback transport bridge instead, so this is what the
	// direct "wireguard" method rewrites the Endpoint line to. Empty means the
	// profile cannot be connected without a transport in front of it.
	DirectEndpoint string `json:"directEndpoint,omitempty"`
}

// DefaultWireGuardPort is where a node's own WireGuard listener sits, and so
// where every transport forwards decoded traffic on a single-hop profile.
const DefaultWireGuardPort = 51820

// HopProfile makes a profile multihop: the transport terminates on an entry
// node that relays WireGuard on to the exit node holding the peer. Nil means
// single-hop. Selectors differ because the wire protocols do — Cloak names a
// ProxyBook key, sing-box a destination port, naive a bridge port — and each
// is issued by the hub, never derived client-side from a node ordering.
type HopProfile struct {
	// SingBoxPort is the entry's loopback hop port for REALITY, Hysteria2
	// and Shadowsocks.
	SingBoxPort int `json:"singBoxPort"`
	// CloakProxyMethod is the entry's ProxyBook key routing to this exit.
	CloakProxyMethod string `json:"cloakProxyMethod,omitempty"`
	// NaiveBridgePort is the entry's framed-UDP bridge for this exit.
	NaiveBridgePort int `json:"naiveBridgePort,omitempty"`
	// EntryRegion and ExitRegion label the session for the UI. Display only;
	// routing never reads them.
	EntryRegion string `json:"entryRegion,omitempty"`
	ExitRegion  string `json:"exitRegion,omitempty"`
}

type Profile struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Cloak CloakProfile `json:"cloak"`
	// Hop is optional; nil means single-hop. See HopProfile.
	Hop *HopProfile `json:"hop,omitempty"`
	// Naive is optional; nil means this profile has no NaiveProxy fallback
	// configured and Connect only ever tries Cloak.
	Naive *NaiveProfile `json:"naive,omitempty"`
	// Reality is optional; nil means this profile has no VLESS+REALITY
	// transport configured.
	Reality *RealityProfile `json:"reality,omitempty"`
	// Hysteria2 is optional; nil means this profile has no Hysteria2
	// transport configured.
	Hysteria2 *Hysteria2Profile `json:"hysteria2,omitempty"`
	// Shadowsocks is optional; nil means this profile has no Shadowsocks
	// transport configured.
	Shadowsocks *ShadowsocksProfile `json:"shadowsocks,omitempty"`
	// Snowflake is optional; nil means this profile has no Snowflake
	// transport configured.
	Snowflake *SnowflakeProfile `json:"snowflake,omitempty"`
	// TransportEndpointIPs are this node's transport endpoints as raw IPs, as
	// the hub reported them. The kill switch permits these and WireGuard routes
	// them outside the tunnel with no DNS lookup — a lookup is impossible
	// behind an engaged Lockdown lock, which blocks DNS, so without these only
	// Cloak (whose remote host is already an IP) could get out.
	TransportEndpointIPs []string         `json:"transportEndpointIPs,omitempty"`
	WireGuard            WireGuardProfile `json:"wireguard"`
}

type Config struct {
	Profiles []Profile `json:"profiles"`
}

func DefaultConfig() Config {
	return Config{Profiles: []Profile{}}
}
