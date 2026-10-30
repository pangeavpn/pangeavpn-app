package state

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLogStoreClearEmptiesTheBufferInPlace(t *testing.T) {
	store := NewLogStore(10)
	store.Add(LogInfo, SourceDaemon, "before")

	store.Clear()
	if got := store.Since(0); len(got) != 0 {
		t.Fatalf("entries after Clear = %d, want 0", len(got))
	}

	store.Add(LogInfo, SourceDaemon, "after")
	if got := store.Since(0); len(got) != 1 || got[0].Msg != "after" {
		t.Fatalf("entries after a later Add = %+v, want only the new one", got)
	}
}

func TestLogStoreClearLeavesTheFileSinkAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	store := NewLogStore(10)
	if err := store.AttachFile(path, 1<<20, 2); err != nil {
		t.Fatalf("AttachFile: %v", err)
	}
	defer store.CloseFile()

	store.Add(LogInfo, SourceDaemon, "first")
	store.Clear()
	store.Add(LogInfo, SourceDaemon, "second")

	lines := readLines(t, path)
	if len(lines) != 2 || !strings.Contains(lines[1], "second") {
		t.Fatalf("disk lines = %q, want both entries still mirrored", lines)
	}
}

func TestLogStoreClearIsSafeAlongsideAdd(t *testing.T) {
	store := NewLogStore(50)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				store.Add(LogInfo, SourceDaemon, "x")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				store.Clear()
			}
		}()
	}
	wg.Wait()
	if got := len(store.Since(0)); got > 50 {
		t.Fatalf("entries = %d, over the cap", got)
	}
}
