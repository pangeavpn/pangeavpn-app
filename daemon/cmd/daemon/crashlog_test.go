package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The daemon runs as a service with stderr discarded, so a panic is invisible
// unless fd 2 itself is redirected. Re-exec and panic to prove it lands.
func TestRedirectStderrCapturesPanic(t *testing.T) {
	if os.Getenv("PANGEA_CRASHLOG_CHILD") == "1" {
		if _, err := redirectStderr(os.Getenv("PANGEA_CRASHLOG_PATH")); err != nil {
			os.Exit(3)
		}
		panic("deliberate crash for crash-log test")
	}

	logPath := filepath.Join(t.TempDir(), "daemon-crash.log")
	cmd := exec.Command(os.Args[0], "-test.run=TestRedirectStderrCapturesPanic")
	cmd.Env = append(os.Environ(),
		"PANGEA_CRASHLOG_CHILD=1",
		"PANGEA_CRASHLOG_PATH="+logPath,
	)
	if err := cmd.Run(); err == nil {
		t.Fatal("child was expected to die from the panic")
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("crash log was never written: %v", err)
	}

	got := string(raw)
	if !strings.Contains(got, "deliberate crash for crash-log test") {
		t.Fatalf("panic message missing from crash log:\n%s", got)
	}
	if !strings.Contains(got, "goroutine") {
		t.Fatalf("stack trace missing from crash log:\n%s", got)
	}
}

func TestRedirectStderrAppendsAcrossRestarts(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "daemon-crash.log")
	if err := os.WriteFile(logPath, []byte("previous run\n"), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	f, err := redirectStderr(logPath)
	if err != nil {
		t.Fatalf("redirectStderr: %v", err)
	}
	defer f.Close()

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(raw), "previous run") {
		t.Fatal("redirect truncated the prior run's crash log")
	}
}
