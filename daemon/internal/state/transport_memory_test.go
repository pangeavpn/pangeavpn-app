package state

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestMemoryStore(t *testing.T) (*TransportMemoryStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transport-memory.json")
	store, err := NewTransportMemoryStore(path)
	if err != nil {
		t.Fatalf("NewTransportMemoryStore: %v", err)
	}
	return store, path
}

func TestTransportMemory_RecordAndLookup(t *testing.T) {
	store, _ := newTestMemoryStore(t)

	if _, ok := store.Lookup("wifi-a"); ok {
		t.Fatal("expected no entry before recording")
	}
	if err := store.Record("wifi-a", "reality"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, ok := store.Lookup("wifi-a")
	if !ok || got != "reality" {
		t.Fatalf("Lookup = (%q, %v), want (reality, true)", got, ok)
	}
}

func TestTransportMemory_EmptyInputsAreNoOps(t *testing.T) {
	store, _ := newTestMemoryStore(t)

	if err := store.Record("", "reality"); err != nil {
		t.Fatalf("Record empty key: %v", err)
	}
	if err := store.Record("wifi-a", ""); err != nil {
		t.Fatalf("Record empty transport: %v", err)
	}
	if _, ok := store.Lookup(""); ok {
		t.Fatal("empty key should never resolve")
	}
	if _, ok := store.Lookup("wifi-a"); ok {
		t.Fatal("empty transport should not have been stored")
	}
}

func TestTransportMemory_PersistsAcrossReload(t *testing.T) {
	store, path := newTestMemoryStore(t)
	if err := store.Record("wifi-a", "naive"); err != nil {
		t.Fatalf("Record: %v", err)
	}

	reopened, err := NewTransportMemoryStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.Lookup("wifi-a")
	if !ok || got != "naive" {
		t.Fatalf("after reload Lookup = (%q, %v), want (naive, true)", got, ok)
	}
}

func TestTransportMemory_PrunesLeastRecentlyUpdated(t *testing.T) {
	store, _ := newTestMemoryStore(t)
	// Deterministic, monotonic clock so pruning order is well-defined.
	var clock int64
	store.now = func() int64 { clock++; return clock }

	total := maxTransportMemoryEntries + 10
	for i := 0; i < total; i++ {
		key := "net-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		if err := store.Record(key, "cloak"); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	store.mu.Lock()
	size := len(store.data.Networks)
	store.mu.Unlock()
	if size != maxTransportMemoryEntries {
		t.Fatalf("store size = %d, want capped at %d", size, maxTransportMemoryEntries)
	}
}

func TestTransportMemory_CorruptFileResetsToEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transport-memory.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	store, err := NewTransportMemoryStore(path)
	if err != nil {
		t.Fatalf("expected corrupt file to be tolerated, got: %v", err)
	}
	if _, ok := store.Lookup("anything"); ok {
		t.Fatal("expected empty store after corrupt load")
	}
	// And it should be usable (rewritten) afterward.
	if err := store.Record("wifi-a", "hysteria2"); err != nil {
		t.Fatalf("Record after corrupt reset: %v", err)
	}
	if got, ok := store.Lookup("wifi-a"); !ok || got != "hysteria2" {
		t.Fatalf("Lookup = (%q, %v), want (hysteria2, true)", got, ok)
	}
}
