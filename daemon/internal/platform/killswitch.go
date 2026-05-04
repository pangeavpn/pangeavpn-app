package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// KillSwitch enforces a network lock that blocks all outbound traffic except
// loopback and the VPN transport endpoint. It is enabled automatically during
// the connect flow, kept active on connect failure (fail-closed), and cleared
// only by an explicit disconnect.
type KillSwitch interface {
	// Enable resolves the endpoint host and blocks all outbound traffic
	// except loopback and the resolved endpoint IP(s). If allowLAN is true,
	// local-network IPv4 ranges (RFC1918, link-local, multicast, broadcast)
	// are also permitted so captive-portal re-checks and gateway liveness
	// probes on restrictive WiFi don't trip Windows into "No internet".
	Enable(ctx context.Context, endpointHost string, allowLAN bool) error

	// Update adds an allow rule for the active tunnel interface so that
	// VPN-routed traffic can egress.
	Update(ctx context.Context, tunnelInterface string) error

	// Clear removes all kill-switch rules and restores the previous
	// network policy. Returns an error if restoration fails.
	Clear(ctx context.Context) error

	// Active reports whether the kill switch is currently engaged.
	Active() bool
}

// LANAllowPrefixes are the IPv4 ranges the kill switch permits when
// allowLAN is set. Keep in sync with wg.LANExcludePrefixes — traffic that
// leaves the tunnel (because AllowedIPs excludes these) must also be
// allowed by the firewall.
var LANAllowPrefixes = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"224.0.0.0/4",
	"255.255.255.255/32",
}

// KillSwitchState is persisted to disk so that crash/startup reconciliation
// can restore normal networking or re-apply the lock.
type KillSwitchState struct {
	Active          bool              `json:"active"`
	AllowLAN        bool              `json:"allowLAN,omitempty"`
	EndpointIPs     []string          `json:"endpointIPs"`
	TunnelInterface string            `json:"tunnelInterface,omitempty"`
	PreviousPolicy  map[string]string `json:"previousPolicy,omitempty"`
}

const killSwitchStateFile = "killswitch-state.json"

// Set by platform-specific files (killswitch_windows.go, killswitch_darwin.go, killswitch_linux.go).
var newPlatformKillSwitch func() KillSwitch

var lookupResolverIP = func(ctx context.Context, network, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, network, host)
}

// NewKillSwitch returns a platform-appropriate kill-switch implementation.
func NewKillSwitch() KillSwitch {
	if newPlatformKillSwitch != nil {
		return newPlatformKillSwitch()
	}
	return &noopKillSwitch{}
}

// noopKillSwitch is used on platforms without a kill-switch backend.
type noopKillSwitch struct{}

func (n *noopKillSwitch) Enable(_ context.Context, _ string, _ bool) error { return nil }
func (n *noopKillSwitch) Update(_ context.Context, _ string) error         { return nil }
func (n *noopKillSwitch) Clear(_ context.Context) error                    { return nil }
func (n *noopKillSwitch) Active() bool                                     { return false }

// ---------------------------------------------------------------------------
// Shared helpers for state persistence
// ---------------------------------------------------------------------------

var stateMu sync.Mutex

func killSwitchStatePath() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, killSwitchStateFile), nil
}

func saveKillSwitchState(st KillSwitchState) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	path, err := killSwitchStatePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal kill switch state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write kill switch state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename kill switch state: %w", err)
	}
	return nil
}

func loadKillSwitchState() (KillSwitchState, error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	path, err := killSwitchStatePath()
	if err != nil {
		return KillSwitchState{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return KillSwitchState{}, nil
		}
		return KillSwitchState{}, fmt.Errorf("read kill switch state: %w", err)
	}

	var st KillSwitchState
	if err := json.Unmarshal(data, &st); err != nil {
		return KillSwitchState{}, fmt.Errorf("unmarshal kill switch state: %w", err)
	}
	return st, nil
}

func removeKillSwitchState() error {
	stateMu.Lock()
	defer stateMu.Unlock()

	path, err := killSwitchStatePath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove kill switch state: %w", err)
	}
	return nil
}

// LoadKillSwitchStatePublic is the exported accessor for reconciliation in
// other packages (e.g. api).
func LoadKillSwitchStatePublic() (KillSwitchState, error) {
	return loadKillSwitchState()
}

// resolveEndpointIPs resolves a hostname or IP string to a deduplicated,
// sorted list of IP strings suitable for firewall rules.
func resolveEndpointIPs(ctx context.Context, host string) ([]string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("empty endpoint host")
	}

	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return []string{v4.String()}, nil
		}
		return nil, fmt.Errorf("endpoint %s is IPv6; only IPv4 endpoints are supported", host)
	}

	ips, err := lookupResolverIP(ctx, "ip4", host)
	if err != nil {
		return nil, fmt.Errorf("resolve endpoint %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("endpoint %s resolved to no IPv4 addresses", host)
	}

	seen := make(map[string]struct{}, len(ips))
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		s := v4.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("endpoint %s resolved to no IPv4 addresses", host)
	}
	sort.Strings(out)
	return out, nil
}
