//go:build android

// Package mobile is the gomobile control+data plane for PangeaVPN Android.
// It ports apps/desktop/src/main/{secureChannel,pangeaApiClient,main,auth}.ts
// (control plane) and reuses daemon/internal/cloak (data plane transport).
package mobile

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// SocketProtector routes a raw socket fd through VpnService.protect() so
// outbound control/cloak dials aren't captured by the TUN.
type SocketProtector interface{ Protect(fd int) bool }

// StatusSink receives Status JSON pushes on every state transition.
type StatusSink interface{ OnStatus(statusJSON string) }

// SecretStore persists small secrets (license key, device identity) outside
// the Go heap, e.g. Android EncryptedSharedPreferences. Get returns "" when
// the key is absent.
type SecretStore interface {
	Get(key string) string
	Set(key, value string)
}

const (
	keyLicense  = "licenseKey"
	keyIdentity = "identityKey"
)

type preparedTunnel struct {
	wgPrivateKeyRaw []byte
	serverPubKeyRaw []byte
	cloakProfile    state.CloakProfile
	serverID        string
	serverName      string
}

var (
	mu        sync.Mutex
	store     SecretStore
	protector SocketProtector
	sink      StatusSink

	licenseKey string
	identity   *identityKeyPair
	servers    []serverInfo
	prepared   *preparedTunnel

	activeTunnel *tunnelRuntime
	cloakMgr     cloak.Manager
	wgLogs       *state.LogStore

	currentStatus statusJSON
)

// Init wires the host callbacks and restores any previously persisted
// license key / device identity. Must be called once before any other
// exported function.
func Init(s SecretStore, p SocketProtector, sk StatusSink) {
	logs := state.NewLogStore(500)

	mu.Lock()
	store = s
	protector = p
	sink = sk
	licenseKey = strings.TrimSpace(s.Get(keyLicense))
	identity = loadIdentity(s)
	wgLogs = logs
	cloakMgr = cloak.NewManager(logs)
	currentStatus = statusJSON{State: string(state.StateDisconnected)}
	mu.Unlock()

	cloak.DialerControl = protectingControl
}

// protectingControl is installed as net.Dialer.Control for every real-network
// dial the control plane and cloak make (DoH, hub, cloak-to-server). The
// WireGuard<->cloak hop is loopback and never needs protecting.
func protectingControl(_, address string, c syscall.RawConn) error {
	mu.Lock()
	p := protector
	mu.Unlock()
	if p == nil {
		return nil
	}
	var protectErr error
	if err := c.Control(func(fd uintptr) {
		if !p.Protect(int(fd)) {
			protectErr = fmt.Errorf("protect failed for %s", address)
		}
	}); err != nil {
		return err
	}
	return protectErr
}

