//go:build darwin

package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	updateStagingDir   = "/Library/Application Support/PangeaVPN/updates"
	updateLogPath      = "/Library/Application Support/PangeaVPN/logs/update.log"
	installedAppPath   = "/Applications/PangeaVPN.app"
	updatePackageID    = "com.pangea.pangeavpn"
	maxUpdatePackage   = 1 << 30
	updateStagingTTL   = 30 * time.Minute
	stagedPackageName  = "update.pkg"
	expandedPackageDir = "expanded"
)

var (
	updateMu         sync.Mutex
	teamIdentifierRe = regexp.MustCompile(`(?m)^TeamIdentifier=([A-Z0-9]{10})$`)
	packageInfoRe    = regexp.MustCompile(`<pkg-info\b[^>]*>`)
	identifierAttrRe = regexp.MustCompile(`\sidentifier="([^"]+)"`)
	versionAttrRe    = regexp.MustCompile(`\sversion="([^"]+)"`)
	signedStatusRe   = regexp.MustCompile(`(?m)^\s*Status: signed by a developer certificate issued by Apple for distribution\s*$`)
	notarizedRe      = regexp.MustCompile(`(?m)^source=Notarized Developer ID$`)
)

// InstallUpdatePackage installs a newer PangeaVPN release package that the app
// downloaded, as root and without a password prompt. Only a package signed by
// this daemon's own Developer ID team, notarized, and newer than the installed
// app gets through, so a caller holding the daemon token can install nothing
// but a genuine upgrade.
func InstallUpdatePackage(pkgPath, expectedSHA256 string) error {
	updateMu.Lock()
	defer updateMu.Unlock()

	// The detached install deletes the staging dir when it finishes; a fresh
	// one means an install is still running.
	if info, err := os.Lstat(updateStagingDir); err == nil && time.Since(info.ModTime()) < updateStagingTTL {
		return errors.New("an update is already being installed")
	}

	team, err := ownTeamID()
	if err != nil {
		return err
	}
	staged, err := stageUpdatePackage(pkgPath, expectedSHA256)
	if err == nil {
		err = verifyUpdatePackage(staged, team)
	}
	if err == nil {
		err = spawnUpdateInstall(staged)
	}
	if err != nil {
		_ = os.RemoveAll(updateStagingDir)
		return err
	}
	return nil
}

// stageUpdatePackage copies the package into a root-only directory and checks
// its digest on the way, so nothing the caller controls can change between
// verification and install.
func stageUpdatePackage(src, expectedSHA256 string) (string, error) {
	want := strings.ToLower(strings.TrimSpace(expectedSHA256))
	if _, err := hex.DecodeString(want); err != nil || len(want) != sha256.Size*2 {
		return "", errors.New("invalid package digest")
	}
	if !filepath.IsAbs(src) || filepath.Clean(src) != src || filepath.Ext(src) != ".pkg" {
		return "", errors.New("package path must be an absolute path to a .pkg")
	}

	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", fmt.Errorf("open package: %w", err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect package: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxUpdatePackage {
		return "", errors.New("package is not a regular file of a plausible size")
	}

	// The support dir is root-owned and not writable by anyone else, so
	// nothing can be planted here between the removal and the mkdir.
	if err := os.RemoveAll(updateStagingDir); err != nil {
		return "", fmt.Errorf("clear update staging: %w", err)
	}
	if err := os.Mkdir(updateStagingDir, 0o700); err != nil {
		return "", fmt.Errorf("create update staging: %w", err)
	}
	dst := filepath.Join(updateStagingDir, stagedPackageName)
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create staged package: %w", err)
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(in, maxUpdatePackage+1))
	closeErr := out.Close()
	if copyErr != nil {
		return "", fmt.Errorf("copy package: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("write staged package: %w", closeErr)
	}
	if n > maxUpdatePackage {
		return "", errors.New("package is too large")
	}
	if hex.EncodeToString(hash.Sum(nil)) != want {
		return "", errors.New("package digest does not match the release checksum")
	}
	return dst, nil
}

func verifyUpdatePackage(pkg, team string) error {
	sig, err := exec.Command("/usr/sbin/pkgutil", "--check-signature", pkg).CombinedOutput()
	if err != nil {
		return fmt.Errorf("package is not signed: %s", firstLine(sig))
	}
	if err := checkPackageSignature(string(sig), team); err != nil {
		return err
	}

	verdict, err := exec.Command("/usr/sbin/spctl", "--assess", "--type", "install", "-vv", pkg).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gatekeeper rejected the package: %s", firstLine(verdict))
	}
	if err := checkGatekeeperVerdict(string(verdict), team); err != nil {
		return err
	}

	version, err := stagedPackageVersion(pkg)
	if err != nil {
		return err
	}
	installed, err := installedAppVersion()
	if err != nil {
		return err
	}
	if compareVersions(version, installed) <= 0 {
		return fmt.Errorf("package version %s is not newer than the installed %s", version, installed)
	}
	return nil
}

// ownTeamID is the Team ID this daemon is signed with: updates must come from
// the same developer. An ad-hoc or unsigned build has none and can't update.
func ownTeamID() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate daemon binary: %w", err)
	}
	out, _ := exec.Command("/usr/bin/codesign", "-dv", "--verbose=2", exe).CombinedOutput()
	return parseTeamIdentifier(string(out))
}

