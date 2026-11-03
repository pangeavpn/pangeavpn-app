//go:build android || ios

package mobile

// Ending a session: the hub refusing it, the user signing out or deleting the
// account, and a wipe.

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const accountDeleteRoute = "/api/client/account/delete"

// storedKeys is every SecretStore key this package writes; Wipe clears them
// all, so a new key belongs here or a wipe leaves it behind.
var storedKeys = []string{
	keyLicense, keyIdentity, keyConfig,
	keyHubIP, keyFronted, keyHubSS, keyHubReality,
	keyDeadDropSeq, keyDeadDropAttempt,
	keySubscription, keyServers, keyPendingDeregister,
}

// sessionKeys belong to one signed-in account and go with it.
var sessionKeys = []string{keyLicense, keySubscription, keyServers}

// signOutLocally ends a session the hub has definitively refused, so no later
// start retries a dead key. The identity stays, keeping its device slot.
func signOutLocally(scope callScope) {
	storeGate.Lock()
	defer storeGate.Unlock()
	mu.Lock()
	// The refusal was for the session scope captured, not whatever came after.
	if sessionEpoch != scope.session {
		mu.Unlock()
		return
	}
	st, logs := store, wgLogs
	licenseKey, servers = "", nil
	setPreparedLocked(nil)
	sessionEpoch++
	mu.Unlock()

	// The store is shared: a session another process stored since is not ours.
	if st != nil && strings.TrimSpace(st.Get(keyLicense)) == scope.licenseKey {
		for _, key := range sessionKeys {
			st.Set(key, "")
		}
	}
	logAdd(logs, state.LogWarn, "the hub refused the stored session; signed out")
}

// Deregister frees this install's device slot on the hub; call it before
// Logout. It forgets the identity only once the hub no longer holds it.
func Deregister() error {
	deregGate.Lock()
	defer deregGate.Unlock()
	mu.Lock()
	scope, ident, logs := scopeLocked(), identity, wgLogs
	mu.Unlock()
	if scope.licenseKey == "" {
		return errors.New("mobile: not authenticated")
	}
	if ident == nil {
		return errors.New("mobile: no device identity")
	}

	// Recorded first, so a host that stops waiting and signs out still leaves the slot to retry.
	savePendingDeregister(pendingDeregister{LicenseKey: scope.licenseKey, IdentityPubkey: ident.pubB64})
	err := deregisterDeviceHub(scope, ident.pubB64)
	if slotFreed(err) {
		clearPendingDeregister(ident.pubB64)
		forgetIdentityIf(scope.session, ident.pubB64)
		return nil
	}
	if isSignOutCode(err) {
		clearPendingDeregister(ident.pubB64)
	} else {
		logAdd(logs, state.LogWarn, "could not free this device's slot on sign-out; will retry")
	}
	return err
}

// DeleteAccount deletes the signed-in account on the hub, then signs out here.
// It ends the session only once the hub confirms the deletion.
func DeleteAccount() error {
	scope := captureScope()
	if scope.licenseKey == "" {
		return errors.New("mobile: not authenticated")
	}

	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := hubRequestAs(scope, http.MethodPost, accountDeleteRoute, struct{}{}, &out); err != nil {
		// The route checks the license, so this answer says the session is dead.
		if isSignOutCode(err) {
			signOutLocally(scope)
		}
		return err
	}
	if !out.Deleted {
		return errors.New("mobile: the hub did not confirm the account was deleted")
	}
	endDeletedAccount(scope)
	return nil
}

// endDeletedAccount is Logout for scope's session alone, and drops the identity
// too: the hub deprovisioned its device along with the account.
func endDeletedAccount(scope callScope) {
	storeGate.Lock()
	mu.Lock()
	if sessionEpoch != scope.session {
		mu.Unlock()
		storeGate.Unlock()
		return
	}
	st, memory, logs, pub := store, transportMem, wgLogs, identity.publicKey()
	licenseKey, identity, servers = "", nil, nil
	setPreparedLocked(nil)
	sessionEpoch++
	mu.Unlock()
	if st != nil {
		clearDeletedAccount(st, scope.licenseKey, pub)
	}
	storeGate.Unlock()

	forgetTransportHistory(memory, logs)
	logAdd(logs, state.LogInfo, "deleted the account; signed out")
}

// clearDeletedAccount leaves a session another process stored since alone.
// Call it under storeGate.
func clearDeletedAccount(st SecretStore, deleted, pub string) {
	if stored := strings.TrimSpace(st.Get(keyLicense)); stored != "" && stored != deleted {
		return
	}
	for _, key := range sessionKeys {
		st.Set(key, "")
	}
	if stored, _ := parseIdentity(st.Get(keyIdentity)); stored.publicKey() == pub {
		st.Set(keyIdentity, "")
	}
}

// forgetTransportHistory clears which networks the tunnel got through on.
func forgetTransportHistory(memory *state.TransportMemoryStore, logs *state.LogStore) {
	if memory == nil {
		return
	}
	if err := memory.Clear(); err != nil {
		logAdd(logs, state.LogWarn, fmt.Sprintf("failed to clear transport memory: %v", err))
	}
}

// Wipe forgets everything this install knows: session, identity, settings, hub
// routes, transport history and logs. Local only; it never calls the hub.
func Wipe() error {
	Stop()

	// One step under the gate: a write begun before it either lands first and is
	// cleared here, or sees the new epochs and is dropped.
	storeGate.Lock()
	mu.Lock()
	st, memory, path, logs := store, transportMem, transportMemPath, wgLogs
	licenseKey, identity, servers = "", nil, nil
	setPreparedLocked(nil)
	sessionEpoch++
	wipeEpoch++
	settings = defaultConfig()
	preferredTransport = settings.PreferredTransport
	currentStatus = statusJSON{State: string(state.StateDisconnected)}
	mu.Unlock()
	if st != nil {
		for _, key := range storedKeys {
			st.Set(key, "")
		}
	}
	storeGate.Unlock()

	// After the reset, so a search still running under the old settings is discarded.
	invalidateHubPath()
	dropDoHConnections()

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