func pushStatus(st, detail, serverID, serverName string) {
	mu.Lock()
	currentStatus = statusJSON{State: st, Detail: detail, ServerID: serverID, ServerName: serverName}
	snapshot := currentStatus
	sk := sink
	mu.Unlock()

	if sk == nil {
		return
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	sk.OnStatus(string(b))
}

// Login performs a token login through the secure channel, generating and
// registering a device identity on first use, and returns the Session JSON.
func Login(token string) (string, error) {
	mu.Lock()
	ident := identity
	st := store
	mu.Unlock()
	if st == nil {
		return "", errors.New("mobile: not initialized")
	}

	if ident == nil {
		var err error
		ident, err = generateIdentity()
		if err != nil {
			return "", err
		}
		mu.Lock()
		identity = ident
		mu.Unlock()
		st.Set(keyIdentity, ident.marshal())
	}

	resp, err := tokenLogin(token, ident.pubB64)
	if err != nil {
		return "", err
	}

	mu.Lock()
	licenseKey = resp.VpnAccessToken
	mu.Unlock()

	if _, err := registerDevice(ident.pubB64, "Android"); err != nil {
		st.Set(keyLicense, "")
		return "", err
	}

	mu.Lock()
	servers = resp.Servers
	mu.Unlock()
	st.Set(keyLicense, licenseKey)

	b, err := json.Marshal(sessionJSON{Email: resp.User.Email, Name: resp.User.Name, Servers: resp.Servers})
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	return string(b), nil
}

// RestoreSession validates the stored license key against the hub and
// rebuilds the Session JSON. Email/name are not persisted locally (only
// licenseKey and identityKey are), so they come back empty on restore.
func RestoreSession() (string, error) {
	mu.Lock()
	lk := licenseKey
	ident := identity
	mu.Unlock()
	if lk == "" {
		return "", errors.New("mobile: no stored session")
	}
	if ident == nil {
		return "", errors.New("mobile: no device identity")
	}

	srvs, err := fetchServers(ident.pubB64)
	if err != nil {
		return "", err
	}

	mu.Lock()
	servers = srvs
	mu.Unlock()

	b, err := json.Marshal(sessionJSON{Servers: srvs})
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	return string(b), nil
}

// Logout clears the stored license key and in-memory session state. The
// device identity is kept so a later Login reuses the same device slot.
func Logout() {
	mu.Lock()
	st := store
	licenseKey = ""
	servers = nil
	prepared = nil
	mu.Unlock()

	if st != nil {
		st.Set(keyLicense, "")
	}
}

// ListServers fetches the current region list from the hub.
func ListServers() (string, error) {
	mu.Lock()
	lk := licenseKey
	ident := identity
	mu.Unlock()
	if lk == "" {
		return "", errors.New("mobile: not authenticated")
	}
	pub := ""
	if ident != nil {
		pub = ident.pubB64
	}

	srvs, err := fetchServers(pub)
	if err != nil {
		return "", err
	}

	mu.Lock()
	servers = srvs
	mu.Unlock()

	b, err := json.Marshal(srvs)
	if err != nil {
		return "", fmt.Errorf("marshal servers: %w", err)
	}
	return string(b), nil
}

// GetSubscription returns the account's subscription info, or the literal
// string "null" when the account has none.
func GetSubscription() (string, error) {
	mu.Lock()
	lk := licenseKey
	mu.Unlock()
	if lk == "" {
		return "", errors.New("mobile: not authenticated")
	}

	sub, err := getSubscriptionHub()
	if err != nil {
		return "", err
	}
	if sub == nil {
		return "null", nil
	}
	b, err := json.Marshal(sub)
	if err != nil {
		return "", fmt.Errorf("marshal subscription: %w", err)
	}
	return string(b), nil
}

// ListDevices returns the account's registered devices.
func ListDevices() (string, error) {
	mu.Lock()
	lk := licenseKey
	mu.Unlock()
	if lk == "" {
		return "", errors.New("mobile: not authenticated")
	}

	devices, err := listDevicesHub()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(devices)
	if err != nil {
		return "", fmt.Errorf("marshal devices: %w", err)
	}
	return string(b), nil
}

// RemoveDevice deregisters a device from the account, freeing its slot.
func RemoveDevice(deviceID string) error {
	mu.Lock()
	lk := licenseKey
	mu.Unlock()
	if lk == "" {
		return errors.New("mobile: not authenticated")
	}
	return removeDeviceHub(deviceID)
}

// Prepare provisions a fresh WireGuard config against serverId and stashes
// it in memory for the next Start call.
func Prepare(serverID string) (string, error) {
	mu.Lock()
	lk := licenseKey
	ident := identity
	srvs := servers
	mu.Unlock()
	if lk == "" {
		return "", errors.New("mobile: not authenticated")
	}
	if ident == nil {
		return "", errors.New("mobile: no device identity")
	}

	var target *serverInfo
	for i := range srvs {
		if srvs[i].ID == serverID {
			target = &srvs[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("mobile: unknown server %q", serverID)
	}

	wgPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate wireguard key: %w", err)
	}
	wgPubB64 := base64.StdEncoding.EncodeToString(wgPriv.PublicKey().Bytes())

	reg, err := registerServer(ident.pubB64, wgPubB64, serverID)
	if err != nil {
		return "", err
	}
	if reg.ServerPubkey == "" || reg.AssignedIP == "" || reg.DNS == "" {
		return "", errors.New("mobile: server returned an incomplete response; device may have been removed")
	}

	serverPubRaw, err := base64.StdEncoding.DecodeString(reg.ServerPubkey)
	if err != nil {
		return "", fmt.Errorf("decode server public key: %w", err)
	}

	var dnsServers []string
	for _, part := range strings.Split(reg.DNS, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			dnsServers = append(dnsServers, trimmed)
		}
	}

	mu.Lock()
	prepared = &preparedTunnel{
		wgPrivateKeyRaw: wgPriv.Bytes(),
		serverPubKeyRaw: serverPubRaw,
		cloakProfile: state.CloakProfile{
			LocalPort:        0,
			RemoteHost:       target.Cloak.RemoteHost,
			RemotePort:       443,
			UID:              target.Cloak.UID,
			PublicKey:        target.Cloak.PublicKey,
			EncryptionMethod: "plain",
			ServerName:       target.Cloak.ServerName,
		},
		serverID:   serverID,
		serverName: target.Name,
	}
	mu.Unlock()

	b, err := json.Marshal(tunnelConfigJSON{
		Address:      reg.AssignedIP,
		PrefixLength: 32,
		DNS:          dnsServers,
		MTU:          1380,
		ServerID:     serverID,
		ServerName:   target.Name,
	})
	if err != nil {
		return "", fmt.Errorf("marshal tunnel config: %w", err)
	}
	return string(b), nil
}

// Start brings up cloak + wireguard-go on the TUN fd handed over by
// VpnService (already detached, i.e. owned by this call).
func Start(tunFd int) error {
	return startTunnel(tunFd)
}

// Stop tears down the running tunnel, if any.
func Stop() {
	stopTunnel()
}

// State returns the current Status JSON, with live byte counters when
// connected.
func State() string {
	mu.Lock()
	snapshot := currentStatus
	t := activeTunnel
	mu.Unlock()

	if t != nil {
		rx, tx := tunnelBytes(t.dev)
		snapshot.BytesIn = rx
		snapshot.BytesOut = tx
	}

	b, err := json.Marshal(snapshot)
	if err != nil {
		return "{}"
	}
	return string(b)
}
