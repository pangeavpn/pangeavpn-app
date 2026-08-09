package state

import (
	"sync"
	"time"
)

type LogStore struct {
	mu         sync.RWMutex
	entries    []LogEntry
	maxEntries int
	sink       *fileSink
}

// AttachFile mirrors every entry to disk, so logs survive a crash that wipes
// the in-memory ring buffer.
func (s *LogStore) AttachFile(path string, maxSize int64, keep int) error {
	sink, err := newFileSink(path, maxSize, keep)
	if err != nil {
		return err
	}

	s.mu.Lock()
	previous := s.sink
	s.sink = sink
	s.mu.Unlock()

	if previous != nil {
		previous.Close()
	}
	return nil
}

func (s *LogStore) CloseFile() error {
	s.mu.Lock()
	sink := s.sink
	s.sink = nil
	s.mu.Unlock()

	if sink == nil {
		return nil
	}
	return sink.Close()
}

func NewLogStore(maxEntries int) *LogStore {
	if maxEntries <= 0 {
		maxEntries = 3000
	}

	return &LogStore{
		entries:    make([]LogEntry, 0, maxEntries),
		maxEntries: maxEntries,
	}
}

func (s *LogStore) Add(level LogLevel, source LogSource, msg string) {
	entry := LogEntry{
		TS:     time.Now().UnixMilli(),
		Level:  level,
		Source: source,
		Msg:    msg,
	}

	s.mu.Lock()
	s.entries = append(s.entries, entry)
	if len(s.entries) > s.maxEntries {
		delta := len(s.entries) - s.maxEntries
		s.entries = append([]LogEntry(nil), s.entries[delta:]...)
	}
	sink := s.sink
	s.mu.Unlock()

	if sink != nil {
		sink.write(entry)
	}
}

func (s *LogStore) Since(since int64) []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if since <= 0 {
		return append([]LogEntry(nil), s.entries...)
	}

	out := make([]LogEntry, 0, len(s.entries))
	for _, item := range s.entries {
		if item.TS >= since {
			out = append(out, item)
		}
	}

	return out
}
