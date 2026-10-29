//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testTeam = "T4TXK4A2JX"

// Captured from a notarized 0.8.1 release candidate.
const signedPkgutilOutput = `Package "PangeaVPN-0.8.1-rc.1-x64.pkg":
   Status: signed by a developer certificate issued by Apple for distribution
   Notarization: trusted by the Apple notary service
   Signed with a trusted timestamp on: 2026-10-27 20:04:33 +0000
   Certificate Chain:
    1. Developer ID Installer: Greez Holdings LLC (T4TXK4A2JX)
       Expires: 2031-10-28 15:12:41 +0000
       SHA256 Fingerprint:
           AA BB CC
       ------------------------------------------------------------------------
    2. Developer ID Certification Authority
       Expires: 2031-09-17 00:00:00 +0000
    3. Apple Root CA
       Expires: 2035-02-09 21:40:36 +0000
`

const acceptedSpctlOutput = `/tmp/PangeaVPN-0.8.1-rc.1-x64.pkg: accepted
source=Notarized Developer ID
origin=Developer ID Installer: Greez Holdings LLC (T4TXK4A2JX)
`

func TestParseTeamIdentifier(t *testing.T) {
	signed := "Executable=/Library/Application Support/PangeaVPN/PangeaDaemon\nIdentifier=daemon\nTeamIdentifier=T4TXK4A2JX\nRuntime Version=26.0.0\n"
	if team, err := parseTeamIdentifier(signed); err != nil || team != testTeam {
		t.Fatalf("parseTeamIdentifier(signed) = %q, %v", team, err)
	}
	if _, err := parseTeamIdentifier("Identifier=daemon\nTeamIdentifier=not set\n"); err == nil {
		t.Error("an ad-hoc daemon must not be able to install updates")
	}
}

func TestCheckPackageSignature(t *testing.T) {
	if err := checkPackageSignature(signedPkgutilOutput, testTeam); err != nil {
		t.Fatalf("genuine package rejected: %v", err)
	}
	if err := checkPackageSignature(signedPkgutilOutput, "ABCDE12345"); err == nil {
		t.Error("a package from another team must be rejected")
	}
	smuggled := strings.Replace(signedPkgutilOutput, "Greez Holdings LLC (T4TXK4A2JX)", "Evil (T4TXK4A2JX) (EVILTEAM00)", 1)
	if err := checkPackageSignature(smuggled, testTeam); err == nil {
		t.Error("another developer's name must not smuggle our Team ID in")
	}
	if err := checkPackageSignature(`Package "x.pkg":`+"\n   Status: no signature\n", testTeam); err == nil {
		t.Error("an unsigned package must be rejected")
	}
	// The team must be the signer, not some other certificate in the chain.
	intermediate := strings.Replace(signedPkgutilOutput, "1. Developer ID Installer: Greez Holdings LLC (T4TXK4A2JX)", "1. Developer ID Installer: Someone Else (EVILTEAM00)", 1)
	intermediate = strings.Replace(intermediate, "2. Developer ID Certification Authority", "2. Developer ID Installer: Greez Holdings LLC (T4TXK4A2JX)", 1)
	if err := checkPackageSignature(intermediate, testTeam); err == nil {
		t.Error("only the leaf certificate may vouch for the team")
	}
}

func TestCheckGatekeeperVerdict(t *testing.T) {
	if err := checkGatekeeperVerdict(acceptedSpctlOutput, testTeam); err != nil {
		t.Fatalf("notarized package rejected: %v", err)
	}
	devID := strings.Replace(acceptedSpctlOutput, "source=Notarized Developer ID", "source=Developer ID", 1)
	if err := checkGatekeeperVerdict(devID, testTeam); err == nil {
		t.Error("a signed but un-notarized package must be rejected")
	}
	other := strings.Replace(acceptedSpctlOutput, "(T4TXK4A2JX)", "(EVILTEAM00)", 1)
	if err := checkGatekeeperVerdict(other, testTeam); err == nil {
		t.Error("a package Gatekeeper attributes to another team must be rejected")
	}
}

func TestParsePackageInfo(t *testing.T) {
	info := `<?xml version="1.0" encoding="utf-8"?>
<pkg-info overwrite-permissions="true" relocatable="false" identifier="com.pangea.pangeavpn" postinstall-action="none" version="0.8.2-rc.1" format-version="2" auth="root">
    <payload numberOfFiles="838" installKBytes="357083"/>
    <bundle path="./PangeaVPN.app" id="com.pangea.pangeavpn" CFBundleShortVersionString="9.9.9" CFBundleVersion="9.9.9"/>
</pkg-info>`
	version, err := parsePackageInfo(info)
	if err != nil || version != "0.8.2-rc.1" {
		t.Fatalf("parsePackageInfo = %q, %v; want the pkg-info version, not the bundle's", version, err)
	}
	if _, err := parsePackageInfo(strings.Replace(info, `identifier="com.pangea.pangeavpn"`, `identifier="com.example.other"`, 1)); err == nil {
		t.Error("a package with another identifier must be rejected")
	}
	if _, err := parsePackageInfo("<pkg-info/>"); err == nil {
		t.Error("package info without identifier or version must be rejected")
	}
}

func TestStageUpdatePackageRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "update.pkg")
	if err := os.WriteFile(pkg, []byte("not really a package"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pkg")
	if err := os.Symlink(pkg, link); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	for name, c := range map[string]struct{ path, digest string }{
		"short digest":   {pkg, "abc"},
		"non-hex digest": {pkg, strings.Repeat("z", 64)},
		"relative path":  {"update.pkg", digest},
		"unclean path":   {dir + "/../" + filepath.Base(dir) + "/update.pkg", digest},
		"not a pkg":      {filepath.Join(dir, "update.zip"), digest},
		"symlink":        {link, digest},
	} {
		if _, err := stageUpdatePackage(c.path, c.digest); err == nil {
			t.Errorf("%s: staging should have failed", name)
		}
	}
}

func TestUpdateInstallScript(t *testing.T) {
	s := updateInstallScript
	install := strings.Index(s, `/usr/sbin/installer -pkg "$1" -target /`)
	cleanup := strings.Index(s, `/bin/rm -rf "$3"`)
	restore := strings.Index(s, "/bin/launchctl bootstrap system")
	relaunch := strings.Index(s, "/usr/bin/open -a "+installedAppPath)
	if install < 0 || cleanup < 0 || restore < 0 || relaunch < 0 {
		t.Fatalf("script missing install, cleanup, recovery or relaunch:\n%s", s)
	}
	if !(install < cleanup && cleanup < restore && restore < relaunch) {
		t.Error("the script must install, clean up, recover the services on failure, then reopen the app")
	}
	if !strings.Contains(s, `if [ "$status" -ne 0 ]; then`) {
		t.Error("services must only be re-bootstrapped when the install failed")
	}
	// Paths arrive as positional parameters, never spliced into the script.
	for _, p := range []string{updateStagingDir, updateLogPath, stagedPackageName} {
		if strings.Contains(s, p) {
			t.Errorf("script must take %q as an argument, not inline it", p)
		}
	}
}
