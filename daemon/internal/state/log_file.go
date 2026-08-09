package state

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// fileSink mirrors log entries to disk as JSON lines. Writes are unbuffered so
// an abrupt process death still leaves the last entry on disk.
type fileSink struct {
	mu      sync.Mutex
	f       *os.File
	path    string
	size    int64
	maxSize int64
	keep    int
}

func newFileSink(path string, maxSize int64, keep int) (*fileSink, error) {
	if maxSize <= 0 {
		maxSize = 8 << 20
	}
	if keep < 0 {
		keep = 0
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		// Nothing written yet, so a close error has no data behind it.
		_ = f.Close()
		return nil, fmt.Errorf("stat log file %s: %w", path, err)
	}

	return &fileSink{f: f, path: path, size: info.Size(), maxSize: maxSize, keep: keep}, nil
}

func (s *fileSink) write(entry LogEntry) {
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return
	}
	if s.size+int64(len(line)) > s.maxSize {
		s.rotateLocked()
	}
	n, err := s.f.Write(line)
	if err != nil {
		return
	}
	s.size += int64(n)
}

// rotateLocked shifts daemon.log -> .1 -> .2, dropping anything past keep.
func (s *fileSink) rotateLocked() {
	if s.f == nil {
		return
	}
	// This handle has log lines behind it, so a close error means entries were
	// lost. Report to stderr, which the daemon points at its crash log — writing
	// it back through the sink being rotated would recurse.
	if err := s.f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "log rotation: closing %s failed, entries may be lost: %v\n", s.path, err)
	}
	s.f = nil

	if s.keep == 0 {
		os.Remove(s.path)
	} else {
		os.Remove(fmt.Sprintf("%s.%d", s.path, s.keep))
		for i := s.keep - 1; i >= 1; i-- {
			os.Rename(fmt.Sprintf("%s.%d", s.path, i), fmt.Sprintf("%s.%d", s.path, i+1))
		}
		os.Rename(s.path, s.path+".1")
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	s.f = f
	s.size = 0
}

func (s *fileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
