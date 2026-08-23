package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// configSchemaVersion is bumped whenever Config/Profile gains fields an older
// build would silently drop on re-marshal. A file whose version is newer
// than this build understands is loaded as-is but never rewritten, so a
// downgrade or rollback can't destroy fields it can't parse.
const configSchemaVersion = 1

// configFile is the on-disk envelope around Config; it exists here (rather
// than as a field on Config in types.go) purely so persistence can carry a
// schema version without reaching into a file owned by another change.
type configFile struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

const staleLockAge = 10 * time.Second

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

	lock, err := acquireLock(s.path)
	if err != nil {
		return err
	}
	defer lock.release()

	s.cleanupStaleTemp()

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A missing primary with a surviving backup means a previous
			// process died mid-write; recover instead of assuming first run.
			if _, backupErr := os.Stat(s.path + ".bak"); backupErr == nil {
				return s.recoverFromBackup(errors.New("config file is missing"))
			}
			return s.persistLocked(s.config)
		}
		return fmt.Errorf("read config: %w", err)
	}

	if len(raw) == 0 {
		return s.recoverFromBackup(errors.New("config file is empty"))
	}

	cfg, rewrite, err := s.decodeConfig(raw)
	if err != nil {
		return s.recoverFromBackup(err)
	}

	s.config = cfg
	if rewrite {
		if err := s.persistLocked(cfg); err != nil {
			return err
		}
	}
	return nil
}

// decodeConfig parses raw into a Config, quarantining any profile that fails
// validation instead of failing the whole load, and reports whether the
// canonical re-marshal differs from raw (so the caller can self-heal the
// file on disk). It never asks for a rewrite of a file whose schema version
// is newer than this build understands.
func (s *ConfigStore) decodeConfig(raw []byte) (Config, bool, error) {
	var file configFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return Config{}, false, fmt.Errorf("parse config: %w", err)
	}

	cfg := canonicalizeConfig(Config{Profiles: file.Profiles})
	cfg.Profiles = quarantineInvalidProfiles(cfg.Profiles)

	if file.Version > configSchemaVersion {
		return cfg, false, nil
	}

	payload, err := marshalConfig(cfg)
	if err != nil {
		return Config{}, false, err
	}
	return cfg, !bytes.Equal(raw, payload), nil
}

// recoverFromBackup is reached when the primary config is empty or
// unparsable. It never falls back to DefaultConfig: the caller's data is
// either recovered from config.json.bak or the load fails outright.
func (s *ConfigStore) recoverFromBackup(cause error) error {
	raw, err := os.ReadFile(s.path + ".bak")
	if err != nil || len(raw) == 0 {
		return fmt.Errorf("config is corrupt and no usable backup exists: %w", cause)
	}

	cfg, _, err := s.decodeConfig(raw)
	if err != nil {
		return fmt.Errorf("config is corrupt and backup is also corrupt: %w", cause)
	}

	log.Printf("config: recovered from backup after primary config was unreadable: %v", cause)
	s.config = cfg
	return s.persistLocked(cfg)
}

// cleanupStaleTemp removes any ".tmp-*" file left behind by a persist that
// crashed before its rename; always safe since a fresh persist creates a new
// uniquely-named temp file.
func (s *ConfigStore) cleanupStaleTemp() {
	matches, _ := filepath.Glob(s.path + ".tmp-*")
	for _, match := range matches {
		_ = os.Remove(match)
	}
}

// persist takes the config lock and writes cfg to disk. Callers that already
// hold the lock (loadOrCreate) must call persistLocked directly.
func (s *ConfigStore) persist(cfg Config) error {
	lock, err := acquireLock(s.path)
	if err != nil {
		return err
	}
	defer lock.release()

	return s.persistLocked(cfg)
}

