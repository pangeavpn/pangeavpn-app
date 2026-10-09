//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var procRegFlushKey = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegFlushKey")

const (
	wcmSvcKeyPath               = `SOFTWARE\Microsoft\WcmSvc`
	enableBadStateTrackingValue = "EnableBadStateTracking"
	// The marker proves the value is ours: without it the value is an administrator's.
	wcmBadStateMarkerFile = "wcm-badstate-owned"
)

type wcmBadStateValue int

const (
	badStateAbsent wcmBadStateValue = iota
	badStateOff                     // DWORD 0, what a pause writes
	badStateOther
)

// wcmBadStatePolicy is the single registry value a pause owns; tests swap in a fake.
type wcmBadStatePolicy interface {
	read() (wcmBadStateValue, error)
	disable() error
	remove() error
}

var wcmBadStateStore wcmBadStatePolicy = registryBadStatePolicy{}

// Serialises the marker/value read-modify-write across kill switch instances.
var wcmBadStateMu sync.Mutex

// wcmAdminValueNoted keeps an administrator's own value to one log line per process,
// since every arm finds it again. Guarded by wcmBadStateMu.
var wcmAdminValueNoted bool

func wcmBadStateMarkerPath() (string, error) {
	statePath, err := killSwitchStatePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(statePath), wcmBadStateMarkerFile), nil
}

// afterLockArmed pauses tracking once a lock is up: it blocks NCSI's probe on
// the physical NIC, which WCM reads as a bad Wi-Fi to reset or soft-disconnect.
func afterLockArmed(err error) error {
	if err != nil {
		return err
	}
	if pauseErr := pauseWCMBadStateTracking(); pauseErr != nil {
		KillSwitchWarn("kill switch: could not pause WCM bad-state tracking, Windows may reset the Wi-Fi while locked: %v", pauseErr)
	}
	return nil
}

func afterLockCleared(err error) error {
	if err != nil {
		return err
	}
	if restoreErr := restoreWCMBadStateTracking(); restoreErr != nil {
		KillSwitchWarn("kill switch clear: could not restore WCM bad-state tracking: %v", restoreErr)
	}
	return nil
}

// ReleaseOrphanedSettings undoes a pause whose lock went away without a Clear;
// callers use it only once they have found no lock live.
func (ks *windowsKillSwitch) ReleaseOrphanedSettings() error {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.active {
		return nil
	}
	return restoreWCMBadStateTracking()
}

// pauseWCMBadStateTracking sets EnableBadStateTracking=0 only where no value
// exists, recording ownership first so a crash never strands an unowned value.
func pauseWCMBadStateTracking() error {
	wcmBadStateMu.Lock()
	defer wcmBadStateMu.Unlock()

	marker, err := wcmBadStateMarkerPath()
	if err != nil {
		return fmt.Errorf("wcm bad-state marker: %w", err)
	}
	current, err := wcmBadStateStore.read()
	if err != nil {
		return fmt.Errorf("read %s: %w", enableBadStateTrackingValue, err)
	}
	if current != badStateAbsent {
		noteAdministratorsValue(marker, current)
		return nil
	}
	if err := writeWCMBadStateMarker(marker); err != nil {
		return err
	}
	if err := wcmBadStateStore.disable(); err != nil {
		return errors.Join(fmt.Errorf("set %s=0: %w", enableBadStateTrackingValue, err), removeWCMBadStateMarker(marker))
	}
	KillSwitchInfo("kill switch: paused Windows WCM bad-state tracking (%s=0) while the lock blocks its connectivity probe", enableBadStateTrackingValue)
	return nil
}

// noteAdministratorsValue logs, once, a value the pause found and left alone
// because no marker says it is ours. Holds wcmBadStateMu.
func noteAdministratorsValue(marker string, current wcmBadStateValue) {
	if wcmAdminValueNoted {
		return
	}
	if _, err := os.Stat(marker); err == nil {
		return
	}
	wcmAdminValueNoted = true
	effect := "tracking stays on, so Windows may still reset the Wi-Fi while locked"
	if current == badStateOff {
		effect = "tracking is already off"
	}
	KillSwitchInfo("kill switch: left the administrator's own %s value in place; %s", enableBadStateTrackingValue, effect)
}

// restoreWCMBadStateTracking undoes only our own pause, and keeps the marker
// on failure so a later Clear or ReleaseOrphanedSettings can retry.
func restoreWCMBadStateTracking() error {
	wcmBadStateMu.Lock()
	defer wcmBadStateMu.Unlock()

	marker, err := wcmBadStateMarkerPath()
	if err != nil {
		return fmt.Errorf("wcm bad-state marker: %w", err)
	}
	if _, err := os.Stat(marker); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("wcm bad-state marker: %w", err)
	}
	current, err := wcmBadStateStore.read()
	if err != nil {
		return fmt.Errorf("read %s: %w", enableBadStateTrackingValue, err)
	}
	if current == badStateOff {
		if err := wcmBadStateStore.remove(); err != nil {
			return fmt.Errorf("delete %s: %w", enableBadStateTrackingValue, err)
		}
		KillSwitchInfo("kill switch: restored Windows WCM bad-state tracking")
	} else {
		KillSwitchInfo("kill switch: %s was changed while paused; keeping that value", enableBadStateTrackingValue)
	}
	return removeWCMBadStateMarker(marker)
}

func writeWCMBadStateMarker(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create wcm bad-state marker: %w", err)
	}
	_, err = f.WriteString(enableBadStateTrackingValue + "=0 set by PangeaVPN\n")
	if err == nil {
		// Durable before the value exists, or a power cut could orphan the value.
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write wcm bad-state marker: %w", err)
	}
	return nil
}

func removeWCMBadStateMarker(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove wcm bad-state marker: %w", err)
	}
	return nil
}

type registryBadStatePolicy struct{}

func (registryBadStatePolicy) read() (wcmBadStateValue, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, wcmSvcKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return badStateAbsent, nil
	}
	if err != nil {
		return badStateAbsent, err
	}
	defer key.Close()

	value, valueType, err := key.GetIntegerValue(enableBadStateTrackingValue)
	switch {
	case errors.Is(err, registry.ErrNotExist):
		return badStateAbsent, nil
	case errors.Is(err, registry.ErrUnexpectedType):
		return badStateOther, nil
	case err != nil:
		return badStateAbsent, err
	case valueType == registry.DWORD && value == 0:
		return badStateOff, nil
	}
	return badStateOther, nil
}

func (registryBadStatePolicy) disable() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, wcmSvcKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetDWordValue(enableBadStateTrackingValue, 0)
}

func (registryBadStatePolicy) remove() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, wcmSvcKeyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.DeleteValue(enableBadStateTrackingValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	// The hive is written lazily: flush before the caller drops the marker, or a power
	// cut could bring the value back with nothing left to say it is ours.
	if status, _, _ := procRegFlushKey.Call(uintptr(key)); status != 0 {
		return fmt.Errorf("flush %s: %w", wcmSvcKeyPath, syscall.Errno(status))
	}
	return nil
}
