//go:build android || ios

// Package mobile is the gomobile control+data plane for PangeaVPN on Android
// and iOS, porting the desktop control plane and reusing its transports.
package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// SocketProtector routes a raw socket fd through VpnService.protect() so
// outbound control/cloak dials aren't captured by the TUN.
type SocketProtector interface{ Protect(fd int) bool }

// StatusSink receives Status JSON pushes on every state transition.
type StatusSink interface{ OnStatus(statusJSON string) }

// SecretStore persists small secrets outside the Go heap, e.g. Android
// EncryptedSharedPreferences. Get returns "" when the key is absent.
type SecretStore interface {
	Get(key string) string
	Set(key, value string)
}

const (
	keyLicense  = "licenseKey"
	keyIdentity = "identityKey"
	keyConfig   = "config"
)

type preparedTunnel struct {
	wgPrivateKeyRaw []byte
	serverPubKeyRaw []byte
	// presharedKeyRaw is nil when the peer is not post-quantum keyed.
	presharedKeyRaw []byte
	profile         *state.Profile
	serverID        string
	serverName      string
	// Set only for a multihop route; serverID stays the exit.
	entryServerID   string
	entryServerName string
	// starts counts the cascades still reading the keys; guarded by mu.
	starts int
}

var (
	mu        sync.Mutex
	store     SecretStore
	protector SocketProtector
	sink      StatusSink
	netKeys   NetworkKeyProvider

	licenseKey string
	identity   *identityKeyPair
	servers    []serverInfo
	prepared   *preparedTunnel
	// sessionEpoch moves whenever the signed-in session changes or ends;
	// wipeEpoch moves on Wipe alone. See epoch.go.
	sessionEpoch uint64
	wipeEpoch    uint64

	// preferredTransport is "" / "auto" for the cascade, or one transport kind.
	preferredTransport string
	transportMem       *state.TransportMemoryStore
	transportMemPath   string
	settings           = defaultConfig()

	activeTunnel *tunnelRuntime
	wgLogs       *state.LogStore

	currentStatus statusJSON
)

// NetworkKeyProvider identifies the network in use, so the cascade can try
// whatever last worked here first. Go cannot reach ConnectivityManager.
type NetworkKeyProvider interface{ NetworkKey() string }

// Init wires the host callbacks and restores any previously persisted
// license key / device identity. Call once before any other export.
func Init(s SecretStore, p SocketProtector, sk StatusSink, nk NetworkKeyProvider, filesDir string) {
	logs := state.NewLogStore(500)
	memoryPath := transportMemoryPath(filesDir)
	memory := openTransportMemory(memoryPath, logs)
	loaded, err := readStoredState(s)
	if err != nil {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("ignoring unreadable stored state: %v", err))
	}

	mu.Lock()
	store = s
	protector = p
	sink = sk
	netKeys = nk
	licenseKey = loaded.licenseKey
	identity = loaded.identity
	wgLogs = logs
	transportMem = memory
	transportMemPath = memoryPath
	settings = loaded.settings
	preferredTransport = settings.PreferredTransport
	currentStatus = statusJSON{State: string(state.StateDisconnected)}
	mu.Unlock()

	cloak.DialerControl = protectingControl
	_ = startProtectServer(filesDir, logs)
}

// SetPreferredTransport pins one transport, or "" / "auto" for the cascade.
func SetPreferredTransport(kind string) {
	mu.Lock()
	preferredTransport = strings.TrimSpace(kind)
	current := settings
	mu.Unlock()

	current.PreferredTransport = strings.TrimSpace(kind)
	persistConfig(current)
}

// PreferredTransport returns the pinned transport, "auto" when unset.
func PreferredTransport() string {
	mu.Lock()
	defer mu.Unlock()
	if preferredTransport == "" {
		return "auto"
	}
	return preferredTransport
}

// SetHubMethod flips one hub connection method, erroring rather than silently
// correcting when it would disable the last remaining way to reach the hub.
func SetHubMethod(method string, enabled bool) (string, error) {
	mu.Lock()
	current := settings
	mu.Unlock()

	next, applied := applyHubMethod(current.HubMethods, method, enabled)
	if !applied {
		if !isHubMethod(method) {
			return "", fmt.Errorf("unknown hub method %q", method)
		}
		return "", errors.New("at least one hub connection method must stay enabled")
	}
	current.HubMethods = next
	persistConfig(current)
	return encodeConfig(current)
}

