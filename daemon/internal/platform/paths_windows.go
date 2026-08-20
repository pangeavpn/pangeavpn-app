//go:build windows

package platform

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const appFolder = "PangeaVPN"

// appSupportDirOverrideEnv redirects the daemon's state directory. Present on
// every platform so a test run can never touch a real installation's files —
// without it, `go test ./internal/platform` on a machine with PangeaVPN
// installed rewrites and deletes the live killswitch-state.json, and destroying
// its Locked record makes the next daemon start clear an engaged Lockdown lock
// as stale crash leftover.
const appSupportDirOverrideEnv = "PANGEA_APP_SUPPORT_DIR"

func AppSupportDir() (string, error) {
	privileged := isPrivilegedProcess()

	// The override only serves unprivileged dev/test runs; honouring it for a
	// SYSTEM service would let anything that can set its environment relocate
	// the token file and killswitch state to an attacker-chosen directory.
	if override := strings.TrimSpace(os.Getenv(appSupportDirOverrideEnv)); override != "" && !privileged {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("%s must be an absolute path", appSupportDirOverrideEnv)
		}
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", fmt.Errorf("create app support dir override: %w", err)
		}
		return override, nil
	}

	baseDir := strings.TrimSpace(os.Getenv("ProgramData"))
	if baseDir == "" {
		systemDrive := strings.TrimSpace(os.Getenv("SystemDrive"))
		if systemDrive == "" {
			systemDrive = `C:`
		}
		baseDir = systemDrive + `\ProgramData`
	}

	appDir := filepath.Join(baseDir, appFolder)
	if err := ensureDataDir(appDir); err != nil {
		return "", fmt.Errorf("create app support dir: %w", err)
	}

	return appDir, nil
}

func appSupportDirOverridden() bool {
	return strings.TrimSpace(os.Getenv(appSupportDirOverrideEnv)) != ""
}

func TokenPath() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "daemon-token.txt"), nil
}

func ConfigPath() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "config.json"), nil
}

func TunnelConfigDir() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}

	// The wireguard folder just inherits appDir's locked-down ACL; only the
	// leaf directory needs its own explicit create-or-verify pass.
	wgDir := filepath.Join(appDir, "wireguard")
	if err := os.Mkdir(wgDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create wireguard dir: %w", err)
	}

	dir := filepath.Join(wgDir, "tunnels")
	if err := ensureDataDir(dir); err != nil {
		return "", fmt.Errorf("create tunnel config dir: %w", err)
	}

	return dir, nil
}

// isPrivilegedProcess reports whether the current process runs as SYSTEM or
// an elevated administrator, the two identities the daemon's state dir trusts.
func isPrivilegedProcess() bool {
	token := windows.GetCurrentProcessToken()
	if token.IsElevated() {
		return true
	}

	user, err := token.GetTokenUser()
	if err != nil {
		return false
	}
	systemSid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false
	}
	return user.User.Sid.Equals(systemSid)
}

// ensureDataDir creates path with a DACL restricted to SYSTEM and
// Administrators, or, if it already exists, verifies it still is — refusing
// to trust a pre-existing directory an unprivileged user could have planted.
func ensureDataDir(path string) error {
	// An unprivileged dev/test run redirected by the override owns nothing
	// SYSTEM-grade; the admin-only regime would only lock it out of its own dir.
	if appSupportDirOverridden() && !isPrivilegedProcess() {
		return os.MkdirAll(path, 0o700)
	}

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return createSecureDir(path)
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink or junction, refusing to use it", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return verifySecureDir(path)
}

func createSecureDir(path string) error {
	sd, err := adminOnlySecurityDescriptor()
	if err != nil {
		return fmt.Errorf("build security descriptor: %w", err)
	}

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode path: %w", err)
	}

	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	if err := windows.CreateDirectory(pathPtr, sa); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return nil
}

// adminOnlyDACL grants SYSTEM and Administrators full control, inheritable by
// children, and nobody else.
func adminOnlyDACL() (*windows.ACL, error) {
	systemSid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, fmt.Errorf("resolve SYSTEM sid: %w", err)
	}
	adminSid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, fmt.Errorf("resolve Administrators sid: %w", err)
	}

	inherit := uint32(windows.CONTAINER_INHERIT_ACE | windows.OBJECT_INHERIT_ACE)
	access := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.ACCESS_MASK(windows.GENERIC_ALL),
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inherit,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(systemSid),
			},
		},
		{
			AccessPermissions: windows.ACCESS_MASK(windows.GENERIC_ALL),
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inherit,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(adminSid),
			},
		},
	}

	dacl, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return nil, fmt.Errorf("build dacl: %w", err)
	}
	return dacl, nil
}

