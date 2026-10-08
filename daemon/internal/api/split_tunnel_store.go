package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

const splitTunnelFile = "split-tunnel.json"

const splitTunnelSchemaVersion = 1

const (
	maxSplitCIDRs = 64
	// maxSplitInput bounds what validation looks at before deduplicating.
	maxSplitInput     = 1024
	minSplitCIDRBits  = 8
	maxSplitRoutes    = 1024
	maxSplitProtect   = 8
	maxSplitPathBytes = 1024
)

// CIDR error codes are stable: the desktop app maps them to messages.
const (
	splitCodeNotIPv4        = "notIPv4"
	splitCodePrefixTooShort = "prefixTooShort"
	splitCodeTooMany        = "tooMany"
	splitCodeTooManyRoutes  = "tooManyRoutes"
)

// splitTunnelSettings is split-tunnel.json. It is daemon-wide: every account on the
// machine follows it. Apps keep the case they were picked in.
type splitTunnelSettings struct {
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
	Apps    []string `json:"apps"`
	CIDRs   []string `json:"cidrs"`
	Protect []string `json:"protect,omitempty"`
}

func (c splitTunnelSettings) clone() splitTunnelSettings {
	c.Apps = append([]string{}, c.Apps...)
	c.CIDRs = append([]string{}, c.CIDRs...)
	c.Protect = append([]string(nil), c.Protect...)
	return c
}

// effectiveApps is what the engine excludes: nothing while split tunnelling is off.
func (c splitTunnelSettings) effectiveApps() []string {
	if !c.Enabled {
		return nil
	}
	return c.Apps
}

// effectiveCIDRs is what the routes and the lock carve out; unparsable entries never get here.
func (c splitTunnelSettings) effectiveCIDRs() []netip.Prefix {
	if !c.Enabled {
		return nil
	}
	out := make([]netip.Prefix, 0, len(c.CIDRs))
	for _, raw := range c.CIDRs {
		if p, code := parseSplitCIDR(raw); code == "" {
			out = append(out, p)
		}
	}
	return sortedPrefixes(out)
}

// splitTunnelStore owns split-tunnel.json. gen moves on every successful save, so the
// service can tell whether the live session runs on the latest settings.
type splitTunnelStore struct {
	path string
	mu   sync.RWMutex
	cfg  splitTunnelSettings
	gen  uint64
}

// SplitTunnelStorePath is where the daemon keeps the split-tunnel settings.
func SplitTunnelStorePath() (string, error) {
	dir, err := platform.AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, splitTunnelFile), nil
}

// splitTunnelPathFn is replaced by tests to avoid touching the real install.
var splitTunnelPathFn = SplitTunnelStorePath

// ForgetSplitTunnel removes the stored settings for an uninstall: the excluded app
// paths name the user's software and must not outlive the product.
func ForgetSplitTunnel() error {
	path, err := splitTunnelPathFn()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove split tunnel settings: %w", err)
	}
	return nil
}

// openSplitTunnelStore always returns a usable store. A file it cannot read is
// reported and treated as split tunnelling off; the next save replaces it.
func openSplitTunnelStore(path string) (*splitTunnelStore, error) {
	st := &splitTunnelStore{path: path, gen: 1, cfg: splitTunnelSettings{Version: splitTunnelSchemaVersion, Apps: []string{}, CIDRs: []string{}}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("read split tunnel settings: %w", err)
	}
	var cfg splitTunnelSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return st, fmt.Errorf("split tunnel settings are unreadable, so split tunnelling is off: %w", err)
	}
	var valid []string
	for _, raw := range cfg.CIDRs {
		if p, code := parseSplitCIDR(raw); code == "" {
			valid = append(valid, p.String())
		}
	}
	cidrs, invalid := normalizeSplitCIDRList(valid)
	switch {
	case len(invalid) > 0:
		cidrs = valid
		cfg.Enabled = false
		err = fmt.Errorf("stored split tunnel ranges exceed the limits (%s), so split tunnelling is off", invalid[0].Code)
	case len(valid) < len(cfg.CIDRs):
		err = fmt.Errorf("dropped %d unreadable stored split tunnel ranges", len(cfg.CIDRs)-len(valid))
	}
	cfg.CIDRs = cidrs
	cfg.Apps = dedupeSplitApps(cfg.Apps)
	cfg.Version = splitTunnelSchemaVersion
	st.cfg = cfg.clone()
	return st, err
}

func (st *splitTunnelStore) snapshot() (splitTunnelSettings, uint64) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.cfg.clone(), st.gen
}

func (st *splitTunnelStore) generation() uint64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.gen
}

// save persists cfg and only then makes it current, so a failed write changes nothing.
func (st *splitTunnelStore) save(cfg splitTunnelSettings) (uint64, error) {
	cfg = cfg.clone()
	cfg.Version = splitTunnelSchemaVersion
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal split tunnel settings: %w", err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := writeFileAtomic(st.path, data); err != nil {
		return st.gen, err
	}
	st.cfg = cfg
	st.gen++
	return st.gen, nil
}

func writeFileAtomic(path string, data []byte) error {
	if path == "" {
		return errors.New("no settings path")
	}
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.%d.tmp", filepath.Base(path), os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create settings temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write settings: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync settings: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close settings: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename settings: %w", err)
	}
	return nil
}

// splitTunnelInvalid names one rejected entry by its index in the list that was
// sent; the entry itself is never echoed back.
type splitTunnelInvalid struct {
	Field string `json:"field"`
	Index int    `json:"index"`
	Code  string `json:"code"`
}

