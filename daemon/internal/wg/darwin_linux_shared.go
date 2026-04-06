//go:build darwin || linux || windows

package wg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// wireGuardGoManager is the in-process WireGuard backend for supported OSes.
// It creates a TUN device and WireGuard device entirely within the daemon
// process using the imported wireguard-go packages.
type wireGuardGoManager struct {
	logs     *state.LogStore
	mu       sync.Mutex
	sessions map[string]*tunnelSession
}

// tunnelSession holds state for an active in-process WireGuard tunnel.
type tunnelSession struct {
	interfaceName string
	device        *device.Device
	tunDevice     tun.Device

	// Networking state for cleanup.
	endpointRoutes   []routeSpec
	dnsOverrides     []darwinDNSOverride // macOS
	linuxDNSOverride *linuxDNSOverride   // Linux
	windowsLUID      uint64              // Windows
	windowsRoutes    []windowsRouteSpec  // Windows endpoint bypass routes
}

type routeSpec struct {
	family      string // "inet" or "inet6"
	destination string
}

type darwinDNSOverride struct {
	service    string
	dnsServers []string
}

type windowsRouteSpec struct {
	interfaceLUID uint64
	destination   string // CIDR prefix
	nextHop       string // Gateway IP
}

type linuxDNSMode string

const (
	linuxDNSModeResolvedLink linuxDNSMode = "resolved_link"
	linuxDNSModeResolvConf   linuxDNSMode = "resolv_conf"
)

type linuxDNSOverride struct {
	mode           linuxDNSMode
	interfaceName  string
	resolvConfPath string
	resolvConfData []byte
	resolvConfMode os.FileMode
	resolvConfHad  bool
}

type parsedUserlandConfig struct {
	wgConfig      string   // Stripped INI with only WireGuard keys.
	addresses     []string // CIDR addresses from [Interface] Address.
	dnsServers    []string // DNS servers from [Interface] DNS.
	mtu           int      // MTU from [Interface] MTU, 0 if unset.
	endpointHosts []string // Hostnames/IPs extracted from [Peer] Endpoint.
}

type tunFactory func(interfaceName string, mtu int) (tun.Device, error)

func newWireGuardGoManager(logs *state.LogStore) *wireGuardGoManager {
	return &wireGuardGoManager{
		logs:     logs,
		sessions: make(map[string]*tunnelSession),
	}
}

func init() {
	newPlatformManager = func(logs *state.LogStore) Manager {
		return newWireGuardGoManager(logs)
	}
}

func (m *wireGuardGoManager) Preflight(_ context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.ConfigText) == "" {
		return errors.New("wireguard configText is required")
	}

	parsed, err := parseUserlandConfig(profile.ConfigText)
	if err != nil {
		return err
	}
	parsed.dnsServers = mergeDNSServers(parsed.dnsServers, profile.DNS)

	if _, err := validateParsedIPv4Only(parsed); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// In-process device lifecycle
// ---------------------------------------------------------------------------

// createInProcessDevice creates a TUN device and WireGuard device, applies
// the UAPI configuration, and brings the device up. The caller is responsible
// for platform-specific address/route/DNS configuration on the returned
// interface.
func (m *wireGuardGoManager) createInProcessDevice(
	interfaceName string,
	mtu int,
	wgConfig string,
) (*device.Device, tun.Device, error) {
	return m.createInProcessDeviceWithFactory(interfaceName, mtu, wgConfig, tun.CreateTUN)
}

func (m *wireGuardGoManager) createInProcessDeviceWithFactory(
	interfaceName string,
	mtu int,
	wgConfig string,
	createTUN tunFactory,
) (*device.Device, tun.Device, error) {
	if mtu <= 0 {
		mtu = device.DefaultMTU
	}

	tunDev, err := createTUN(interfaceName, mtu)
	if err != nil {
		return nil, nil, fmt.Errorf("create tun device %s: %w", interfaceName, err)
	}

	logger := newWGLogger(m.logs)
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)

	uapi, err := wgConfigToUAPI(wgConfig)
	if err != nil {
		dev.Close()
		return nil, nil, fmt.Errorf("translate config to uapi: %w", err)
	}

	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, nil, fmt.Errorf("apply uapi config: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, nil, fmt.Errorf("bring device up: %w", err)
	}

	return dev, tunDev, nil
}

// closeDevice safely shuts down a WireGuard device. Safe to call with nil.
func closeDevice(dev *device.Device) {
	if dev != nil {
		dev.Close()
	}
}

// ---------------------------------------------------------------------------
// Session management
// ---------------------------------------------------------------------------

func (m *wireGuardGoManager) session(tunnelKey string) (*tunnelSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[tunnelKey]
	return s, ok
}