// GetConfig returns the settings blob as JSON.
func GetConfig() (string, error) {
	mu.Lock()
	current := settings
	mu.Unlock()
	return encodeConfig(current)
}

// SetConfig replaces the settings from a JSON blob, returning what was stored
// after validation so the host can show corrected values.
func SetConfig(raw string) (string, error) {
	parsed := decodeConfig(raw)
	persistConfig(parsed)
	return encodeConfig(parsed)
}

// persistConfig writes settings through and mirrors the fields the connect
// path reads directly.
func persistConfig(c config) {
	sanitized := c.sanitize()
	encoded, err := encodeConfig(sanitized)

	// Under the gate, so a Wipe never leaves memory and the store disagreeing.
	storeGate.Lock()
	mu.Lock()
	methodsChanged := settings.HubMethods != sanitized.HubMethods
	settings = sanitized
	preferredTransport = sanitized.PreferredTransport
	s := store
	mu.Unlock()
	if err == nil && s != nil {
		s.Set(keyConfig, encoded)
	}
	storeGate.Unlock()

	// The route in use may belong to a method just switched off.
	if methodsChanged {
		invalidateHubPath()
	}
}

// networkKey identifies the current network, empty when the host offers none.
func networkKey() string {
	if netKeys == nil {
		return ""
	}
	return strings.TrimSpace(netKeys.NetworkKey())
}

// rememberTransport records the winning transport for this network.
func rememberTransport(key, kind string) {
	mu.Lock()
	memory := transportMem
	logs := wgLogs
	mu.Unlock()
	if memory == nil || key == "" || kind == "" {
		return
	}
	if err := memory.Record(key, kind); err != nil {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("failed to record last-good transport: %v", err))
	}
}

// protectingControl is net.Dialer.Control for every real-network dial. The
// WireGuard<->transport hop is loopback and never needs protecting.
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

func marshalSession(session sessionJSON) (string, error) {
	b, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	return string(b), nil
}

// publicServers is the host's view of servers: what this build can run there.
func publicServers(srvs []serverInfo) []publicServer {
	st := &starter{}
	return toPublicServers(srvs, func(profile *state.Profile, kind string) bool {
		_, availability := st.StarterFor(profile, kind)
		return availability == transport.Available
	})
}

// RestoreSession validates the stored license key and rebuilds the Session
// JSON. Email/name are not persisted, so they come back empty.
func RestoreSession() (string, error) {
	mu.Lock()
	scope, ident := scopeLocked(), identity
	mu.Unlock()
	if scope.licenseKey == "" {
		return "", errors.New("mobile: no stored session")
	}
	if ident == nil {
		return "", errors.New("mobile: no device identity")
	}

	srvs, cached, err := loadServers(scope, ident.pubB64)
	if err != nil {
		return "", err
	}
	return marshalSession(sessionJSON{Servers: publicServers(srvs), Cached: cached})
}

// loadServers fetches the region list, serving the cache only when the hub
// was never reached; an answer that ends the session signs out locally.
func loadServers(scope callScope, identityPub string) ([]serverInfo, bool, error) {
	srvs, err := fetchServers(scope, identityPub)
	cached := false
	if err != nil {
		if isSignOutCode(err) {
			signOutLocally(scope)
			return nil, false, err
		}
		if hubAnswered(err) || errors.Is(err, errSessionEnded) {
			return nil, false, err
		}
		srvs = restoreCachedServers(storedValue(keyServers), cacheOwner(scope.licenseKey, identityPub))
		if len(srvs) == 0 {
			return nil, false, err
		}
		cached = true
	}

	mu.Lock()
	defer mu.Unlock()
	// The cache may have been read just before a sign-out or wipe cleared it.
	if sessionEpoch != scope.session {
		return nil, false, errSessionEnded
	}
	servers = srvs
	return srvs, cached, nil
}

// Logout clears the stored license key, session state, caches and transport
// history. The identity is kept; Deregister first to free its device slot.
func Logout() {
	storeGate.Lock()
	mu.Lock()
	st := store
	memory := transportMem
	logs := wgLogs
	licenseKey = ""
	servers = nil
	setPreparedLocked(nil)
	sessionEpoch++
	mu.Unlock()
	if st != nil {
		for _, key := range sessionKeys {
			st.Set(key, "")
		}
	}
	storeGate.Unlock()

	// The history records which networks the tunnel got through on.
	if memory != nil {
		if err := memory.Clear(); err != nil {
			logAdd(logs, state.LogWarn, fmt.Sprintf("failed to clear transport memory: %v", err))
		}
	}
}

