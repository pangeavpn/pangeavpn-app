//go:build android || ios

package mobile

// Signing in: the token login, this install's device registration, and the
// one step that makes the new session the stored one.

import "errors"

// Login performs a token login through the secure channel, generating and
// registering a device identity on first use, and returns the Session JSON.
func Login(token string) (string, error) {
	mu.Lock()
	ident, st, scope := identity, store, scopeLocked()
	mu.Unlock()
	if st == nil {
		return "", errors.New("mobile: not initialized")
	}

	if ident == nil {
		fresh, err := generateIdentity()
		if err != nil {
			return "", err
		}
		if !storeIdentityFor(scope, fresh) {
			return "", errSessionEnded
		}
		ident = fresh
	}

	resp, err := tokenLogin(scope.wipe, token, ident.pubB64)
	if err != nil {
		return "", err
	}
	scope, ok := beginSession(scope, resp.VpnAccessToken, ident)
	if !ok {
		return "", errSessionEnded
	}

	ident, err = registerThisDevice(scope, ident)
	if err != nil {
		// Memory keeps the key, so a device-limit login can still free a slot.
		storeForSession(scope, keyLicense, "")
		return "", err
	}

	// The login answer can carry another device's Cloak UID, so refetch for
	// this identity; the login list stands in, never cached, if that fails.
	srvs, err := fetchServers(scope, ident.pubB64)
	if errors.Is(err, errSessionEnded) {
		return "", err
	}
	if err != nil || len(srvs) == 0 {
		srvs = resp.Servers
	}
	if !commitSession(scope, ident, srvs) {
		return "", errSessionEnded
	}
	return marshalSession(sessionJSON{Email: resp.User.Email, Name: resp.User.Name, Servers: publicServers(srvs)})
}

// beginSession signs lk in for ident unless the session moved on during the
// token login, returning the new session's scope.
func beginSession(scope callScope, lk string, ident *identityKeyPair) (callScope, bool) {
	mu.Lock()
	defer mu.Unlock()
	if sessionEpoch != scope.session {
		return callScope{}, false
	}
	licenseKey, identity, servers = lk, ident, nil
	setPreparedLocked(nil)
	sessionEpoch++
	return scopeLocked(), true
}

// commitSession stores the license beside the identity it registered, both
// under the gate, so a Reload never reads one without the other.
func commitSession(scope callScope, ident *identityKeyPair, srvs []serverInfo) bool {
	storeGate.Lock()
	defer storeGate.Unlock()
	mu.Lock()
	if sessionEpoch != scope.session {
		mu.Unlock()
		return false
	}
	identity, servers = ident, srvs
	s := store
	mu.Unlock()
	if s != nil {
		s.Set(keyIdentity, ident.marshal())
		s.Set(keyLicense, scope.licenseKey)
	}
	return true
}

// registerThisDevice registers ident, rotating it once when it is still
// registered to another account, as after signing out offline and switching.
func registerThisDevice(scope callScope, ident *identityKeyPair) (*identityKeyPair, error) {
	_, err := registerDevice(scope, ident.pubB64, platformName)
	if hubErrorCode(err) != "IDENTITY_CONFLICT" {
		return ident, err
	}
	fresh, genErr := generateIdentity()
	if genErr != nil {
		return ident, err
	}
	if !storeIdentityFor(scope, fresh) {
		return ident, errSessionEnded
	}
	_, err = registerDevice(scope, fresh.pubB64, platformName)
	return fresh, err
}
