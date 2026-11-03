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
	// RemotePorts are "start:end" UDP ranges to hop across; absent means no hopping.
	RemotePorts []string `json:"remotePorts,omitempty"`
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

type anytlsInfo struct {
	RemoteHost string `json:"remoteHost"`
	RemoteIP   string `json:"remoteIp,omitempty"`
	RemotePort int    `json:"remotePort"`
	Password   string `json:"password"`
	ServerName string `json:"serverName,omitempty"`
	Insecure   bool   `json:"insecure,omitempty"`
	PinSHA256  string `json:"pinSha256,omitempty"`
	TargetHost string `json:"targetHost,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
}

// controlPlaneShadowsocksInfo reaches the hub rather than WireGuard.
type controlPlaneShadowsocksInfo struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Method     string `json:"method"`
	Password   string `json:"password"`
}

// controlPlaneRealityInfo is the node's REALITY user that reaches only the hub.
type controlPlaneRealityInfo struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	UUID       string `json:"uuid"`
	PublicKey  string `json:"publicKey"`
	ShortID    string `json:"shortId"`
	ServerName string `json:"serverName"`
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
	AnyTLS                  *anytlsInfo                  `json:"anytls,omitempty"`
	ControlPlaneShadowsocks *controlPlaneShadowsocksInfo `json:"controlPlaneShadowsocks,omitempty"`
	ControlPlaneReality     *controlPlaneRealityInfo     `json:"controlPlaneReality,omitempty"`
	Snowflake               *snowflakeInfo               `json:"snowflake,omitempty"`
	// FrontedEndpoints repeats per region: the regions route answers with a
	// bare array, so there is nowhere top-level to put it.
	FrontedEndpoints []string `json:"frontedEndpoints,omitempty"`
	// Multihop marks a node that may relay as the entry of a two-hop route.
	Multihop bool `json:"multihop,omitempty"`
}

// publicServer is all the host sees of a server: credentials never leave Go.
// Each transport flag means the hub advertised it and this build can run it.
type publicServer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Region      string `json:"region"`
	Country     string `json:"country"`
	Load        *int   `json:"load"`
	Multihop    bool   `json:"multihop"`
	Cloak       bool   `json:"cloak"`
	Reality     bool   `json:"reality"`
	Hysteria2   bool   `json:"hysteria2"`
	Shadowsocks bool   `json:"shadowsocks"`
	AnyTLS      bool   `json:"anytls"`
	Naive       bool   `json:"naive"`
	Snowflake   bool   `json:"snowflake"`
}

type sessionJSON struct {
	Email   string         `json:"email"`
	Name    string         `json:"name"`
	Servers []publicServer `json:"servers"`
	// Cached marks a list served from the store because the hub was unreachable.
	Cached bool `json:"cached"`
}

type subscriptionInfo struct {
	Status    string  `json:"status"`
	Renews    bool    `json:"renews"`
	ExpiresAt *string `json:"expiresAt"`
	Entitled  bool    `json:"entitled"`
	Cached    bool    `json:"cached"`
}

// hubDevice is the hub's row; its identity key is never passed to the host.
type hubDevice struct {
	ID             string  `json:"id"`
	IdentityPubkey *string `json:"identityPubkey"`
	FriendlyName   *string `json:"friendlyName"`
	CreatedAt      string  `json:"createdAt"`
	Status         string  `json:"status"`
}

type deviceInfo struct {
	ID           string  `json:"id"`
	FriendlyName *string `json:"friendlyName"`
	CreatedAt    string  `json:"createdAt"`
	Status       string  `json:"status"`
	Current      bool    `json:"current"`
}

type tunnelConfigJSON struct {
	Address      string   `json:"address"`
	PrefixLength int      `json:"prefixLength"`
	DNS          []string `json:"dns"`
	MTU          int      `json:"mtu"`
	ServerID     string   `json:"serverId"`
	ServerName   string   `json:"serverName"`
	// Set only for a multihop route; ServerID stays the exit.
	EntryServerID   string `json:"entryServerId,omitempty"`
	EntryServerName string `json:"entryServerName,omitempty"`
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
	// LastHandshake is unix seconds of the newest WireGuard handshake, 0 if none.
	LastHandshake int64 `json:"lastHandshake"`
	PostQuantum   bool  `json:"postQuantum"`
	// Set only for a multihop route; ServerID stays the exit.
	EntryServerID   string `json:"entryServerId,omitempty"`
	EntryServerName string `json:"entryServerName,omitempty"`
}
