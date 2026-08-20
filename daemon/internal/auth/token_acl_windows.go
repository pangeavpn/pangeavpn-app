//go:build windows

package auth

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ensureTokenReadACL replaces the token file's DACL with an explicit grant to
// SYSTEM, Administrators, and interactively logged-on users only, dropping
// any inherited entries (e.g. the broad access a parent directory grants).
func ensureTokenReadACL(path string) error {
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("resolve SYSTEM sid: %w", err)
	}
	adminsSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("resolve Administrators sid: %w", err)
	}
	interactiveSID, err := windows.CreateWellKnownSid(windows.WinInteractiveSid)
	if err != nil {
		return fmt.Errorf("resolve Interactive sid: %w", err)
	}

	entries := []windows.EXPLICIT_ACCESS{
		tokenAccessEntry(systemSID, windows.GENERIC_ALL),
		tokenAccessEntry(adminsSID, windows.GENERIC_ALL),
		tokenAccessEntry(interactiveSID, windows.FILE_GENERIC_READ),
	}

	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("build token ACL: %w", err)
	}

	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	); err != nil {
		return fmt.Errorf("set token ACL: %w", err)
	}
	return nil
}

func tokenAccessEntry(sid *windows.SID, mask windows.ACCESS_MASK) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: mask,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}

// readTrustedTokenFile refuses to adopt a token file unless it is owned by
// SYSTEM/Administrators and grants no non-admin principal write access, so a
// planted file in an attacker-writable directory is never trusted.
func readTrustedTokenFile(path string) (string, error) {
	if err := verifyTrustedTokenOwner(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	return string(data), nil
}

func verifyTrustedTokenOwner(path string) error {
	namePtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode token path: %w", err)
	}
	if attrs, err := windows.GetFileAttributes(namePtr); err != nil {
		return fmt.Errorf("stat token file: %w", err)
	} else if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("token file is a reparse point")
	}

	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read token file security descriptor: %w", err)
	}

	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read token file owner: %w", err)
	}
	if !isTrustedTokenPrincipal(owner) {
		return fmt.Errorf("token file is not owned by SYSTEM or Administrators")
	}

	dacl, present, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read token file DACL: %w", err)
	}
	if !present || dacl == nil {
		return fmt.Errorf("token file has no DACL")
	}

	const writeMask = windows.FILE_GENERIC_WRITE | windows.FILE_WRITE_DATA |
		windows.GENERIC_WRITE | windows.GENERIC_ALL | windows.WRITE_DAC | windows.WRITE_OWNER

	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("read token file ACE %d: %w", i, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if isTrustedTokenPrincipal(sid) {
			continue
		}
		if ace.Mask&writeMask != 0 {
			return fmt.Errorf("token file grants write access to a non-admin principal")
		}
	}

	return nil
}

func isTrustedTokenPrincipal(sid *windows.SID) bool {
	if systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid); err == nil && sid.Equals(systemSID) {
		return true
	}
	if adminsSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid); err == nil && sid.Equals(adminsSID) {
		return true
	}
	return false
}
