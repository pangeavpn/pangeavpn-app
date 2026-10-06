package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// KillSwitch enforces a network lock that blocks all outbound traffic except
// loopback and the VPN transport endpoint; only an explicit disconnect clears it.
type KillSwitch interface {
	// Enable blocks outbound traffic except loopback and resolved endpointHost IPs
	// (empty hosts = pure block-all Lockdown); re-entrant, and locked persists.
	Enable(ctx context.Context, endpointHosts []string, allowLAN bool, locked bool) error

	// Update adds an allow rule for the active tunnel interface so that
	// VPN-routed traffic can egress.
	Update(ctx context.Context, tunnel TunnelRef) error

	// Clear removes all kill-switch rules and restores the previous
	// network policy. Returns an error if restoration fails.
	Clear(ctx context.Context) error

	// Active reports whether the kill switch is currently engaged.
	Active() bool
}

// TunnelRef scopes a permit to an adapter: Name is what pf/nftables match on;
// WindowsLUID avoids a by-name lookup racing a rebuild that reuses the name.
type TunnelRef struct {
	Name        string
	WindowsLUID uint64
}

// LANAllowPrefixes are the ranges the kill switch permits when allowLAN is
// set. Keep in sync with wg.LANExcludePrefixes.
var LANAllowPrefixes = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"224.0.0.0/4",
	"255.255.255.255/32",
	"100.64.0.0/10",
}

// LANAllowPrefixesV6 are the IPv6 analogues. Kept separate because the
// Windows and Linux kill switches only build IPv4 rules from a prefix list.
var LANAllowPrefixesV6 = []string{
	"fe80::/10",
	"ff02::/16",
	"fc00::/7",
}

