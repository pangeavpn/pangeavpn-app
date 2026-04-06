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

type WireGuardStatus struct {
	Running  bool   `json:"running"`
	Detail   string `json:"detail"`
	BytesIn  int64  `json:"bytesIn"`
	BytesOut int64  `json:"bytesOut"`
}

type StatusResponse struct {
	State            DaemonState     `json:"state"`
	Detail           string          `json:"detail"`
	Cloak            CloakStatus     `json:"cloak"`
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
}

type WireGuardProfile struct {
	ConfigText  string   `json:"configText"`
	TunnelName  string   `json:"tunnelName"`
	DNS         []string `json:"dns"`
	BypassHosts []string `json:"bypassHosts,omitempty"`
}

type Profile struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Cloak     CloakProfile     `json:"cloak"`
	WireGuard WireGuardProfile `json:"wireguard"`
}

type Config struct {
	Profiles []Profile `json:"profiles"`
}

func DefaultConfig() Config {
	return Config{Profiles: []Profile{}}
}
