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
	closed  bool
}

func newFileSink(path string, maxSize int64, keep int) (*fileSink, error) {
	if maxSize <= 0 {
		maxSize = 8 << 20
	}
	if keep < 0 {
		keep = 0
	}

	s := &fileSink{path: path, maxSize: maxSize, keep: keep}
	if err := s.openLocked(); err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	return s, nil
}

// openLocked (re)opens s.path and syncs s.size to what is actually on disk,
// rather than assuming a fresh file — callers may be recovering from a
// rotation that failed to move the old file out of the way.
func (s *fileSink) openLocked() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	s.f = f
	s.size = info.Size()
	return nil
}

func (s *fileSink) write(entry LogEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	if s.f == nil {
		if err := s.openLocked(); err != nil {
			return fmt.Errorf("reopen log file %s: %w", s.path, err)
		}
	}
	if s.size+int64(len(line)) > s.maxSize {
		if err := s.rotateLocked(); err != nil {
			fmt.Fprintf(os.Stderr, "log rotation: %s: %v\n", s.path, err)
		}
		if s.f == nil {
			return fmt.Errorf("log file %s unavailable after failed rotation", s.path)
		}
	}

	n, werr := s.f.Write(line)
	s.size += int64(n)
	if werr != nil {
		return fmt.Errorf("write log entry to %s: %w", s.path, werr)
	}
	return nil
}

// rotateLocked shifts daemon.log -> .1 -> .2, dropping anything past keep. It
// always leaves s.f pointing at a real, size-accounted file: if a rename step
// fails (locked handle, cross-device, permissions), the original file stays
// at s.path and gets reopened with its true size, so the caller keeps
// retrying rotation instead of silently re-arming the cap against stale data.
func (s *fileSink) rotateLocked() error {
	if s.f == nil {
		return nil
	}
	// This handle has log lines behind it, so a close error means entries were
	// lost. Report to stderr, which the daemon points at its crash log — writing
	// it back through the sink being rotated would recurse.
	if err := s.f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "log rotation: closing %s failed, entries may be lost: %v\n", s.path, err)
	}
	s.f = nil

	var rotateErr error
	if s.keep == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			rotateErr = fmt.Errorf("remove %s: %w", s.path, err)
		}
	} else {
		os.Remove(fmt.Sprintf("%s.%d", s.path, s.keep))
		for i := s.keep - 1; i >= 1; i-- {
			src := fmt.Sprintf("%s.%d", s.path, i)
			dst := fmt.Sprintf("%s.%d", s.path, i+1)
			if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
				rotateErr = fmt.Errorf("rename %s to %s: %w", src, dst, err)
			}
		}
		if err := os.Rename(s.path, s.path+".1"); err != nil {
			rotateErr = fmt.Errorf("rename %s to %s.1: %w", s.path, s.path, err)
		}
	}

	if err := s.openLocked(); err != nil {
		if rotateErr != nil {
			return fmt.Errorf("%v; reopen after rotation: %w", rotateErr, err)
		}
		return fmt.Errorf("reopen after rotation: %w", err)
	}
	return rotateErr
}

func (s *fileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
