//go:build windows && (amd64 || arm64)

package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

type fakeBadStatePolicy struct {
	present bool
	value   uint32

	readErr    error
	disableErr error
	removeErr  error

	disables int
	removes  int
}

func (f *fakeBadStatePolicy) read() (wcmBadStateValue, error) {
	switch {
	case f.readErr != nil:
		return badStateAbsent, f.readErr
	case !f.present:
		return badStateAbsent, nil
	case f.value == 0:
		return badStateOff, nil
	}
	return badStateOther, nil
}

func (f *fakeBadStatePolicy) disable() error {
	f.disables++
	if f.disableErr != nil {
		return f.disableErr
	}
	f.present, f.value = true, 0
	return nil
}

func (f *fakeBadStatePolicy) remove() error {
	f.removes++
	if f.removeErr != nil {
		return f.removeErr
	}
	f.present, f.value = false, 0
	return nil
}

// useFakeBadStatePolicy keeps every test off the real HKLM value and the real
// ProgramData marker; it returns where the marker lives for this test.
func useFakeBadStatePolicy(t *testing.T, f *fakeBadStatePolicy) string {
	t.Helper()
	isolateStateDir(t)
	previous := wcmBadStateStore
	wcmBadStateStore = f
	t.Cleanup(func() { wcmBadStateStore = previous })
	marker, err := wcmBadStateMarkerPath()
	if err != nil {
		t.Fatalf("marker path: %v", err)
	}
	return marker
}

func markerExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}
	return false
}

func writeMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("owned\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

// failWFPOpen stops a test from ever reaching the real Base Filtering Engine.
func failWFPOpen(t *testing.T) {
	t.Helper()
	previous := openWFPEngine
	openWFPEngine = func() (*wfpEngine, error) { return nil, errors.New("BFE unavailable") }
	t.Cleanup(func() { openWFPEngine = previous })
}

func captureKillSwitchWarnings(t *testing.T) *[]string {
	t.Helper()
	var warnings []string
	previous := KillSwitchWarnf
	KillSwitchWarnf = func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { KillSwitchWarnf = previous })
	return &warnings
}

func TestWCMBadStateMarkerSharesKillSwitchStateDir(t *testing.T) {
	marker := useFakeBadStatePolicy(t, &fakeBadStatePolicy{})
	statePath, err := killSwitchStatePath()
	if err != nil {
		t.Fatalf("state path: %v", err)
	}
	if filepath.Dir(marker) != filepath.Dir(statePath) {
		t.Fatalf("marker dir = %q, want the kill switch state dir %q", filepath.Dir(marker), filepath.Dir(statePath))
	}
}

func TestPauseWCMBadStateTracking_AbsentTakesOwnership(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)

	if err := pauseWCMBadStateTracking(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if !policy.present || policy.value != 0 {
		t.Fatalf("value = (present %v, %d), want present 0", policy.present, policy.value)
	}
	if !markerExists(t, marker) {
		t.Fatal("marker missing after taking ownership; restore would never undo the value")
	}
}

func TestPauseWCMBadStateTracking_RepeatedIsNoop(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)

	for i := 0; i < 3; i++ {
		if err := pauseWCMBadStateTracking(); err != nil {
			t.Fatalf("pause %d: %v", i, err)
		}
	}
	if policy.disables != 1 {
		t.Fatalf("disable called %d times, want 1", policy.disables)
	}
	if !markerExists(t, marker) {
		t.Fatal("marker missing after repeated pause")
	}
}

func TestWCMBadStateTracking_AdminValueUntouched(t *testing.T) {
	for _, value := range []uint32{0, 1} {
		policy := &fakeBadStatePolicy{present: true, value: value}
		marker := useFakeBadStatePolicy(t, policy)

		if err := pauseWCMBadStateTracking(); err != nil {
			t.Fatalf("value %d: pause: %v", value, err)
		}
		if markerExists(t, marker) {
			t.Fatalf("value %d: pause claimed an administrator's value", value)
		}
		if err := restoreWCMBadStateTracking(); err != nil {
			t.Fatalf("value %d: restore: %v", value, err)
		}
		if policy.disables != 0 || policy.removes != 0 {
			t.Fatalf("value %d: disables=%d removes=%d, want the admin value untouched", value, policy.disables, policy.removes)
		}
		if !policy.present || policy.value != value {
			t.Fatalf("value %d: now (present %v, %d)", value, policy.present, policy.value)
		}
	}
}

