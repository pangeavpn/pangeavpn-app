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
	// ActiveTransport is "cloak", "naive", "reality", or "" when disconnected.
	ActiveTransport  string          `json:"activeTransport"`
	Cloak            CloakStatus     `json:"cloak"`
	Naive            TransportStatus `json:"naive"`
	Reality          TransportStatus `json:"reality"`
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
	Reality   *RealityProfile  `json:"reality,omitempty"`
	WireGuard WireGuardProfile `json:"wireguard"`
}

type Config struct {
	Profiles []Profile `json:"profiles"`
}

func DefaultConfig() Config {
	return Config{Profiles: []Profile{}}
}