// ListServers fetches the current region list from the hub.
func ListServers() (string, error) {
	mu.Lock()
	scope, ident := scopeLocked(), identity
	mu.Unlock()
	if scope.licenseKey == "" {
		return "", errors.New("mobile: not authenticated")
	}

	srvs, _, err := loadServers(scope, ident.publicKey())
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(publicServers(srvs))
	if err != nil {
		return "", fmt.Errorf("marshal servers: %w", err)
	}
	return string(b), nil
}

// GetSubscription returns the account's subscription info, or the literal
// string "null" when the account has none.
func GetSubscription() (string, error) {
	mu.Lock()
	scope, pub := scopeLocked(), identity.publicKey()
	mu.Unlock()
	if scope.licenseKey == "" {
		return "", errors.New("mobile: not authenticated")
	}

	owner := cacheOwner(scope.licenseKey, pub)
	now := time.Now()
	sub, err := getSubscriptionHub(scope)
	var info subscriptionInfo
	switch {
	case err == nil && sub == nil:
		storeForSession(scope, keySubscription, "")
		return "null", nil
	case err == nil:
		storeForSession(scope, keySubscription, encodeCachedSubscription(*sub, owner, now))
		info = liveSubscriptionInfo(*sub)
	case hubAnswered(err) || errors.Is(err, errSessionEnded):
		var answer *hubAnswerError
		if errors.As(err, &answer) && answer.Auth {
			storeForSession(scope, keySubscription, "")
		}
		return "", err
	default:
		// Unreachable: the last answer, with entitlement re-derived for now.
		cached, ok := restoreCachedSubscription(storedValue(keySubscription), owner, now)
		if !ok {
			return "", err
		}
		if scope.sessionEnded() {
			return "", errSessionEnded
		}
		info = cachedSubscriptionInfo(cached, now)
	}

	b, err := json.Marshal(info)
	if err != nil {
		return "", fmt.Errorf("marshal subscription: %w", err)
	}
	return string(b), nil
}

// ListDevices returns the account's registered devices, flagging this one.
func ListDevices() (string, error) {
	mu.Lock()
	scope, pub := scopeLocked(), identity.publicKey()
	mu.Unlock()
	if scope.licenseKey == "" {
		return "", errors.New("mobile: not authenticated")
	}

	rows, err := listDevicesHub(scope)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(markCurrentDevices(rows, pub))
	if err != nil {
		return "", fmt.Errorf("marshal devices: %w", err)
	}
	return string(b), nil
}

// RemoveDevice deregisters a device from the account, freeing its slot.
func RemoveDevice(deviceID string) error {
	scope := captureScope()
	if scope.licenseKey == "" {
		return errors.New("mobile: not authenticated")
	}
	return removeDeviceHub(scope, deviceID)
}

// RenameDevice renames one of the account's devices and returns the name the
// hub stored. The name is checked first, so a bad one never costs a request.
func RenameDevice(deviceID, name string) (string, error) {
	scope := captureScope()
	if scope.licenseKey == "" {
		return "", errors.New("mobile: not authenticated")
	}
	if strings.TrimSpace(deviceID) == "" {
		return "", errors.New("mobile: missing device id")
	}
	trimmed, err := validateDeviceName(name)
	if err != nil {
		return "", err
	}

	stored, err := renameDeviceHub(scope, deviceID, trimmed)
	if err != nil {
		return "", err
	}
	if stored != nil && *stored != "" {
		return *stored, nil
	}
	return trimmed, nil
}

// AccountNumber is the stored license key, "" unless fully signed in. It is
// the bearer credential: never log it or put it in any other JSON.
func AccountNumber() string {
	mu.Lock()
	lk, ident, st := licenseKey, identity, store
	mu.Unlock()
	if lk == "" || ident == nil || st == nil {
		return ""
	}
	// Memory keeps the key through a device-limit login the store refused.
	if strings.TrimSpace(st.Get(keyLicense)) != lk {
		return ""
	}
	return lk
}
