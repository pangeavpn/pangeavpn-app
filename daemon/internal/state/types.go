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
	SourceDaemon    LogSource = "daemon"
	SourceCloak     LogSource = "cloak"
	SourceNaive     LogSource = "naive"
	SourceSnowflake LogSource = "snowflake"
	SourceWireGuard LogSource = "wireguard"
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
	// ActiveTransport is "cloak", "naive", "reality", "hysteria2", "snowflake", or "" when disconnected.
	ActiveTransport  string          `json:"activeTransport"`
	Cloak            CloakStatus     `json:"cloak"`
	Naive            TransportStatus `json:"naive"`
	Reality          TransportStatus `json:"reality"`
	Hysteria2        TransportStatus `json:"hysteria2"`
	Snowflake        TransportStatus `json:"snowflake"`
	WireGuard        WireGuardStatus `json:"wireguard"`
	KillSwitchActive bool            `json:"killSwitchActive"`
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
}

type Profile struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Cloak CloakProfile `json:"cloak"`
	// Naive is optional; nil means this profile has no NaiveProxy fallback
	// configured and Connect only ever tries Cloak.
	Naive *NaiveProfile `json:"naive,omitempty"`
	// Reality is optional; nil means this profile has no VLESS+REALITY
	// transport configured.
	Reality *RealityProfile `json:"reality,omitempty"`
	// Hysteria2 is optional; nil means this profile has no Hysteria2
	// transport configured.
	Hysteria2 *Hysteria2Profile `json:"hysteria2,omitempty"`
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
