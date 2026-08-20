package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestFileSinkWritesJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	sink, err := newFileSink(path, 1<<20, 2)
	if err != nil {
		t.Fatalf("newFileSink: %v", err)
	}
	defer sink.Close()

	sink.write(LogEntry{TS: 1, Level: LogInfo, Source: SourceNaive, Msg: "first"})
	sink.write(LogEntry{TS: 2, Level: LogError, Source: SourceDaemon, Msg: "second"})

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
	}

	var got LogEntry
	if err := json.Unmarshal([]byte(lines[1]), &got); err != nil {
		t.Fatalf("unmarshal line 2: %v", err)
	}
	if got.Msg != "second" || got.Level != LogError {
		t.Fatalf("line 2 = %+v, want msg=second level=error", got)
	}
}

// A crash must not lose the last entry, so every write hits the fd immediately.
func TestFileSinkIsReadableBeforeClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	sink, err := newFileSink(path, 1<<20, 2)
	if err != nil {
		t.Fatalf("newFileSink: %v", err)
	}
	defer sink.Close()

	sink.write(LogEntry{TS: 1, Level: LogInfo, Source: SourceDaemon, Msg: "unflushed"})

	if lines := readLines(t, path); len(lines) != 1 || !strings.Contains(lines[0], "unflushed") {
		t.Fatalf("entry not on disk before Close: %q", lines)
	}
}

func TestFileSinkRotatesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	sink, err := newFileSink(path, 200, 2)
	if err != nil {
		t.Fatalf("newFileSink: %v", err)
	}
	defer sink.Close()

	for i := 0; i < 60; i++ {
		sink.write(LogEntry{TS: int64(i), Level: LogInfo, Source: SourceDaemon, Msg: strings.Repeat("x", 40)})
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active log missing: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated log .1 missing: %v", err)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("keep=2 should prune .3, stat err = %v", err)
	}
}

func TestFileSinkConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	sink, err := newFileSink(path, 1<<20, 2)
	if err != nil {
		t.Fatalf("newFileSink: %v", err)
	}
	defer sink.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sink.write(LogEntry{TS: int64(n), Level: LogInfo, Source: SourceDaemon, Msg: "concurrent"})
		}(i)
	}
	wg.Wait()

	lines := readLines(t, path)
	if len(lines) != 50 {
		t.Fatalf("got %d lines, want 50", len(lines))
	}
	for i, line := range lines {
		var e LogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %d is not valid JSON (interleaved write): %v", i, err)
		}
	}
}

// A rename step in rotation can fail (locked handle, permissions); the sink
// must keep its size counter honest against the real file instead of
// re-arming the cap against zero, and must keep accepting writes.
func TestFileSinkRotationSurvivesRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	sink, err := newFileSink(path, 200, 1)
	if err != nil {
		t.Fatalf("newFileSink: %v", err)
	}
	defer sink.Close()

	// A non-empty ".1" directory can never become the rename target: the drop
	// step can't remove it and the rename can't replace it, so every rotation
	// attempt fails the same way for the rest of the test.
	if err := os.Mkdir(path+".1", 0o755); err != nil {
		t.Fatalf("mkdir %s.1: %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path+".1", "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed %s.1: %v", path, err)
	}

	for i := 0; i < 10; i++ {
		if err := sink.write(LogEntry{TS: int64(i), Level: LogInfo, Source: SourceDaemon, Msg: strings.Repeat("x", 40)}); err != nil {
			t.Logf("write %d: %v", i, err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat active log: %v", err)
	}

	sink.mu.Lock()
	tracked := sink.size
	sink.mu.Unlock()

	if tracked != info.Size() {
		t.Fatalf("tracked size %d does not match real file size %d after failed rotation", tracked, info.Size())
	}
	if tracked < sink.maxSize {
		t.Fatalf("expected cap to still be exceeded after failed rotation, tracked=%d maxSize=%d", tracked, sink.maxSize)
	}
}

// Concurrent Add calls must keep the file sink and the in-memory ring in the
// same order, and trimming past maxEntries must preserve that order too.
func TestLogStoreConcurrentAddPreservesOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	const maxEntries = 50
	const n = 200

	store := NewLogStore(maxEntries)
	if err := store.AttachFile(path, 1<<20, 2); err != nil {
		t.Fatalf("AttachFile: %v", err)
	}
	defer store.CloseFile()

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.Add(LogInfo, SourceDaemon, fmt.Sprintf("entry-%d", i))
		}(i)
	}
	wg.Wait()

	mem := store.Since(0)
	if len(mem) != maxEntries {
		t.Fatalf("in-memory entries = %d, want %d", len(mem), maxEntries)
	}

	lines := readLines(t, path)
	if len(lines) != n {
		t.Fatalf("file lines = %d, want %d", len(lines), n)
	}

	tail := lines[len(lines)-maxEntries:]
	for i, line := range tail {
		var e LogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %d not valid JSON: %v", i, err)
		}
		if e.Msg != mem[i].Msg {
			t.Fatalf("file/memory order mismatch at %d: file=%q memory=%q", i, e.Msg, mem[i].Msg)
		}
	}
}

func TestLogStoreAttachFileMirrorsEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	store := NewLogStore(10)
	if err := store.AttachFile(path, 1<<20, 2); err != nil {
		t.Fatalf("AttachFile: %v", err)
	}
	defer store.CloseFile()

	store.Add(LogInfo, SourceNaive, "mirrored to disk")

	if got := store.Since(0); len(got) != 1 {
		t.Fatalf("in-memory entries = %d, want 1", len(got))
	}
	if lines := readLines(t, path); len(lines) != 1 || !strings.Contains(lines[0], "mirrored to disk") {
		t.Fatalf("disk lines = %q", lines)
	}
}