func TestRestoreWCMBadStateTracking_RemovesOurs(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)

	if err := pauseWCMBadStateTracking(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := restoreWCMBadStateTracking(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if policy.present {
		t.Fatalf("value still present (%d) after restoring our own pause", policy.value)
	}
	if markerExists(t, marker) {
		t.Fatal("marker left behind after restore")
	}
}

func TestRestoreWCMBadStateTracking_AdminChangedKeepsValue(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)

	if err := pauseWCMBadStateTracking(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	policy.value = 1
	if err := restoreWCMBadStateTracking(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if policy.removes != 0 || !policy.present || policy.value != 1 {
		t.Fatalf("admin's 1 not kept: removes=%d present=%v value=%d", policy.removes, policy.present, policy.value)
	}
	if markerExists(t, marker) {
		t.Fatal("marker left behind; a later pause would treat the admin's value as ours")
	}
}

func TestRestoreWCMBadStateTracking_MarkerWithoutValue(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)
	writeMarker(t, marker)

	if err := restoreWCMBadStateTracking(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if policy.removes != 0 {
		t.Fatalf("remove called %d times with no value present", policy.removes)
	}
	if markerExists(t, marker) {
		t.Fatal("marker left behind")
	}
}

func TestRestoreWCMBadStateTracking_NoMarkerIsNoop(t *testing.T) {
	policy := &fakeBadStatePolicy{readErr: errors.New("must not be read")}
	useFakeBadStatePolicy(t, policy)

	if err := restoreWCMBadStateTracking(); err != nil {
		t.Fatalf("restore without a marker: %v", err)
	}
	if policy.removes != 0 {
		t.Fatalf("remove called %d times without ownership", policy.removes)
	}
}

// A crash between the marker write and the value create leaves only the marker.
func TestPauseWCMBadStateTracking_CompletesInterruptedPause(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)
	writeMarker(t, marker)

	if err := pauseWCMBadStateTracking(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if !policy.present || policy.value != 0 || !markerExists(t, marker) {
		t.Fatalf("interrupted pause not completed: present=%v value=%d", policy.present, policy.value)
	}
}

func TestPauseWCMBadStateTracking_Failures(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		policy := &fakeBadStatePolicy{readErr: errors.New("access denied")}
		marker := useFakeBadStatePolicy(t, policy)
		if err := pauseWCMBadStateTracking(); err == nil {
			t.Fatal("pause succeeded despite a read failure")
		}
		if policy.disables != 0 || markerExists(t, marker) {
			t.Fatalf("read failure still wrote: disables=%d", policy.disables)
		}
	})
	t.Run("disable", func(t *testing.T) {
		policy := &fakeBadStatePolicy{disableErr: errors.New("access denied")}
		marker := useFakeBadStatePolicy(t, policy)
		if err := pauseWCMBadStateTracking(); err == nil {
			t.Fatal("pause succeeded despite a write failure")
		}
		if markerExists(t, marker) {
			t.Fatal("marker left behind for a value that was never written")
		}
	})
	t.Run("state dir", func(t *testing.T) {
		policy := &fakeBadStatePolicy{}
		useFakeBadStatePolicy(t, policy)
		killSwitchStatePathFn = func() (string, error) { return "", errors.New("no state dir") }
		if err := pauseWCMBadStateTracking(); err == nil {
			t.Fatal("pause succeeded without anywhere to record ownership")
		}
		if policy.disables != 0 {
			t.Fatal("value written with no marker to prove it is ours")
		}
	})
}

// Each case fails before the lock could exist, and before any real WFP call.
func TestWindowsKillSwitchFailedEnableLeavesWCMAlone(t *testing.T) {
	cases := map[string]struct {
		hosts []string
		setup func(t *testing.T)
	}{
		"endpoint resolution": {
			hosts: []string{"blocked.test"},
			setup: func(t *testing.T) {
				originalLookup := lookupResolverIP
				lookupResolverIP = func(context.Context, string, string) ([]net.IP, error) {
					return nil, errors.New("dns blocked by kill switch")
				}
				t.Cleanup(func() { lookupResolverIP = originalLookup })
			},
		},
		"engine open": {
			hosts: []string{"198.51.100.9"},
			setup: failWFPOpen,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			policy := &fakeBadStatePolicy{}
			marker := useFakeBadStatePolicy(t, policy)
			tc.setup(t)

			if err := (&windowsKillSwitch{}).Enable(context.Background(), tc.hosts, false, false); err == nil {
				t.Fatal("Enable succeeded with no lock installed")
			}
			if policy.disables != 0 || markerExists(t, marker) {
				t.Fatalf("a failed Enable paused WCM bad-state tracking (disables=%d)", policy.disables)
			}
		})
	}
}

func TestWindowsKillSwitchFailedClearKeepsWCMPaused(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)
	if err := pauseWCMBadStateTracking(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	failWFPOpen(t)

	if err := (&windowsKillSwitch{}).Clear(context.Background()); err == nil {
		t.Fatal("Clear succeeded with no engine to remove the lock through")
	}
	if policy.removes != 0 || !policy.present || !markerExists(t, marker) {
		t.Fatalf("a failed Clear restored WCM bad-state tracking under a live lock (removes=%d)", policy.removes)
	}
}