// adminOnlySecurityDescriptor wraps that DACL in a protected security
// descriptor owned by SYSTEM.
func adminOnlySecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	dacl, err := adminOnlyDACL()
	if err != nil {
		return nil, err
	}

	adminSid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, fmt.Errorf("resolve Administrators sid: %w", err)
	}

	sd, err := windows.NewSecurityDescriptor()
	if err != nil {
		return nil, fmt.Errorf("new security descriptor: %w", err)
	}
	if err := sd.SetDACL(dacl, true, false); err != nil {
		return nil, fmt.Errorf("set dacl: %w", err)
	}
	// Administrators is the one trusted owner both SYSTEM and an elevated
	// admin can assign; SYSTEM needs SeRestorePrivilege they may not hold.
	if err := sd.SetOwner(adminSid, false); err != nil {
		return nil, fmt.Errorf("set owner: %w", err)
	}

	return sd.ToSelfRelative()
}

type dirSecurityVerdict int

const (
	dirSecurityOK dirSecurityVerdict = iota
	dirSecurityRepairable
	dirSecurityUntrusted
)

// classifyDirSecurity separates a directory an unprivileged user could have
// planted (untrusted) from an admin-created one whose DACL is merely too wide.
func classifyDirSecurity(owner *windows.SID, dacl *windows.ACL, daclPresent bool) (dirSecurityVerdict, error) {
	if owner == nil || (!owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid)) {
		return dirSecurityUntrusted, errors.New("not owned by SYSTEM or Administrators")
	}
	if !daclPresent || dacl == nil {
		return dirSecurityRepairable, errors.New("no readable DACL")
	}

	for i := uint16(0); i < dacl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil {
			return dirSecurityUntrusted, fmt.Errorf("read ACE %d: %w", i, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		return dirSecurityRepairable, fmt.Errorf("grants access to a non-admin principal (%s)", sid.String())
	}

	return dirSecurityOK, nil
}

// readDACL reports absence as present=false; x/sys signals it through
// ERROR_OBJECT_NOT_FOUND, and its second return is "defaulted", not "present".
func readDACL(sd *windows.SECURITY_DESCRIPTOR) (*windows.ACL, bool, error) {
	dacl, _, err := sd.DACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return dacl, dacl != nil, nil
}

func inspectDirSecurity(path string) (dirSecurityVerdict, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return dirSecurityUntrusted, fmt.Errorf("read security info for %s: %w", path, err)
	}

	owner, _, err := sd.Owner()
	if err != nil {
		return dirSecurityUntrusted, fmt.Errorf("read owner of %s: %w", path, err)
	}
	dacl, present, err := readDACL(sd)
	if err != nil {
		return dirSecurityUntrusted, fmt.Errorf("read DACL of %s: %w", path, err)
	}

	verdict, reason := classifyDirSecurity(owner, dacl, present)
	if reason != nil {
		reason = fmt.Errorf("%s %w", path, reason)
	}
	return verdict, reason
}

// verifySecureDir tightens an admin-owned directory that predates the
// lockdown instead of refusing it, and only fails when it cannot be trusted.
func verifySecureDir(path string) error {
	verdict, reason := inspectDirSecurity(path)
	switch verdict {
	case dirSecurityOK:
		return nil
	case dirSecurityRepairable:
		if !isPrivilegedProcess() {
			return fmt.Errorf("%w; re-run elevated or set %s", reason, appSupportDirOverrideEnv)
		}
		log.Printf("platform: re-securing %s (%v)", path, reason)
		if err := applyAdminOnlyDACL(path); err != nil {
			return err
		}
		if verdict, reason := inspectDirSecurity(path); verdict != dirSecurityOK {
			return fmt.Errorf("still not secure after re-securing: %w", reason)
		}
		return nil
	default:
		return reason
	}
}

func applyAdminOnlyDACL(path string) error {
	dacl, err := adminOnlyDACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("re-secure %s: %w", path, err)
	}
	return nil
}
