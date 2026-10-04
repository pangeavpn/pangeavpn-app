//go:build linux

package procmatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxResolveSymlinks(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real", "app")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realBin := filepath.Join(realDir, "app-bin")
	if err := os.WriteFile(realBin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, filepath.Join(base, "launcher")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "linked")); err != nil {
		t.Fatal(err)
	}
	env := newRuleEnv("linux", hostSettings())
	file := compileIn(t, env, filepath.Join(base, "launcher"))
	dir := compileIn(t, env, filepath.Join(base, "linked", "app")+"/")
	checkMatches(t, file, []matchCase{{realBin, true}, {filepath.Join(base, "launcher"), true}, {realBin + "2", false}})
	checkMatches(t, dir, []matchCase{{realBin, true}, {filepath.Join(base, "real", "app2", "x"), false}})
	if got := resolveSymlinks("/snap/bin/firefox", RuleDir); got != nil {
		t.Fatalf("snap rule resolved to %q", got)
	}
}

func TestLinuxHostInit(t *testing.T) {
	exe, err := os.Readlink("/proc/1/exe")
	if err != nil {
		t.Skipf("cannot read pid 1: %v", err)
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	if got := ValidateRule(exe); got != CodeSystemProcess {
		t.Fatalf("ValidateRule(%q) = %q, want %q", exe, got, CodeSystemProcess)
	}
	if !hostEnv().isStop(exe) {
		t.Fatalf("pid 1 image %q does not stop the ancestor walk", exe)
	}
}
