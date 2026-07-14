//go:build android

package mobile

// JSON shapes exchanged with the Kotlin side. Field names and nullability
// mirror apps/desktop/src/shared/ipc.ts exactly.

type cloakInfo struct {
	RemoteHost string `json:"remoteHost"`
	UID        string `json:"uid"`
	PublicKey  string `json:"publicKey"`
	ServerName string `json:"serverName,omitempty"`
}

type serverInfo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Region  string    `json:"region"`
	Country string    `json:"country"`
	Load    *int      `json:"load"`
	Cloak   cloakInfo `json:"cloak"`
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
}
