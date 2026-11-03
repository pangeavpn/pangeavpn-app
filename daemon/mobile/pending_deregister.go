//go:build android || ios

package mobile

// A sign-out whose deregistration never reached the hub leaves the device in
// the account's list, so the slot is remembered and freed on a later try.

import (
	"encoding/json"
	"sync"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const keyPendingDeregister = "pendingDeregister"

// deregGate keeps deregistration and sign-in apart, so neither deletes the
// registration the other just made for a reused identity.
var deregGate sync.Mutex

type pendingDeregister struct {
	LicenseKey     string `json:"licenseKey"`
	IdentityPubkey string `json:"identityPubkey"`
}

// slotFreed means the hub no longer lists the device under this account.
func slotFreed(err error) bool {
	switch hubErrorCode(err) {
	case "DEVICE_NOT_FOUND", "FORBIDDEN":
		return true
	}
	return err == nil
}

func loadPendingDeregister() *pendingDeregister {
	mu.Lock()
	st := store
	mu.Unlock()
	if st == nil {
		return nil
	}
	var p pendingDeregister
	if json.Unmarshal([]byte(st.Get(keyPendingDeregister)), &p) != nil || p.LicenseKey == "" || p.IdentityPubkey == "" {
		return nil
	}
	return &p
}

func savePendingDeregister(p pendingDeregister) {
	raw, err := json.Marshal(p)
	if err != nil {
		return
	}
	storeGate.Lock()
	defer storeGate.Unlock()
	mu.Lock()
	st := store
	mu.Unlock()
	if st != nil {
		st.Set(keyPendingDeregister, string(raw))
	}
}

// clearPendingDeregister drops the record only while it still names pub.
func clearPendingDeregister(pub string) {
	storeGate.Lock()
	defer storeGate.Unlock()
	mu.Lock()
	st := store
	mu.Unlock()
	if st == nil {
		return
	}
	var p pendingDeregister
	if json.Unmarshal([]byte(st.Get(keyPendingDeregister)), &p) == nil && p.IdentityPubkey == pub {
		st.Set(keyPendingDeregister, "")
	}
}

// RetryPendingDeregister frees a slot an earlier sign-out could not. It skips
// while a sign-in runs, since Login settles the slot itself.
func RetryPendingDeregister() error {
	if !deregGate.TryLock() {
		return nil
	}
	defer deregGate.Unlock()
	return retryPendingDeregisterLocked()
}

// retryPendingDeregisterLocked needs deregGate held.
func retryPendingDeregisterLocked() error {
	p := loadPendingDeregister()
	if p == nil {
		return nil
	}
	scope := captureScope()
	scope.licenseKey = p.LicenseKey
	mu.Lock()
	logs := wgLogs
	mu.Unlock()

	err := deregisterDeviceHub(scope, p.IdentityPubkey)
	switch {
	case slotFreed(err):
		forgetIdentityIf(scope.session, p.IdentityPubkey)
		logAdd(logs, state.LogInfo, "freed the device slot an earlier sign-out left behind")
	case isSignOutCode(err):
		logAdd(logs, state.LogWarn, "the hub refused the signed-out account's key; its device slot is left to the account")
	default:
		logAdd(logs, state.LogWarn, "freeing the device slot an earlier sign-out left behind failed; will retry")
		return err
	}
	clearPendingDeregister(p.IdentityPubkey)
	return nil
}