// IsLANAllowAddress reports whether ip falls inside the Allow-LAN ranges.
func IsLANAllowAddress(ip net.IP) bool {
	prefixes := LANAllowPrefixes
	if ip.To4() == nil {
		prefixes = LANAllowPrefixesV6
	}
	for _, cidr := range prefixes {
		if _, network, err := net.ParseCIDR(cidr); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// KillSwitchState is persisted to disk so that crash/startup reconciliation
// can restore normal networking or re-apply the lock.
type KillSwitchState struct {
	Active          bool     `json:"active"`
	AllowLAN        bool     `json:"allowLAN,omitempty"`
	EndpointIPs     []string `json:"endpointIPs"`
	TunnelInterface string   `json:"tunnelInterface,omitempty"`
	// Locked marks an intentional Lockdown lock that must survive daemon
	// restarts rather than being cleared as stale crash leftover.
	Locked bool `json:"locked,omitempty"`
}

const killSwitchStateFile = "killswitch-state.json"

// Set by platform-specific files (killswitch_windows.go, killswitch_darwin.go, killswitch_linux.go).
var newPlatformKillSwitch func() KillSwitch

var lookupResolverIP = func(ctx context.Context, network, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, network, host)
}

// endpointResolveTimeout bounds one Enable's DNS pass: behind an armed lock,
// port 53 is blackholed and an unbounded resolve hangs the caller's mutexes.
var endpointResolveTimeout = 15 * time.Second

// EndpointResolveWarn, when set, is called for each endpoint host that could
// not be resolved and was therefore skipped. Wired to the daemon log store.
var EndpointResolveWarn func(host string, err error)

// KillSwitchWarnf reports a degraded-but-still-closed kill switch (stale permit
// left behind, incomplete teardown). Wired to the daemon log store.
var KillSwitchWarnf func(format string, args ...any)

func KillSwitchWarn(format string, args ...any) {
	if warn := KillSwitchWarnf; warn != nil {
		warn(format, args...)
	}
}

// KillSwitchInfof records a host setting the lock changed or gave back, so an
// incident log shows what was in force. Wired to the daemon log store.
var KillSwitchInfof func(format string, args ...any)

func KillSwitchInfo(format string, args ...any) {
	if info := KillSwitchInfof; info != nil {
		info(format, args...)
	}
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

func (n *noopKillSwitch) Enable(_ context.Context, _ []string, _ bool, _ bool) error { return nil }
func (n *noopKillSwitch) Update(_ context.Context, _ TunnelRef) error                { return nil }
func (n *noopKillSwitch) Clear(_ context.Context) error                              { return nil }
func (n *noopKillSwitch) Active() bool                                               { return false }

var stateMu sync.Mutex

// killSwitchStatePathFn is replaced by tests: AppSupportDir ignores its env
// override when privileged, which would share the real installation's file.
var killSwitchStatePathFn = defaultKillSwitchStatePath

func killSwitchStatePath() (string, error) {
	return killSwitchStatePathFn()
}

func defaultKillSwitchStatePath() (string, error) {
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

	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.%d.tmp", killSwitchStateFile, os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create kill switch state temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write kill switch state: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync kill switch state: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close kill switch state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename kill switch state: %w", err)
	}
	// Best-effort: fsyncing the directory entry isn't supported on Windows.
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

// ErrKillSwitchStateUnreadable distinguishes "state file absent" (nothing was
// ever engaged) from "present but unreadable" (a stuck lock may still be live).
var ErrKillSwitchStateUnreadable = errors.New("kill switch state file exists but could not be read")

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
		return KillSwitchState{}, fmt.Errorf("%w: %v", ErrKillSwitchStateUnreadable, err)
	}

	var st KillSwitchState
	if err := json.Unmarshal(data, &st); err != nil {
		return KillSwitchState{}, fmt.Errorf("%w: unmarshal: %v", ErrKillSwitchStateUnreadable, err)
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

// persistLockedUpgrade records a Lockdown flag change on a re-arm that needs no
// rule change. locked is the caller's desired state, not one echoed from disk.
func persistLockedUpgrade(prev KillSwitchState, locked bool) error {
	if prev.Locked == locked {
		return nil
	}
	prev.Locked = locked
	if err := saveKillSwitchState(prev); err != nil {
		return fmt.Errorf("kill switch enable: record lockdown: %w", err)
	}
	return nil
}

// updateTunnelInterfaceState records the tunnel interface permitted through an
// already-engaged kill switch. Callers must check err, not fall back to prev.
func updateTunnelInterfaceState(tunnel string) (KillSwitchState, error) {
	prev, err := loadKillSwitchState()
	if err != nil {
		return KillSwitchState{}, fmt.Errorf("read kill switch state before tunnel update: %w", err)
	}
	prev.TunnelInterface = tunnel
	if err := saveKillSwitchState(prev); err != nil {
		return KillSwitchState{}, err
	}
	return prev, nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// resolveEndpointHosts resolves each host to IPs, dedups and sorts, skipping
// (and warning on) failures — falling back to/unioning with the persisted set.
func resolveEndpointHosts(ctx context.Context, hosts []string) ([]string, error) {
	if len(hosts) == 0 {
		// No endpoints to permit: caller wants a pure block-all lock
		// (e.g. Lockdown engaged while disconnected). Not an error.
		return nil, nil
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, endpointResolveTimeout)
		defer cancel()
	}
	seen := make(map[string]struct{}, len(hosts))
	out := make([]string, 0, len(hosts))
	anyFailed := false
	for _, host := range hosts {
		ips, err := resolveEndpointIPs(ctx, host)
		if err != nil {
			anyFailed = true
			if warn := EndpointResolveWarn; warn != nil {
				warn(host, err)
			}
			continue
		}
		for _, ip := range ips {
			if _, ok := seen[ip]; ok {
				continue
			}
			seen[ip] = struct{}{}
			out = append(out, ip)
		}
	}
	if !anyFailed {
		sort.Strings(out)
		return out, nil
	}

	prev, _ := loadKillSwitchState()
	if len(out) == 0 {
		if len(prev.EndpointIPs) == 0 {
			return nil, fmt.Errorf("no IPs resolved from endpoint hosts %v", hosts)
		}
		return append([]string(nil), prev.EndpointIPs...), nil
	}
	for _, ip := range prev.EndpointIPs {
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	sort.Strings(out)
	return out, nil
}

// resolveEndpointIPs resolves a hostname or IP string (v4 or v6) to a
// deduplicated, sorted list of IP strings suitable for firewall rules.
func resolveEndpointIPs(ctx context.Context, host string) ([]string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("empty endpoint host")
	}

	if ip := net.ParseIP(host); ip != nil {
		return []string{canonicalIPString(ip)}, nil
	}

	ips, err := lookupResolverIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve endpoint %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("endpoint %s resolved to no addresses", host)
	}

	seen := make(map[string]struct{}, len(ips))
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		s := canonicalIPString(ip)
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("endpoint %s resolved to no addresses", host)
	}
	sort.Strings(out)
	return out, nil
}

// canonicalIPString renders an IPv4-mapped address in dotted form rather
// than the ::ffff:a.b.c.d form net.IP.String() would otherwise produce.
func canonicalIPString(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}
