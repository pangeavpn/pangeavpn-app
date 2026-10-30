//go:build android || ios

package mobile

// Ending a session: the hub refusing it, the user signing out, and a wipe.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// storedKeys is every SecretStore key this package writes; Wipe clears them
// all, so a new key belongs here or a wipe leaves it behind.
var storedKeys = []string{
	keyLicense, keyIdentity, keyConfig,
	keyHubIP, keyFronted, keyHubSS, keyHubReality,
	keyDeadDropSeq, keyDeadDropAttempt,
	keySubscription, keyServers,
}

// sessionKeys belong to one signed-in account and go with it.
var sessionKeys = []string{keyLicense, keySubscription, keyServers}

// signOutLocally ends a session the hub has definitively refused, so no later
// start retries a dead key. The identity stays, keeping its device slot.
func signOutLocally() {
	mu.Lock()
	st := store
	logs := wgLogs
	licenseKey = ""
	servers = nil
	prepared = nil
	mu.Unlock()

	if st != nil {
		for _, key := range sessionKeys {
			st.Set(key, "")
		}
	}
	logAdd(logs, state.LogWarn, "the hub refused the stored session; signed out")
}

// Deregister frees this install's device slot on the hub; call it before
// Logout. It forgets the identity only once the hub no longer holds it.
func Deregister() error {
	mu.Lock()
	lk, ident := licenseKey, identity
	mu.Unlock()
	if lk == "" {
		return errors.New("mobile: not authenticated")
	}
	if ident == nil {
		return errors.New("mobile: no device identity")
	}

	err := deregisterDeviceHub(lk, ident.pubB64)
	switch code := hubErrorCode(err); {
	case err == nil, code == "DEVICE_NOT_FOUND", code == "FORBIDDEN":
		forgetIdentity(ident.pubB64)
		return nil
	}
	return err
}

// Wipe forgets everything this install knows: session, identity, settings, hub
// routes, transport history and logs. Local only; it never calls the hub.
func Wipe() error {
	Stop()

	mu.Lock()
	st, memory, path, logs := store, transportMem, transportMemPath, wgLogs
	licenseKey, identity, servers, prepared = "", nil, nil, nil
	settings = defaultConfig()
	preferredTransport = settings.PreferredTransport
	currentStatus = statusJSON{State: string(state.StateDisconnected)}
	mu.Unlock()

	// After the reset, so a search still running under the old settings is discarded.
	invalidateHubPath()
	dropDoHConnections()

	if st != nil {
		for _, key := range storedKeys {
			st.Set(key, "")
		}
	}
	var errs []error
	if memory != nil {
		if err := memory.Clear(); err != nil {
			errs = append(errs, fmt.Errorf("clear transport memory: %w", err))
		}
	}
	if path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove transport memory: %w", err))
		}
	}
	// Cleared in place: the WireGuard logger reads this store without a lock.
	if logs != nil {
		logs.Clear()
	}
	return errors.Join(errs...)
}
