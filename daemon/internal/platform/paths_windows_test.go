//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func mustSid(t *testing.T, wellKnown windows.WELL_KNOWN_SID_TYPE) *windows.SID {
	t.Helper()
	sid, err := windows.CreateWellKnownSid(wellKnown)
	if err != nil {
		t.Fatalf("create sid: %v", err)
	}
	return sid
}

func mustACL(t *testing.T, sids ...*windows.SID) *windows.ACL {
	t.Helper()
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(sids))
	for _, sid := range sids {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.ACCESS_MASK(windows.GENERIC_ALL),
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatalf("build acl: %v", err)
	}
	return acl
}

func TestClassifyDirSecurity(t *testing.T) {
	system := mustSid(t, windows.WinLocalSystemSid)
	admins := mustSid(t, windows.WinBuiltinAdministratorsSid)
	users := mustSid(t, windows.WinBuiltinUsersSid)

	cases := []struct {
		name        string
		owner       *windows.SID
		dacl        *windows.ACL
		daclPresent bool
		want        dirSecurityVerdict
	}{
		{"locked down", system, mustACL(t, system, admins), true, dirSecurityOK},
		{"inherited users ace", system, mustACL(t, system, admins, users), true, dirSecurityRepairable},
		{"admin owner missing dacl", admins, nil, false, dirSecurityRepairable},
		{"user owned", users, mustACL(t, system, admins), true, dirSecurityUntrusted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := classifyDirSecurity(tc.owner, tc.dacl, tc.daclPresent)
			if got != tc.want {
				t.Fatalf("verdict = %v (%v), want %v", got, reason, tc.want)
			}
			if got != dirSecurityOK && reason == nil {
				t.Fatal("expected a reason for a non-OK verdict")
			}
		})
	}
}

// Reproduces an install whose state dir predates the lockdown: created by an
// elevated process, so admin-owned, but still inheriting the parent's ACEs.
func TestEnsureDataDirRepairsInheritedACL(t *testing.T) {
	if !isPrivilegedProcess() {
		t.Skip("requires an elevated process")
	}

	dir := filepath.Join(t.TempDir(), "PangeaVPN")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if verdict, reason := inspectDirSecurity(dir); verdict != dirSecurityRepairable {
		t.Fatalf("precondition: verdict = %v (%v), want repairable", verdict, reason)
	}

	if err := ensureDataDir(dir); err != nil {
		t.Fatalf("ensureDataDir: %v", err)
	}
	if verdict, reason := inspectDirSecurity(dir); verdict != dirSecurityOK {
		t.Fatalf("after repair: verdict = %v (%v), want OK", verdict, reason)
	}
}

// A fresh state dir must be assignable by an elevated admin, not only SYSTEM.
func TestEnsureDataDirCreatesLockedDownDir(t *testing.T) {
	if !isPrivilegedProcess() {
		t.Skip("requires an elevated process")
	}

	dir := filepath.Join(t.TempDir(), "PangeaVPN")
	if err := ensureDataDir(dir); err != nil {
		t.Fatalf("ensureDataDir: %v", err)
	}
	if verdict, reason := inspectDirSecurity(dir); verdict != dirSecurityOK {
		t.Fatalf("verdict = %v (%v), want OK", verdict, reason)
	}
}

func TestEnsureDataDirHonoursOverrideWhenUnprivileged(t *testing.T) {
	if isPrivilegedProcess() {
		t.Skip("requires an unprivileged process")
	}

	t.Setenv(appSupportDirOverrideEnv, t.TempDir())
	dir := filepath.Join(t.TempDir(), "logs")
	if err := ensureDataDir(dir); err != nil {
		t.Fatalf("ensureDataDir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("stat %s: %v", dir, err)
	}
}
