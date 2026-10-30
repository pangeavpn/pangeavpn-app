//go:build android || ios

package mobile

// Epochs let work that outlives a sign-out or a wipe find out, so its late
// answer never lands in memory or in the store.

import (
	"errors"
	"sync"
)

var errSessionEnded = errors.New("mobile: session ended")

// storeGate orders epoch-checked writes against the clears that move an epoch,
// so a write lands either before the clear or not at all. Take it before mu.
var storeGate sync.Mutex

// callScope is what a call captured before reaching the hub: the license it
// sends and the epochs that must not move before its answer is used.
type callScope struct {
	licenseKey string
	session    uint64
	wipe       uint64
}

func captureScope() callScope {
	mu.Lock()
	defer mu.Unlock()
	return scopeLocked()
}

// scopeLocked is captureScope for a caller already holding mu.
func scopeLocked() callScope {
	return callScope{licenseKey: licenseKey, session: sessionEpoch, wipe: wipeEpoch}
}

// sessionEnded reports whether the session this scope captured is over.
func (c callScope) sessionEnded() bool {
	mu.Lock()
	defer mu.Unlock()
	return sessionEpoch != c.session
}

func currentWipeEpoch() uint64 {
	mu.Lock()
	defer mu.Unlock()
	return wipeEpoch
}

// setStoredSince writes a hub-route cache only while no Wipe has run since
// wipe was read.
func setStoredSince(wipe uint64, key, value string) {
	storeGate.Lock()
	defer storeGate.Unlock()
	mu.Lock()
	s, current := store, wipeEpoch
	mu.Unlock()
	if s != nil && current == wipe {
		s.Set(key, value)
	}
}

// storeForSession writes only while the scope's session is still the signed-in
// one, so an answer that lands after a sign-out cannot bring its cache back.
func storeForSession(scope callScope, key, value string) {
	storeGate.Lock()
	defer storeGate.Unlock()
	mu.Lock()
	s, current := store, sessionEpoch
	mu.Unlock()
	if s != nil && current == scope.session {
		s.Set(key, value)
	}
}

// storeIdentityFor installs ident in memory and the store unless the session
// moved on since scope was captured.
func storeIdentityFor(scope callScope, ident *identityKeyPair) bool {
	storeGate.Lock()
	defer storeGate.Unlock()
	mu.Lock()
	if sessionEpoch != scope.session {
		mu.Unlock()
		return false
	}
	s := store
	identity = ident
	mu.Unlock()
	if s != nil {
		s.Set(keyIdentity, ident.marshal())
	}
	return true
}
