package state

import (
	"sync"
	"time"
)

type LogStore struct {
	mu         sync.RWMutex
	entries    []LogEntry
	maxEntries int
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
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, LogEntry{
		TS:     time.Now().UnixMilli(),
		Level:  level,
		Source: source,
		Msg:    msg,
	})

	if len(s.entries) > s.maxEntries {
		delta := len(s.entries) - s.maxEntries
		s.entries = append([]LogEntry(nil), s.entries[delta:]...)
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
