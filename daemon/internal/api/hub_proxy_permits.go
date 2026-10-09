package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// hubProxyPermitFile lists the IPs the lock permits only because a hub proxy was
// running; the proxy dies with the daemon, so a restart drops them.
const hubProxyPermitFile = "hub-proxy-permits.json"

var hubProxyPermitsMu sync.Mutex

// Indirected so tests keep the record in memory, like the session record.
var saveHubProxyPermits = saveHubProxyPermitsToDisk
var loadHubProxyPermits = loadHubProxyPermitsFromDisk

func hubProxyPermitsPath() (string, error) {
	dir, err := platform.AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hubProxyPermitFile), nil
}

func saveHubProxyPermitsToDisk(ips []string) error {
	hubProxyPermitsMu.Lock()
	defer hubProxyPermitsMu.Unlock()
	path, err := hubProxyPermitsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(ips)
	if err != nil {
		return fmt.Errorf("marshal hub proxy permits: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write hub proxy permits: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace hub proxy permits: %w", err)
	}
	return nil
}

func loadHubProxyPermitsFromDisk() ([]string, error) {
	hubProxyPermitsMu.Lock()
	defer hubProxyPermitsMu.Unlock()
	path, err := hubProxyPermitsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hub proxy permits: %w", err)
	}
	var ips []string
	if err := json.Unmarshal(data, &ips); err != nil {
		return nil, fmt.Errorf("parse hub proxy permits: %w", err)
	}
	return ips, nil
}

// recordHubProxyPermits replaces the record; a failed write only means a crash
// could leave these nodes permitted until the next Enable, so it is logged.
func (s *Service) recordHubProxyPermits(ips []string) {
	if err := saveHubProxyPermits(ips); err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not record hub proxy permits: %v", err))
	}
}

// noteHubProxyPermit adds a hub proxy's node to the record unless the session or
// the control plane needs that address anyway.
func (s *Service) noteHubProxyPermit(host string) {
	ips := ipLiterals([]string{host})
	if len(ips) == 0 || slices.Contains(s.permitsKeptAcrossRestart(), ips[0]) {
		return
	}
	current, err := loadHubProxyPermits()
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not read hub proxy permits: %v", err))
	}
	if !slices.Contains(current, ips[0]) {
		s.recordHubProxyPermits(append(current, ips[0]))
	}
}

// permitsKeptAcrossRestart are the addresses a restart must keep permitted: the
// control plane, and the live or recorded session's own endpoints.
func (s *Service) permitsKeptAcrossRestart() []string {
	keep := ipLiterals(s.storedControlPlaneHosts())
	if profile, ok := s.getCurrentProfile(); ok {
		keep = append(keep, ipLiterals(killSwitchPermits(profile))...)
	}
	if record, err := loadSessionRecord(); err == nil && record.ProfileID != "" {
		if profile, found := s.config.FindProfile(record.ProfileID); found {
			keep = append(keep, ipLiterals(killSwitchPermits(profile))...)
		}
	}
	return keep
}

// withoutHubProxyPermits drops recorded hub-proxy-only addresses from a lock the
// previous process left, then clears the record: no hub proxy survived it.
func (s *Service) withoutHubProxyPermits(endpoints []string) []string {
	recorded, err := loadHubProxyPermits()
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not read hub proxy permits: %v", err))
		return endpoints
	}
	if len(recorded) == 0 {
		return endpoints
	}
	keep := s.permitsKeptAcrossRestart()
	out := make([]string, 0, len(endpoints))
	for _, ip := range endpoints {
		if slices.Contains(recorded, ip) && !slices.Contains(keep, ip) {
			continue
		}
		out = append(out, ip)
	}
	s.recordHubProxyPermits(nil)
	return out
}