// peerTransferStats reads aggregate rx/tx bytes from all peers via UAPI.
func peerTransferStats(dev *device.Device) (rxBytes, txBytes int64) {
	if dev == nil {
		return 0, 0
	}
	ipcData, err := dev.IpcGet()
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(ipcData, "\n") {
		if strings.HasPrefix(line, "rx_bytes=") {
			if v, err := strconv.ParseInt(line[9:], 10, 64); err == nil {
				rxBytes += v
			}
		} else if strings.HasPrefix(line, "tx_bytes=") {
			if v, err := strconv.ParseInt(line[9:], 10, 64); err == nil {
				txBytes += v
			}
		}
	}
	return rxBytes, txBytes
}

func (m *wireGuardGoManager) storeSession(tunnelKey string, s *tunnelSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[tunnelKey] = s
}

func (m *wireGuardGoManager) removeSession(tunnelKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tunnelKey)
}

func (m *wireGuardGoManager) hasActiveDevice(tunnelKey string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[tunnelKey]
	return ok && s != nil && s.device != nil
}

func (m *wireGuardGoManager) ActiveLUIDs() map[uint64]struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[uint64]struct{}, len(m.sessions))
	for _, s := range m.sessions {
		if s != nil && s.windowsLUID != 0 {
			result[s.windowsLUID] = struct{}{}
		}
	}
	return result
}

func (m *wireGuardGoManager) ActiveInterfaceName(_ context.Context, profile state.WireGuardProfile) (string, error) {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return "", errors.New("wireguard tunnelName is required")
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	session, ok := m.session(tunnelKey)
	if !ok || session == nil {
		return "", fmt.Errorf("wireguard tunnel %s is not running", profile.TunnelName)
	}

	interfaceName := strings.TrimSpace(session.interfaceName)
	if interfaceName == "" {
		return "", fmt.Errorf("wireguard tunnel %s has no active interface name", profile.TunnelName)
	}

	return interfaceName, nil
}

// ---------------------------------------------------------------------------
// Config parsing
// ---------------------------------------------------------------------------

func parseUserlandConfig(input string) (parsedUserlandConfig, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	outLines := make([]string, 0, 64)
	addresses := make([]string, 0, 4)
	dnsServers := make([]string, 0, 4)
	endpointSet := map[string]struct{}{}
	mtu := 0
	section := ""

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			outLines = append(outLines, line)
			continue
		}

		key, value, ok := parseKeyValue(trimmed)
		if ok && section == "interface" {
			switch strings.ToLower(key) {
			case "address":
				addresses = append(addresses, splitCSV(value)...)
				continue
			case "dns":
				dnsServers = append(dnsServers, splitCSV(value)...)
				continue
			case "table", "preup", "predown", "postup", "postdown", "saveconfig":
				continue
			case "mtu":
				if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
					mtu = parsed
				}
				continue
			}
		}
		if ok && section == "peer" && strings.EqualFold(key, "Endpoint") {
			host := parseEndpointHost(value)
			if host != "" {
				endpointSet[strings.ToLower(host)] = struct{}{}
			}
		}

		outLines = append(outLines, line)
	}

	if err := scanner.Err(); err != nil {
		return parsedUserlandConfig{}, fmt.Errorf("parse wireguard config failed: %w", err)
	}

	stripped := strings.TrimSpace(strings.Join(outLines, "\n"))
	if stripped == "" {
		return parsedUserlandConfig{}, errors.New("wireguard config is empty")
	}

	endpointHosts := make([]string, 0, len(endpointSet))
	for host := range endpointSet {
		endpointHosts = append(endpointHosts, host)
	}
	sort.Strings(endpointHosts)

	return parsedUserlandConfig{
		wgConfig:      stripped + "\n",
		addresses:     normalizeStrings(addresses),
		dnsServers:    uniqueStringsPreserveOrder(dnsServers),
		mtu:           mtu,
		endpointHosts: endpointHosts,
	}, nil
}

func parseKeyValue(line string) (string, string, bool) {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return "", "", false
	}
	index := strings.Index(line, "=")
	if index < 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:index])
	value := stripInlineComment(line[index+1:])
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}

func stripInlineComment(value string) string {
	for i, r := range value {
		if r == '#' || r == ';' {
			return value[:i]
		}
	}
	return value
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseEndpointHost(value string) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return ""
	}

	if strings.HasPrefix(cleaned, "[") {
		end := strings.Index(cleaned, "]")
		if end > 1 {
			return strings.TrimSpace(cleaned[1:end])
		}
	}

	if host, _, err := net.SplitHostPort(cleaned); err == nil {
		return strings.TrimSpace(strings.Trim(host, "[]"))
	}

	if strings.Count(cleaned, ":") == 1 {
		parts := strings.SplitN(cleaned, ":", 2)
		return strings.TrimSpace(parts[0])
	}

	return strings.TrimSpace(strings.Trim(cleaned, "[]"))
}

