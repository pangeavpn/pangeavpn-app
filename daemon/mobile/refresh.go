//go:build android || ios

package mobile

// What the host calls when something outside this Go runtime changed: another
// process wrote the shared store, or the device moved to a different network.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const transportMemoryFile = "transport-memory.json"

// storedState is everything Init and Reload load from the SecretStore.
type storedState struct {
	licenseKey string
	identity   *identityKeyPair
	settings   config
}

// readStoredState falls back to absent or default values for anything corrupt,
// and reports what it could not decode.
func readStoredState(s SecretStore) (storedState, error) {
	ident, identErr := parseIdentity(s.Get(keyIdentity))
	cfg, cfgErr := parseConfig(s.Get(keyConfig))
	loaded := storedState{
		licenseKey: strings.TrimSpace(s.Get(keyLicense)),
		identity:   ident,
		settings:   cfg,
	}
	return loaded, errors.Join(identErr, cfgErr)
}

func transportMemoryPath(filesDir string) string {
	return filepath.Join(filesDir, transportMemoryFile)
}

// openTransportMemory returns nil when the file cannot be opened.
func openTransportMemory(path string, logs *state.LogStore) *state.TransportMemoryStore {
	memory, err := state.NewTransportMemoryStore(path)
	if err != nil {
		// Best-effort cache: losing it only forfeits trying the winner first.
		logAdd(logs, state.LogWarn, fmt.Sprintf("transport memory unavailable: %v", err))
		return nil
	}
	return memory
}

// Reload re-reads the session, settings and transport history another process may
// have changed, never touching a running tunnel. It errors only on a corrupt value.
func Reload() error {
	mu.Lock()
	s := store
	path := transportMemPath
	logs := wgLogs
	mu.Unlock()
	if s == nil {
		return nil
	}

	memory := openTransportMemory(path, logs)

	// Under the gate, so the license and identity read are one pair a Login
	// in this process stored together, never half of an old one.
	storeGate.Lock()
	loaded, decodeErr := readStoredState(s)
	mu.Lock()
	sessionChanged := loaded.licenseKey != licenseKey || loaded.identity.publicKey() != identity.publicKey()
	methodsChanged := loaded.settings.HubMethods != settings.HubMethods
	licenseKey = loaded.licenseKey
	identity = loaded.identity
	settings = loaded.settings
	preferredTransport = loaded.settings.PreferredTransport
	transportMem = memory
	// A config provisioned for another account or device must not be started,
	// and its server list carries that device's Cloak UID.
	if sessionChanged {
		setPreparedLocked(nil)
		servers = nil
		sessionEpoch++
	}
	if licenseKey == "" {
		servers = nil
	}
	mu.Unlock()
	storeGate.Unlock()

	if methodsChanged {
		invalidateHubPath()
	}
	if decodeErr != nil {
		return fmt.Errorf("mobile: reload: %w", decodeErr)
	}
	return nil
}

// NetworkChanged forgets what was learned on the previous network: the hub
// route, its failure cooldown and idle DoH connections. It never touches a tunnel.
func NetworkChanged() {
	invalidateHubPath()
	dropDoHConnections()
}

// dropDoHConnections keeps the client: an in-flight lookup returns its
// connection to this pool, and a replaced pool would never be drained.
func dropDoHConnections() {
	hubMu.Lock()
	client := dohHTTP
	hubMu.Unlock()
	if client != nil {
		client.CloseIdleConnections()
	}
}