// persistLocked writes cfg via a uniquely-named temp file, fsyncs it,
// updates a backup copy of the previous config, then renames the temp file
// directly over the target (atomic on both POSIX and Windows, so there is
// never a moment with no config.json on disk) and fsyncs the directory.
func (s *ConfigStore) persistLocked(cfg Config) error {
	payload, err := marshalConfig(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp config: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("write tmp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync tmp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp config: %w", err)
	}

	if prev, err := os.ReadFile(s.path); err == nil && len(prev) > 0 {
		_ = os.WriteFile(s.path+".bak", prev, 0o600)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	removeTmp = false

	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

func marshalConfig(cfg Config) ([]byte, error) {
	payload, err := json.MarshalIndent(configFile{Version: configSchemaVersion, Profiles: cfg.Profiles}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return payload, nil
}

// fileLock is an exclusive-create lock file guarding the read-modify-write
// around config.json against a second daemon instance, installer, or CLI
// touching the same path concurrently.
type fileLock struct {
	path string
}

func acquireLock(path string) (*fileLock, error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)

	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return &fileLock{path: lockPath}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire config lock: %w", err)
		}

		// A lock file older than staleLockAge almost certainly belongs to a
		// process that crashed without releasing it; reclaim it.
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.Remove(lockPath)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire config lock: timed out waiting for %s", lockPath)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (l *fileLock) release() {
	_ = os.Remove(l.path)
}

const (
	minPort = 1
	maxPort = 65535
)

// portClaim is one transport's bid for a LocalPort, tracked so ValidateConfig
// can reject two transports (same profile or different profiles) binding the
// same port instead of letting them fail later inside transport startup.
type portClaim struct {
	transport string
	port      int
}

func ValidateConfig(cfg Config) error {
	seenIDs := map[string]struct{}{}

	for _, profile := range cfg.Profiles {
		if err := checkProfile(profile, seenIDs); err != nil {
			return err
		}
		registerProfile(profile, seenIDs)
	}

	return nil
}

// quarantineInvalidProfiles is the non-fatal counterpart to ValidateConfig,
// used only when loading from disk: a profile that fails validation is
// dropped and logged rather than aborting daemon startup.
func quarantineInvalidProfiles(profiles []Profile) []Profile {
	seenIDs := map[string]struct{}{}
	kept := make([]Profile, 0, len(profiles))

	for _, profile := range profiles {
		if err := checkProfile(profile, seenIDs); err != nil {
			log.Printf("config: quarantining invalid profile %q: %v", profile.ID, err)
			continue
		}
		registerProfile(profile, seenIDs)
		kept = append(kept, profile)
	}

	return kept
}

func checkProfile(profile Profile, seenIDs map[string]struct{}) error {
	if profile.ID == "" {
		return errors.New("profile id is required")
	}
	if _, exists := seenIDs[profile.ID]; exists {
		return fmt.Errorf("duplicate profile id: %s", profile.ID)
	}
	if profile.Name == "" {
		return fmt.Errorf("profile %s missing name", profile.ID)
	}
	if profile.WireGuard.ConfigText == "" {
		return fmt.Errorf("profile %s missing wireguard config", profile.ID)
	}

	claims, err := profilePortClaims(profile)
	if err != nil {
		return err
	}

	// Profiles and their transports are alternatives, never concurrent, so a
	// shared local port is legitimate; port 0 means "bind an ephemeral port".
	for _, claim := range claims {
		if claim.port == 0 {
			continue
		}
		if claim.port < minPort || claim.port > maxPort {
			return fmt.Errorf("profile %s %s local port %d out of range %d-%d",
				profile.ID, claim.transport, claim.port, minPort, maxPort)
		}
	}

	return nil
}

func registerProfile(profile Profile, seenIDs map[string]struct{}) {
	seenIDs[profile.ID] = struct{}{}
}

// profilePortClaims lists the ports a profile's configured transports bind
// to, requiring a remote host (or, for Snowflake, a broker URL) on any
// transport it considers configured. Cloak is only checked when RemoteHost
// is set: profiles that rely solely on another transport, or on the direct
// WireGuard method, legitimately leave it empty.
func profilePortClaims(profile Profile) ([]portClaim, error) {
	var claims []portClaim

	if profile.Cloak.RemoteHost != "" {
		if profile.Cloak.RemotePort <= 0 {
			return nil, fmt.Errorf("profile %s cloak missing remote port", profile.ID)
		}
		claims = append(claims, portClaim{"cloak", profile.Cloak.LocalPort})
	}
	if profile.Naive != nil {
		if profile.Naive.RemoteHost == "" {
			return nil, fmt.Errorf("profile %s naive missing remote host", profile.ID)
		}
		claims = append(claims, portClaim{"naive", profile.Naive.LocalPort})
	}
	if profile.Reality != nil {
		if profile.Reality.RemoteHost == "" {
			return nil, fmt.Errorf("profile %s reality missing remote host", profile.ID)
		}
		claims = append(claims, portClaim{"reality", profile.Reality.LocalPort})
	}
	if profile.Hysteria2 != nil {
		if profile.Hysteria2.RemoteHost == "" {
			return nil, fmt.Errorf("profile %s hysteria2 missing remote host", profile.ID)
		}
		claims = append(claims, portClaim{"hysteria2", profile.Hysteria2.LocalPort})
	}
	if profile.Shadowsocks != nil {
		if profile.Shadowsocks.RemoteHost == "" {
			return nil, fmt.Errorf("profile %s shadowsocks missing remote host", profile.ID)
		}
		claims = append(claims, portClaim{"shadowsocks", profile.Shadowsocks.LocalPort})
	}
	if profile.Snowflake != nil {
		if profile.Snowflake.BrokerURL == "" {
			return nil, fmt.Errorf("profile %s snowflake missing broker url", profile.ID)
		}
		claims = append(claims, portClaim{"snowflake", profile.Snowflake.LocalPort})
	}

	return claims, nil
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
	copyProfile.TransportEndpointIPs = cloneStrings(profile.TransportEndpointIPs)
	copyProfile.Naive = cloneNaiveProfile(profile.Naive)
	copyProfile.Reality = cloneRealityProfile(profile.Reality)
	copyProfile.Hysteria2 = cloneHysteria2Profile(profile.Hysteria2)
	copyProfile.Shadowsocks = cloneShadowsocksProfile(profile.Shadowsocks)
	copyProfile.Snowflake = cloneSnowflakeProfile(profile.Snowflake)
	return copyProfile
}

// cloneNaiveProfile returns an independent copy of profile behind a fresh
// pointer, or nil if profile is nil. NaiveProfile is a flat struct (no
// nested pointers/slices), so a plain value copy is sufficient — but without
// this, cloneProfile would leave the returned Profile's Naive pointer
// aliasing the config store's own internal *NaiveProfile, letting a caller
// mutate the store's state without its lock.
func cloneNaiveProfile(profile *NaiveProfile) *NaiveProfile {
	if profile == nil {
		return nil
	}
	copyProfile := *profile
	return &copyProfile
}

// cloneRealityProfile mirrors cloneNaiveProfile: RealityProfile is flat
// (scalar fields only), so a value copy behind a fresh pointer is enough.
func cloneRealityProfile(profile *RealityProfile) *RealityProfile {
	if profile == nil {
		return nil
	}
	copyProfile := *profile
	return &copyProfile
}

// cloneHysteria2Profile mirrors cloneNaiveProfile: Hysteria2Profile is flat
// (scalar fields only), so a value copy behind a fresh pointer is enough.
func cloneHysteria2Profile(profile *Hysteria2Profile) *Hysteria2Profile {
	if profile == nil {
		return nil
	}
	copyProfile := *profile
	return &copyProfile
}

// cloneShadowsocksProfile mirrors cloneNaiveProfile: ShadowsocksProfile is
// flat (scalar fields only), so a value copy behind a fresh pointer is enough.
func cloneShadowsocksProfile(profile *ShadowsocksProfile) *ShadowsocksProfile {
	if profile == nil {
		return nil
	}
	copyProfile := *profile
	return &copyProfile
}

// cloneSnowflakeProfile mirrors cloneNaiveProfile but also copies the
// FrontDomains and ICEServers slices into fresh slices so the returned
// profile shares no backing array with the config store's state.
func cloneSnowflakeProfile(profile *SnowflakeProfile) *SnowflakeProfile {
	if profile == nil {
		return nil
	}
	copyProfile := *profile
	copyProfile.FrontDomains = cloneStrings(profile.FrontDomains)
	copyProfile.ICEServers = cloneStrings(profile.ICEServers)
	return &copyProfile
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}
