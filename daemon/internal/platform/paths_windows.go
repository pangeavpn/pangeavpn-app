//go:build windows

package platform

import (
	"errors"
	"fmt"
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

// adminOnlySecurityDescriptor builds a protected (non-inherited-from-parent)
// DACL granting SYSTEM and Administrators full control, inheritable by children.
func adminOnlySecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
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

	sd, err := windows.NewSecurityDescriptor()
	if err != nil {
		return nil, fmt.Errorf("new security descriptor: %w", err)
	}
	if err := sd.SetDACL(dacl, true, false); err != nil {
		return nil, fmt.Errorf("set dacl: %w", err)
	}
	// Owner is set to the caller's own identity (SYSTEM or an elevated
	// admin), which never requires SeRestorePrivilege to assign.
	if err := sd.SetOwner(systemSid, false); err != nil {
		return nil, fmt.Errorf("set owner: %w", err)
	}

	return sd.ToSelfRelative()
}

// verifySecureDir hard-fails unless an already-existing directory is owned by
// SYSTEM or Administrators and its DACL grants no access to anyone else.
func verifySecureDir(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read security info for %s: %w", path, err)
	}

	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read owner of %s: %w", path, err)
	}
	if !owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return fmt.Errorf("%s is not owned by SYSTEM or Administrators", path)
	}

	dacl, present, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read DACL of %s: %w", path, err)
	}
	if !present || dacl == nil {
		return fmt.Errorf("%s has no DACL", path)
	}

	for i := uint16(0); i < dacl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil {
			return fmt.Errorf("read ACE %d of %s: %w", i, path, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		return fmt.Errorf("%s grants access to a non-admin principal (%s)", path, sid.String())
	}

	return nil
}