// ---------------------------------------------------------------------------
// Merge and normalize utilities
// ---------------------------------------------------------------------------

func mergeEndpointHosts(endpointHosts []string, bypassHosts []string) []string {
	if len(bypassHosts) == 0 {
		return endpointHosts
	}

	merged := make([]string, 0, len(endpointHosts)+len(bypassHosts))
	merged = append(merged, endpointHosts...)
	for _, host := range bypassHosts {
		parsed := parseEndpointHost(host)
		if parsed == "" {
			continue
		}
		merged = append(merged, strings.ToLower(parsed))
	}
	return normalizeStrings(merged)
}

func mergeDNSServers(configDNS []string, profileDNS []string) []string {
	merged := make([]string, 0, len(configDNS)+len(profileDNS))
	merged = append(merged, configDNS...)
	merged = append(merged, profileDNS...)
	return uniqueStringsPreserveOrder(merged)
}

func validateParsedIPv4Only(parsed parsedUserlandConfig) ([]string, error) {
	if err := validateIPv4InterfaceAddresses(parsed.addresses); err != nil {
		return nil, err
	}
	if err := validateIPv4DNSServers(parsed.dnsServers); err != nil {
		return nil, err
	}

	allowedIPs, err := extractAllowedIPsFromConfig(parsed.wgConfig)
	if err != nil {
		return nil, err
	}
	return allowedIPs, nil
}

func validateIPv4InterfaceAddresses(addresses []string) error {
	for _, raw := range addresses {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		prefix, err := netip.ParsePrefix(trimmed)
		if err != nil {
			return fmt.Errorf("invalid [Interface] address %q: %w", trimmed, err)
		}
		if !prefix.Addr().Is4() {
			return fmt.Errorf("IPv6 [Interface] address is not supported: %s", trimmed)
		}
	}
	return nil
}

func validateIPv4DNSServers(dnsServers []string) error {
	for _, raw := range dnsServers {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		addr, err := netip.ParseAddr(trimmed)
		if err != nil {
			return fmt.Errorf("invalid DNS server %q: %w", trimmed, err)
		}
		if !addr.Is4() {
			return fmt.Errorf("IPv6 DNS server is not supported: %s", trimmed)
		}
	}
	return nil
}

func normalizeStrings(values []string) []string {
	unique := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		unique[trimmed] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}

	out := make([]string, 0, len(unique))
	for value := range unique {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueStringsPreserveOrder(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatDebugStringList(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}

// ---------------------------------------------------------------------------
// Endpoint route resolution (shared logic, platform apply/remove is separate)
// ---------------------------------------------------------------------------

func resolveEndpointRoutes(ctx context.Context, endpointHosts []string) []routeSpec {
	if len(endpointHosts) == 0 {
		return nil
	}

	unique := map[string]routeSpec{}
	for _, host := range endpointHosts {
		for _, ip := range resolveHostIPs(ctx, host) {
			if shouldSkipEndpointRouteIP(ip) {
				continue
			}

			v4 := ip.To4()
			if v4 == nil {
				continue
			}
			route := routeSpec{family: "inet", destination: v4.String()}
			unique["inet:"+route.destination] = route
		}
	}

	routes := make([]routeSpec, 0, len(unique))
	for _, route := range unique {
		routes = append(routes, route)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].family == routes[j].family {
			return routes[i].destination < routes[j].destination
		}
		return routes[i].family < routes[j].family
	})
	return routes
}

func shouldSkipEndpointRouteIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast()
}

func resolveHostIPs(ctx context.Context, host string) []net.IP {
	if host == "" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return []net.IP{v4}
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil
	}
	return ips
}

// normalizedRoutesForPrefix splits 0.0.0.0/0 into two /1 prefixes (and
// similarly for ::/0) to avoid replacing the default route.
func normalizedRoutesForPrefix(prefix string) ([]string, string, error) {
	ip, network, err := net.ParseCIDR(prefix)
	if err != nil {
		return nil, "", fmt.Errorf("invalid allowed ip %s: %w", prefix, err)
	}

	ones, bits := network.Mask.Size()
	if bits == 32 {
		if ones == 0 {
			return []string{"0.0.0.0/1", "128.0.0.0/1"}, "inet", nil
		}
		return []string{network.String()}, "inet", nil
	}
	if bits == 128 {
		if ones == 0 {
			return []string{"::/1", "8000::/1"}, "inet6", nil
		}
		return []string{network.String()}, "inet6", nil
	}

	if ip.To4() != nil {
		return []string{network.String()}, "inet", nil
	}
	return []string{network.String()}, "inet6", nil
}
