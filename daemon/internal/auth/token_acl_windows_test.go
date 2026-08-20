//go:build windows

package auth

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func mustTokenSid(t *testing.T, wellKnown windows.WELL_KNOWN_SID_TYPE) *windows.SID {
	t.Helper()
	sid, err := windows.CreateWellKnownSid(wellKnown)
	if err != nil {
		t.Fatalf("create sid: %v", err)
	}
	return sid
}

func mustTokenACL(t *testing.T, entries ...windows.EXPLICIT_ACCESS) *windows.ACL {
	t.Helper()
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatalf("build acl: %v", err)
	}
	return acl
}

// The interactive read grant is what lets the desktop read the token at all.
// Rejecting it made every boot regenerate the token, rotating it out from
// under a desktop that had already read the old one.
func TestDaclGrantsNonAdminWrite(t *testing.T) {
	system := mustTokenSid(t, windows.WinLocalSystemSid)
	admins := mustTokenSid(t, windows.WinBuiltinAdministratorsSid)
	interactive := mustTokenSid(t, windows.WinInteractiveSid)
	users := mustTokenSid(t, windows.WinBuiltinUsersSid)

	cases := []struct {
		name    string
		entries []windows.EXPLICIT_ACCESS
		wantErr bool
	}{
		{
			name: "the acl the daemon itself writes",
			entries: []windows.EXPLICIT_ACCESS{
				tokenAccessEntry(system, windows.GENERIC_ALL),
				tokenAccessEntry(admins, windows.GENERIC_ALL),
				tokenAccessEntry(interactive, windows.FILE_GENERIC_READ),
			},
		},
		{
			name: "admins only",
			entries: []windows.EXPLICIT_ACCESS{
				tokenAccessEntry(system, windows.GENERIC_ALL),
				tokenAccessEntry(admins, windows.GENERIC_ALL),
			},
		},
		{
			name: "a non-admin write grant",
			entries: []windows.EXPLICIT_ACCESS{
				tokenAccessEntry(system, windows.GENERIC_ALL),
				tokenAccessEntry(users, windows.FILE_GENERIC_WRITE),
			},
			wantErr: true,
		},
		{
			name: "a non-admin grant that can rewrite the DACL",
			entries: []windows.EXPLICIT_ACCESS{
				tokenAccessEntry(system, windows.GENERIC_ALL),
				tokenAccessEntry(users, windows.WRITE_DAC),
			},
			wantErr: true,
		},
		{
			name: "a non-admin delete grant",
			entries: []windows.EXPLICIT_ACCESS{
				tokenAccessEntry(system, windows.GENERIC_ALL),
				tokenAccessEntry(users, windows.DELETE),
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := daclGrantsNonAdminWrite(mustTokenACL(t, tc.entries...))
			if tc.wantErr && err == nil {
				t.Fatal("expected the ACL to be refused")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ACL refused: %v", err)
			}
		})
	}
}

// End to end over a real file: what ensureTokenReadACL writes must survive the
// scan, or the daemon refuses its own token on the next boot.
func TestEnsureTokenReadACLSurvivesItsOwnCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon-token.txt")
	if err := os.WriteFile(path, []byte("0123456789abcdef"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	if err := ensureTokenReadACL(path); err != nil {
		t.Fatalf("ensureTokenReadACL: %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read security info: %v", err)
	}
	dacl, present, err := readTokenDACL(sd)
	if err != nil || !present {
		t.Fatalf("read DACL: present=%v err=%v", present, err)
	}

	if err := daclGrantsNonAdminWrite(dacl); err != nil {
		t.Fatalf("the daemon's own ACL was refused: %v", err)
	}
}