// parseSplitCIDR accepts an IPv4 prefix or a bare address (taken as /32), masked to
// its canonical form.
func parseSplitCIDR(raw string) (netip.Prefix, string) {
	s := strings.TrimSpace(raw)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil || !p.Addr().Is4() {
			return netip.Prefix{}, splitCodeNotIPv4
		}
		if p.Bits() < minSplitCIDRBits {
			return netip.Prefix{}, splitCodePrefixTooShort
		}
		return p.Masked(), ""
	}
	addr, err := netip.ParseAddr(s)
	if err != nil || !addr.Is4() {
		return netip.Prefix{}, splitCodeNotIPv4
	}
	return netip.PrefixFrom(addr, 32), ""
}

// normalizeSplitCIDRList checks every entry, then drops duplicates and ranges another
// entry covers. Limits are on what survives, and on the routes it would cost.
func normalizeSplitCIDRList(entries []string) ([]string, []splitTunnelInvalid) {
	if len(entries) > maxSplitInput {
		return nil, []splitTunnelInvalid{{Field: "cidrs", Index: maxSplitInput, Code: splitCodeTooMany}}
	}
	var invalid []splitTunnelInvalid
	prefixes := make([]netip.Prefix, 0, len(entries))
	origin := make([]int, 0, len(entries))
	for i, raw := range entries {
		p, code := parseSplitCIDR(raw)
		if code != "" {
			invalid = append(invalid, splitTunnelInvalid{Field: "cidrs", Index: i, Code: code})
			continue
		}
		prefixes = append(prefixes, p)
		origin = append(origin, i)
	}
	if len(invalid) > 0 {
		return nil, invalid
	}
	kept, keptOrigin := reduceCoveredPrefixes(prefixes, origin)
	if len(kept) > maxSplitCIDRs {
		return nil, []splitTunnelInvalid{{Field: "cidrs", Index: keptOrigin[maxSplitCIDRs], Code: splitCodeTooMany}}
	}
	if len(kept) > 0 && wg.CountExcludeRoutes(kept) > maxSplitRoutes {
		return nil, []splitTunnelInvalid{{Field: "cidrs", Index: keptOrigin[0], Code: splitCodeTooManyRoutes}}
	}
	out := make([]string, 0, len(kept))
	for _, p := range kept {
		out = append(out, p.String())
	}
	return out, nil
}

// reduceCoveredPrefixes keeps input order, the first of any duplicates, and no range
// that a wider entry already contains.
func reduceCoveredPrefixes(prefixes []netip.Prefix, origin []int) ([]netip.Prefix, []int) {
	var kept []netip.Prefix
	var keptOrigin []int
	for i, p := range prefixes {
		covered := false
		for j, q := range prefixes {
			if i == j {
				continue
			}
			if (q.Bits() < p.Bits() && q.Contains(p.Addr())) || (q == p && j < i) {
				covered = true
				break
			}
		}
		if !covered {
			kept = append(kept, p)
			keptOrigin = append(keptOrigin, origin[i])
		}
	}
	return kept, keptOrigin
}

// splitAppKey mirrors the desktop's comparison: Windows and macOS paths are case-insensitive.
func splitAppKey(goos, rule string) string {
	switch goos {
	case "windows":
		return strings.ToLower(strings.ReplaceAll(rule, "/", `\`))
	case "darwin":
		lower := strings.ToLower(rule)
		if trimmed := strings.TrimRight(lower, "/"); strings.HasSuffix(trimmed, ".app") {
			return trimmed
		}
		return lower
	}
	return rule
}

func dedupeSplitApps(apps []string) []string {
	out := make([]string, 0, len(apps))
	seen := make(map[string]struct{}, len(apps))
	for _, app := range apps {
		key := splitAppKey(runtime.GOOS, app)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, app)
	}
	return out
}

// compileSplitApps checks app entries against the per-OS policy and the protected
// images; indirected so tests can stand in for the host's policy.
var compileSplitApps = procmatch.ValidateRules

func validateSplitApps(apps, neverBypass []string) []splitTunnelInvalid {
	if len(apps) > maxSplitInput {
		return []splitTunnelInvalid{{Field: "apps", Index: maxSplitInput, Code: procmatch.CodeTooMany}}
	}
	var invalid []splitTunnelInvalid
	for _, e := range compileSplitApps(apps, neverBypass) {
		invalid = append(invalid, splitTunnelInvalid{Field: "apps", Index: e.Index, Code: e.Code})
	}
	slices.SortFunc(invalid, func(a, b splitTunnelInvalid) int { return a.Index - b.Index })
	return invalid
}

// validateSplitProtect checks the images the client asks to keep in the tunnel.
func validateSplitProtect(entries []string) []splitTunnelInvalid {
	if len(entries) > maxSplitProtect {
		return []splitTunnelInvalid{{Field: "protect", Index: maxSplitProtect, Code: procmatch.CodeTooMany}}
	}
	var invalid []splitTunnelInvalid
	for i, entry := range entries {
		code := ""
		switch {
		case strings.ContainsRune(entry, 0):
			code = procmatch.CodeNUL
		case len(entry) > maxSplitPathBytes:
			code = procmatch.CodeTooLong
		case !filepath.IsAbs(entry):
			code = procmatch.CodeNotAbsolute
		}
		if code != "" {
			invalid = append(invalid, splitTunnelInvalid{Field: "protect", Index: i, Code: code})
		}
	}
	return invalid
}
