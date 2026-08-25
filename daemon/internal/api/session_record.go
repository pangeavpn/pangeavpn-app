package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// sessionRecord persists the last user-requested session so a daemon that
// crashed while connected can keep the kill switch armed and reconnect.
type sessionRecord struct {
	ProfileID          string `json:"profileID"`
	AllowLAN           bool   `json:"allowLAN,omitempty"`
	Lockdown           bool   `json:"lockdown,omitempty"`
	PreferredTransport string `json:"preferredTransport,omitempty"`
}

const sessionRecordFile = "last-session.json"

var sessionRecordMu sync.Mutex

// sessionRecordPathFn is replaced by tests to avoid touching the real install.
var sessionRecordPathFn = defaultSessionRecordPath

func defaultSessionRecordPath() (string, error) {
	dir, err := platform.AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionRecordFile), nil
}

// Indirected for tests, mirroring loadKillSwitchState in service.go.
var saveSessionRecord = saveSessionRecordToDisk
var loadSessionRecord = loadSessionRecordFromDisk
var removeSessionRecord = removeSessionRecordFromDisk

func saveSessionRecordToDisk(rec sessionRecord) error {
	sessionRecordMu.Lock()
	defer sessionRecordMu.Unlock()

	path, err := sessionRecordPathFn()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session record: %w", err)
	}

	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.%d.tmp", sessionRecordFile, os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create session record temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write session record: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync session record: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close session record: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename session record: %w", err)
	}
	return nil
}

func loadSessionRecordFromDisk() (sessionRecord, error) {
	sessionRecordMu.Lock()
	defer sessionRecordMu.Unlock()

	path, err := sessionRecordPathFn()
	if err != nil {
		return sessionRecord{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sessionRecord{}, nil
		}
		return sessionRecord{}, fmt.Errorf("read session record: %w", err)
	}
	var rec sessionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return sessionRecord{}, fmt.Errorf("unmarshal session record: %w", err)
	}
	return rec, nil
}

func removeSessionRecordFromDisk() error {
	sessionRecordMu.Lock()
	defer sessionRecordMu.Unlock()

	path, err := sessionRecordPathFn()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session record: %w", err)
	}
	return nil
}