func TestAfterLockArmed_PausesOnlyOnceTheLockIsUp(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)

	armErr := errors.New("install failed")
	if err := afterLockArmed(armErr); !errors.Is(err, armErr) {
		t.Fatalf("afterLockArmed(failure) = %v, want the arm error back", err)
	}
	if policy.disables != 0 || markerExists(t, marker) {
		t.Fatalf("a failed arm paused WCM bad-state tracking (disables=%d)", policy.disables)
	}
	if err := afterLockArmed(nil); err != nil {
		t.Fatalf("afterLockArmed(nil) = %v", err)
	}
	if policy.disables != 1 || !markerExists(t, marker) {
		t.Fatalf("an armed lock left WCM bad-state tracking on (disables=%d)", policy.disables)
	}
}

func TestAfterLockArmed_PauseFailureDoesNotFailEnable(t *testing.T) {
	useFakeBadStatePolicy(t, &fakeBadStatePolicy{disableErr: errors.New("access denied")})
	warnings := captureKillSwitchWarnings(t)

	if err := afterLockArmed(nil); err != nil {
		t.Fatalf("afterLockArmed = %v, want nil: the lock is up whatever WCM does", err)
	}
	if len(*warnings) != 1 {
		t.Fatalf("warnings = %q, want the failed pause reported once", *warnings)
	}
}

func TestAfterLockCleared_RestoresOnlyOnceTheLockIsDown(t *testing.T) {
	policy := &fakeBadStatePolicy{}
	marker := useFakeBadStatePolicy(t, policy)
	if err := pauseWCMBadStateTracking(); err != nil {
		t.Fatalf("pause: %v", err)
	}

	clearErr := errors.New("partial teardown")
	if err := afterLockCleared(clearErr); !errors.Is(err, clearErr) {
		t.Fatalf("afterLockCleared(failure) = %v, want the clear error back", err)
	}
	if policy.removes != 0 || !policy.present || !markerExists(t, marker) {
		t.Fatalf("a failed Clear restored WCM bad-state tracking (removes=%d)", policy.removes)
	}
	if err := afterLockCleared(nil); err != nil {
		t.Fatalf("afterLockCleared(nil) = %v", err)
	}
	if policy.present || markerExists(t, marker) {
		t.Fatalf("a cleared lock left our pause behind (present=%v)", policy.present)
	}
}

func TestAfterLockCleared_RestoreFailureDoesNotFailClear(t *testing.T) {
	policy := &fakeBadStatePolicy{present: true, removeErr: errors.New("access denied")}
	marker := useFakeBadStatePolicy(t, policy)
	writeMarker(t, marker)
	warnings := captureKillSwitchWarnings(t)

	if err := afterLockCleared(nil); err != nil {
		t.Fatalf("afterLockCleared = %v, want nil: the lock is already down", err)
	}
	if len(*warnings) != 1 {
		t.Fatalf("warnings = %q, want the failed restore reported once", *warnings)
	}
	if !markerExists(t, marker) {
		t.Fatal("marker dropped; nothing could restore our value later")
	}
}

func TestWindowsKillSwitchReleaseOrphanedSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		armed       bool
		wantRestore bool
	}{
		"no lock": {armed: false, wantRestore: true},
		"armed":   {armed: true, wantRestore: false},
	} {
		t.Run(name, func(t *testing.T) {
			policy := &fakeBadStatePolicy{}
			marker := useFakeBadStatePolicy(t, policy)
			if err := pauseWCMBadStateTracking(); err != nil {
				t.Fatalf("pause: %v", err)
			}

			if err := (&windowsKillSwitch{active: tc.armed}).ReleaseOrphanedSettings(); err != nil {
				t.Fatalf("ReleaseOrphanedSettings: %v", err)
			}
			restored := !policy.present && !markerExists(t, marker)
			if restored != tc.wantRestore {
				t.Fatalf("restored = %v, want %v (removes=%d)", restored, tc.wantRestore, policy.removes)
			}
		})
	}
}

func TestRestoreWCMBadStateTracking_FailuresKeepMarker(t *testing.T) {
	for name, policy := range map[string]*fakeBadStatePolicy{
		"read":   {present: true, readErr: errors.New("access denied")},
		"remove": {present: true, removeErr: errors.New("access denied")},
	} {
		t.Run(name, func(t *testing.T) {
			marker := useFakeBadStatePolicy(t, policy)
			writeMarker(t, marker)
			if err := restoreWCMBadStateTracking(); err == nil {
				t.Fatal("restore succeeded despite a registry failure")
			}
			if !markerExists(t, marker) {
				t.Fatal("marker dropped; a retry could no longer restore our value")
			}
		})
	}
}
