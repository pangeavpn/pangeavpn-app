package mobile

// JSON shapes exchanged with the Kotlin side. Field names and nullability
// mirror apps/desktop/src/shared/ipc.ts exactly.

type cloakInfo struct {
	RemoteHost string `json:"remoteHost"`
	UID        string `json:"uid"`
	PublicKey  string `json:"publicKey"`
	ServerName string `json:"serverName,omitempty"`
}

// realityInfo and the transport structs below mirror ServerInfo in
// apps/desktop/src/shared/ipc.ts, which is the contract with the hub.
type naiveInfo struct {
	RemoteHost string `json:"remoteHost"`
	RemoteIP   string `json:"remoteIp,omitempty"`
	RemotePort int    `json:"remotePort"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	ServerName string `json:"serverName,omitempty"`
}

type realityInfo struct {
	RemoteHost string `json:"remoteHost"`
	RemoteIP   string `json:"remoteIp,omitempty"`
	RemotePort int    `json:"remotePort"`
	UUID       string `json:"uuid"`
	PublicKey  string `json:"publicKey"`
	ShortID    string `json:"shortId"`
	Flow       string `json:"flow,omitempty"`
	ServerName string `json:"serverName,omitempty"`
}

type hysteria2Info struct {
	RemoteHost   string `json:"remoteHost"`
	RemoteIP     string `json:"remoteIp,omitempty"`
	RemotePort   int    `json:"remotePort"`
	Password     string `json:"password"`
	ObfsPassword string `json:"obfsPassword"`
	ServerName   string `json:"serverName,omitempty"`
	PinSHA256    string `json:"pinSha256,omitempty"`
}

type shadowsocksInfo struct {
	RemoteHost string `json:"remoteHost"`
	RemoteIP   string `json:"remoteIp,omitempty"`
	RemotePort int    `json:"remotePort"`
	Method     string `json:"method"`
	Password   string `json:"password"`
	TargetHost string `json:"targetHost,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
	UDPOverTCP bool   `json:"udpOverTcp,omitempty"`
}

// controlPlaneShadowsocksInfo reaches the hub rather than WireGuard.
type controlPlaneShadowsocksInfo struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Method     string `json:"method"`
	Password   string `json:"password"`
}

type snowflakeInfo struct {
	BrokerURL         string   `json:"brokerURL"`
	BridgeFingerprint string   `json:"bridgeFingerprint"`
	FrontDomains      []string `json:"frontDomains,omitempty"`
	AmpCacheURL       string   `json:"ampCacheURL,omitempty"`
	ICEServers        []string `json:"iceServers,omitempty"`
}

type serverInfo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Region  string    `json:"region"`
	Country string    `json:"country"`
	Load    *int      `json:"load"`
	Cloak   cloakInfo `json:"cloak"`
	// Each is nil when the hub node has no listener for that transport,
	// which is what makes StarterFor skip it.
	Naive                   *naiveInfo                   `json:"naive,omitempty"`
	Reality                 *realityInfo                 `json:"reality,omitempty"`
	Hysteria2               *hysteria2Info               `json:"hysteria2,omitempty"`
	Shadowsocks             *shadowsocksInfo             `json:"shadowsocks,omitempty"`
	ControlPlaneShadowsocks *controlPlaneShadowsocksInfo `json:"controlPlaneShadowsocks,omitempty"`
	Snowflake               *snowflakeInfo               `json:"snowflake,omitempty"`
}

type sessionJSON struct {
	Email   string       `json:"email"`
	Name    string       `json:"name"`
	Servers []serverInfo `json:"servers"`
}

type subscriptionInfo struct {
	Status    string  `json:"status"`
	Renews    bool    `json:"renews"`
	ExpiresAt *string `json:"expiresAt"`
}

type deviceInfo struct {
	ID           string  `json:"id"`
	FriendlyName *string `json:"friendlyName"`
	CreatedAt    string  `json:"createdAt"`
	Status       string  `json:"status"`
}

type tunnelConfigJSON struct {
	Address      string   `json:"address"`
	PrefixLength int      `json:"prefixLength"`
	DNS          []string `json:"dns"`
	MTU          int      `json:"mtu"`
	ServerID     string   `json:"serverId"`
	ServerName   string   `json:"serverName"`
}

type statusJSON struct {
	State      string `json:"state"`
	Detail     string `json:"detail"`
	BytesIn    int64  `json:"bytesIn"`
	BytesOut   int64  `json:"bytesOut"`
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	// ActiveTransport is which cascade rung carried the live tunnel.
	ActiveTransport string `json:"activeTransport,omitempty"`
}
