package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type ConfigStore struct {
	mu     sync.RWMutex
	path   string
	config Config
}

func NewConfigStore(path string) (*ConfigStore, error) {
	store := &ConfigStore{
		path:   path,
		config: DefaultConfig(),
	}

	if err := store.loadOrCreate(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *ConfigStore) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config)
}

func (s *ConfigStore) Set(cfg Config) error {
	next := canonicalizeConfig(cloneConfig(cfg))
	if err := ValidateConfig(next); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.persist(next); err != nil {
		return err
	}
	s.config = next

	return nil
}

func (s *ConfigStore) FindProfile(profileID string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, profile := range s.config.Profiles {
		if profile.ID == profileID {
			return cloneProfile(profile), true
		}
	}

	return Profile{}, false
}

func (s *ConfigStore) loadOrCreate() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.persist(s.config)
		}
		return fmt.Errorf("read config: %w", err)
	}

	if len(raw) == 0 {
		return s.persist(s.config)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	cfg = canonicalizeConfig(cfg)

	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	next := cloneConfig(cfg)
	payload, err := marshalConfig(next)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, payload) {
		if err := s.persist(next); err != nil {
			return err
		}
	}

	s.config = next
	return nil
}

func (s *ConfigStore) persist(cfg Config) error {
	payload, err := marshalConfig(cfg)
	if err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return fmt.Errorf("write tmp config: %w", err)
	}

	backupPath := s.path + ".bak"
	if _, statErr := os.Stat(s.path); statErr == nil {
		_ = os.Rename(s.path, backupPath)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Rename(backupPath, s.path) // restore backup on failure
		return fmt.Errorf("replace config: %w", err)
	}

	_ = os.Remove(backupPath)
	return nil
}

func marshalConfig(cfg Config) ([]byte, error) {
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return payload, nil
}

func ValidateConfig(cfg Config) error {
	seenIDs := map[string]struct{}{}

	for _, profile := range cfg.Profiles {
		if profile.ID == "" {
			return errors.New("profile id is required")
		}
		if _, exists := seenIDs[profile.ID]; exists {
			return fmt.Errorf("duplicate profile id: %s", profile.ID)
		}
		seenIDs[profile.ID] = struct{}{}
		if profile.Name == "" {
			return fmt.Errorf("profile %s missing name", profile.ID)
		}
	}

	return nil
}

func canonicalizeConfig(cfg Config) Config {
	if cfg.Profiles == nil {
		cfg.Profiles = []Profile{}
	}

	for i := range cfg.Profiles {
		if cfg.Profiles[i].WireGuard.DNS == nil {
			cfg.Profiles[i].WireGuard.DNS = []string{}
		}
	}

	return cfg
}

func cloneConfig(cfg Config) Config {
	out := Config{Profiles: make([]Profile, len(cfg.Profiles))}
	for i, profile := range cfg.Profiles {
		out.Profiles[i] = cloneProfile(profile)
	}
	return out
}

func cloneProfile(profile Profile) Profile {
	copyProfile := profile
	copyProfile.WireGuard.DNS = cloneStrings(profile.WireGuard.DNS)
	copyProfile.WireGuard.BypassHosts = cloneStrings(profile.WireGuard.BypassHosts)
	return copyProfile
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}