func parseTeamIdentifier(codesignOutput string) (string, error) {
	m := teamIdentifierRe.FindStringSubmatch(codesignOutput)
	if m == nil {
		return "", errors.New("this daemon is not Developer ID signed, so it cannot install updates")
	}
	return m[1], nil
}

// The leaf line ends with the Team ID in parentheses; anchoring on the end of
// the line stops another developer's name from smuggling ours in.
func checkPackageSignature(pkgutilOutput, team string) error {
	if !signedStatusRe.MatchString(pkgutilOutput) {
		return errors.New("package is not signed with a Developer ID certificate")
	}
	leaf := regexp.MustCompile(`(?m)^\s*1\. Developer ID Installer: .+ \(` + regexp.QuoteMeta(team) + `\)\s*$`)
	if !leaf.MatchString(pkgutilOutput) {
		return fmt.Errorf("package is not signed by team %s", team)
	}
	return nil
}

func checkGatekeeperVerdict(spctlOutput, team string) error {
	if !notarizedRe.MatchString(spctlOutput) {
		return errors.New("package is not notarized")
	}
	origin := regexp.MustCompile(`(?m)^origin=Developer ID Installer: .+ \(` + regexp.QuoteMeta(team) + `\)$`)
	if !origin.MatchString(spctlOutput) {
		return fmt.Errorf("gatekeeper attributes the package to someone other than team %s", team)
	}
	return nil
}

func stagedPackageVersion(pkg string) (string, error) {
	dir := filepath.Join(updateStagingDir, expandedPackageDir)
	defer os.RemoveAll(dir)
	if out, err := exec.Command("/usr/sbin/pkgutil", "--expand", pkg, dir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("expand package: %s", firstLine(out))
	}
	infos, _ := filepath.Glob(filepath.Join(dir, "*.pkg", "PackageInfo"))
	if len(infos) != 1 {
		return "", fmt.Errorf("package holds %d components, expected 1", len(infos))
	}
	data, err := os.ReadFile(infos[0])
	if err != nil {
		return "", fmt.Errorf("read package info: %w", err)
	}
	return parsePackageInfo(string(data))
}

func parsePackageInfo(packageInfo string) (string, error) {
	tag := packageInfoRe.FindString(packageInfo)
	id := identifierAttrRe.FindStringSubmatch(tag)
	version := versionAttrRe.FindStringSubmatch(tag)
	if id == nil || version == nil {
		return "", errors.New("package info has no identifier or version")
	}
	if id[1] != updatePackageID {
		return "", fmt.Errorf("package %s is not PangeaVPN", id[1])
	}
	return version[1], nil
}

// The pkg installs the app root-owned; one anybody else could rewrite can't
// vouch for the installed version, which is what blocks downgrades.
func installedAppVersion() (string, error) {
	plist := filepath.Join(installedAppPath, "Contents", "Info.plist")
	info, err := os.Lstat(plist)
	if err != nil {
		return "", fmt.Errorf("inspect installed app: %w", err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || info.Mode()&0o022 != 0 || !info.Mode().IsRegular() {
		return "", errors.New("the installed app is not root-owned, so its version can't be trusted")
	}
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", plist).Output()
	if err != nil {
		return "", fmt.Errorf("read installed version: %w", err)
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return "", errors.New("installed app has no version")
	}
	return version, nil
}

// The package's preinstall boots this daemon out, which would take a child in
// our session with it: the install runs in its own session, then restarts the
// services if it failed and reopens the app for whoever is at the console.
const updateInstallScript = `umask 077
/bin/sleep 1
echo "=== $(/bin/date -u '+%Y-%m-%dT%H:%M:%SZ') installing $1" >>"$2"
/usr/sbin/installer -pkg "$1" -target / >>"$2" 2>&1
status=$?
/bin/rm -rf "$3"
echo "installer exited with $status" >>"$2"
if [ "$status" -ne 0 ]; then
  for plist in /Library/LaunchDaemons/com.pangea.pangeavpn.pf.plist /Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist; do
    [ -f "$plist" ] && /bin/launchctl bootstrap system "$plist" >>"$2" 2>&1
  done
  /bin/launchctl kickstart system/com.pangea.pangeavpn.daemon >>"$2" 2>&1
fi
uid=$(/usr/bin/stat -f %u /dev/console 2>/dev/null)
case "$uid" in ''|0|*[!0-9]*) exit "$status" ;; esac
/bin/launchctl asuser "$uid" /usr/bin/sudo -u "#$uid" /usr/bin/open -a ` + installedAppPath + ` >>"$2" 2>&1
exit "$status"
`

func spawnUpdateInstall(pkg string) error {
	if err := os.MkdirAll(filepath.Dir(updateLogPath), 0o700); err != nil {
		return fmt.Errorf("create update log dir: %w", err)
	}
	cmd := exec.Command("/bin/sh", "-c", updateInstallScript, "pangeavpn-update", pkg, updateLogPath, updateStagingDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn update install: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func firstLine(out []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}
