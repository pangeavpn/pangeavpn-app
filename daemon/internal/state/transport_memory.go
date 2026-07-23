package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// maxTransportMemoryEntries caps how many networks are remembered; the
// least-recently-updated entries are pruned past it. High enough to cover every
// network a user realistically revisits, low enough to bound the file.
const maxTransportMemoryEntries = 64

// TransportMemoryEntry records which transport last established a tunnel on a
// network and when, so the auto-connect cascade can try it first.
type TransportMemoryEntry struct {
	Transport string `json:"transport"`
	UpdatedAt int64  `json:"updatedAt"`
}

type transportMemoryData struct {
	Networks map[string]TransportMemoryEntry `json:"networks"`
}

// TransportMemoryStore is a small persisted map of network key -> last-good
// transport. It is a best-effort optimization cache, not authoritative config:
// a missing or corrupt file resets to empty rather than failing, since the
// cascade always falls back to trying every transport.
type TransportMemoryStore struct {
	mu   sync.Mutex
	path string
	data transportMemoryData
	now  func() int64
}

// NewTransportMemoryStore opens (or creates) the store at path. A corrupt or
// unreadable file is treated as empty so the daemon still starts.
func NewTransportMemoryStore(path string) (*TransportMemoryStore, error) {
	s := &TransportMemoryStore{
		path: path,
		data: transportMemoryData{Networks: map[string]TransportMemoryEntry{}},
		now:  func() int64 { return time.Now().Unix() },
	}
	if err := s.loadOrCreate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Lookup returns the transport last recorded as working for networkKey, or
// ("", false) when there is nothing remembered (including an empty key).
func (s *TransportMemoryStore) Lookup(networkKey string) (string, bool) {
	if networkKey == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data.Networks[networkKey]
	if !ok || entry.Transport == "" {
		return "", false
	}
	return entry.Transport, true
}

// Record remembers transport as the last-good for networkKey and persists.
// No-ops on empty inputs. Prunes least-recently-updated entries past the cap.
func (s *TransportMemoryStore) Record(networkKey, transport string) error {
	if networkKey == "" || transport == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Networks[networkKey] = TransportMemoryEntry{Transport: transport, UpdatedAt: s.now()}
	s.pruneLocked()
	return s.persistLocked()
}

func (s *TransportMemoryStore) pruneLocked() {
	if len(s.data.Networks) <= maxTransportMemoryEntries {
		return
	}
	type keyed struct {
		key       string
		updatedAt int64
	}
	entries := make([]keyed, 0, len(s.data.Networks))
	for key, entry := range s.data.Networks {
		entries = append(entries, keyed{key, entry.UpdatedAt})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].updatedAt < entries[j].updatedAt })
	for i := 0; i < len(entries)-maxTransportMemoryEntries; i++ {
		delete(s.data.Networks, entries[i].key)
	}
}

func (s *TransportMemoryStore) loadOrCreate() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create transport memory directory: %w", err)
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.persistLocked()
		}
		// Unreadable cache is non-fatal: start empty.
		return nil
	}

	var data transportMemoryData
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &data); err != nil {
			// Corrupt cache is non-fatal: start empty and rewrite.
			s.data = transportMemoryData{Networks: map[string]TransportMemoryEntry{}}
			return s.persistLocked()
		}
	}
	if data.Networks == nil {
		data.Networks = map[string]TransportMemoryEntry{}
	}
	s.data = data
	return nil
}

func (s *TransportMemoryStore) persistLocked() error {
	payload, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal transport memory: %w", err)
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return fmt.Errorf("write tmp transport memory: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace transport memory: %w", err)
	}
	return nil
}
