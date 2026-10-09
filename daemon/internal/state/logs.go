package state

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type LogStore struct {
	mu sync.RWMutex
	// writeMu serialises Add end-to-end (append + sink write) so concurrent
	// callers can't interleave and write the file out of append order.
	writeMu    sync.Mutex
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

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	previous := s.sink
	s.sink = sink
	s.mu.Unlock()

	if previous != nil {
		return previous.Close()
	}
	return nil
}

func (s *LogStore) CloseFile() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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

	// Held across both the append and the sink write so the two stay in the
	// same order across goroutines instead of racing between them.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	s.entries = append(s.entries, entry)
	if len(s.entries) > s.maxEntries {
		delta := len(s.entries) - s.maxEntries
		copy(s.entries, s.entries[delta:])
		s.entries = s.entries[:s.maxEntries]
	}
	sink := s.sink
	s.mu.Unlock()

	if sink != nil {
		if err := sink.write(entry); err != nil {
			fmt.Fprintf(os.Stderr, "log store: mirror to file failed: %v\n", err)
		}
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
