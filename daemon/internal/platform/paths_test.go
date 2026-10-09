//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppSupportDirHonoursOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv(appSupportDirOverrideEnv, dir)

	got, err := AppSupportDir()
	if err != nil {
		t.Fatalf("AppSupportDir: %v", err)
	}
	if got != dir {
		t.Fatalf("app support dir = %q, want the override %q", got, dir)
	}
}

func TestAppSupportDirRejectsRelativeOverride(t *testing.T) {
	t.Setenv(appSupportDirOverrideEnv, "relative/state")

	if _, err := AppSupportDir(); err == nil {
		t.Fatal("expected a relative override to be refused")
	}
}

// The launchd plist points the daemon at the installer's directory, so the
// token must land there rather than in a root-only path the app cannot read.
func TestTokenPathFollowsOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv(appSupportDirOverrideEnv, dir)

	got, err := TokenPath()
	if err != nil {
		t.Fatalf("TokenPath: %v", err)
	}
	if want := filepath.Join(dir, "daemon-token.txt"); got != want {
		t.Fatalf("token path = %q, want %q", got, want)
	}
}

// repairDataDirMode runs the mode repair verifyRootOwnedDir applies, without
// the uid-0 check a test process cannot satisfy.
func repairDataDirMode(t *testing.T, dir string) error {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return repairDirMode(dir, info.Mode().Perm())
}

// mkdirMode creates dir at an exact mode, defeating the umask MkdirAll applies.
func mkdirMode(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(dir, mode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}

func TestEnsureDataDirCreatesTraversableDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "support")

	if err := ensureDataDir(dir); err != nil {
		t.Fatalf("ensureDataDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&dirTraverseBits != dirTraverseBits {
		t.Fatalf("mode = %#o, want group and other execute", perm)
	}
}

// An 0.5.7 install left the state dir 0700, which denies the desktop app the
// traversal it needs to reach the token; the daemon has to repair that itself.
func TestVerifyRootOwnedDirRestoresTraversal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stale")
	mkdirMode(t, dir, 0o700)

	if err := repairDataDirMode(t, dir); err != nil {
		t.Fatalf("repair: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&dirTraverseBits != dirTraverseBits {
		t.Fatalf("mode = %#o, want traversal restored", perm)
	}
}

func TestVerifyRootOwnedDirStripsWriteBits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "loose")
	mkdirMode(t, dir, 0o777)

	if err := repairDataDirMode(t, dir); err != nil {
		t.Fatalf("repair: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		t.Fatalf("mode = %#o, want group and other write stripped", perm)
	}
}

func TestVerifyRootOwnedDirLeavesAGoodModeAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fine")
	mkdirMode(t, dir, 0o755)

	if err := repairDataDirMode(t, dir); err != nil {
		t.Fatalf("repair: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("mode = %#o, want 0755 untouched", perm)
	}
}

func TestEnsureDataDirRefusesSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := ensureDataDir(link); err == nil {
		t.Fatal("expected a symlinked data dir to be refused")
	}
}

func TestEnsureDataDirRefusesNonDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := ensureDataDir(file); err == nil {
		t.Fatal("expected a file to be refused as a data dir")
	}
}
