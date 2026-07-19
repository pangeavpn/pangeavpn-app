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
}

type StatusResponse struct {
	State  DaemonState `json:"state"`
	Detail string      `json:"detail"`
	// ActiveTransport is "cloak", "naive", "hysteria2", or "" when disconnected.
	ActiveTransport  string          `json:"activeTransport"`
	Cloak            CloakStatus     `json:"cloak"`
	Naive            TransportStatus `json:"naive"`
	Hysteria2        TransportStatus `json:"hysteria2"`
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
	// Hysteria2 is optional; nil means this profile has no Hysteria2
	// transport configured.
	Hysteria2 *Hysteria2Profile `json:"hysteria2,omitempty"`
	WireGuard WireGuardProfile  `json:"wireguard"`
}

type Config struct {
	Profiles []Profile `json:"profiles"`
}

func DefaultConfig() Config {
	return Config{Profiles: []Profile{}}
}
