//go:build darwin || linux || windows

package wg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/conn/bindtest"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	splitTestPrivateKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	splitTestPeerKey    = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	splitTestPeerKey2   = "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="
)

// legacyTransformWGConfigExcludeLAN is the Allow LAN transform as it shipped before the split
// generalisation; the wrapper must keep producing exactly its bytes.
func legacyTransformWGConfigExcludeLAN(configText string) (string, error) {
	keep := collectTunnelPrefixes(configText)
	scanner := bufio.NewScanner(strings.NewReader(configText))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var out []string
	section := ""
	for scanner.Scan() {
		rawLine := scanner.Text()
		trimmed := strings.TrimSpace(rawLine)
		if header, ok := sectionHeader(trimmed); ok {
			section = header
			out = append(out, rawLine)
			continue
		}
		if section != "peer" || trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			out = append(out, rawLine)
			continue
		}
		rawKey, rawValue, ok := strings.Cut(rawLine, "=")
		if !ok {
			out = append(out, rawLine)
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rawKey), "AllowedIPs") {
			out = append(out, rawLine)
			continue
		}
		value := rawValue
		comment := ""
		for i, r := range value {
			if r == '#' || r == ';' {
				comment = value[i:]
				value = value[:i]
				break
			}
		}
		var v4Inputs, v6Passthrough []netip.Prefix
		var v6Raw []string
		for part := range strings.SplitSeq(value, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(p)
			if err != nil {
				return "", fmt.Errorf("invalid AllowedIPs entry %q: %w", p, err)
			}
			if prefix.Addr().Is4() {
				v4Inputs = append(v4Inputs, prefix.Masked())
			} else {
				v6Passthrough = append(v6Passthrough, prefix.Masked())
				v6Raw = append(v6Raw, p)
			}
		}
		var parts []string
		if len(v4Inputs) > 0 {
			filtered := reinclude(subtractRanges(v4Inputs, lanExcludeRanges), keep)
			if len(filtered) == 0 {
				for _, p := range v4Inputs {
					parts = append(parts, p.String())
				}
			} else {
				for _, p := range filtered {
					parts = append(parts, p.String())
				}
			}
		}
		if len(v6Passthrough) > 0 {
			v6Filtered := subtractRanges(v6Passthrough, lanExcludeRangesV6)
			if len(v6Filtered) == 0 {
				parts = append(parts, v6Raw...)
			} else {
				for _, p := range v6Filtered {
					parts = append(parts, p.String())
				}
			}
		}
		rewritten := "AllowedIPs = " + strings.Join(parts, ", ")
		if comment != "" {
			rewritten += " " + strings.TrimSpace(comment)
		}
		out = append(out, rewritten)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("parse wg config for lan-exclude: %w", err)
	}
	return strings.Join(out, "\n") + "\n", nil
}

var allowLANFixtures = []string{
	"[Interface]\nPrivateKey = abc=\nAddress = 10.0.0.2/32\n\n[Peer]\nPublicKey = xyz=\nAllowedIPs = 0.0.0.0/0\nEndpoint = 1.2.3.4:443\n",
	"[Interface]\nAllowedIPs = 10.0.0.0/8\n",
	"[Peer]\nAllowedIPs = 0.0.0.0/0, ::/0\n",
	"[Peer]\nAllowedIPs = 192.168.1.0/24\n",
	"[Peer]\nAllowedIPs = fc00::/7\n",
	"[Interface]\nAddress = 10.0.0.2/32\nDNS = 10.0.0.53, 10.0.0.54\n\n[Peer]\nAllowedIPs = 0.0.0.0/0\n",
	"[Peer] # eu-1\nAllowedIPs = 0.0.0.0/0\n",
	"[Interface]\nPrivateKey = k=\nAddress = 100.64.3.4/32\nDNS = 100.64.0.1\nMTU = 1380\n\n# primary\n[Peer]\nPublicKey = a=\nAllowedIPs=0.0.0.0/1,128.0.0.0/1 # split default\nEndpoint = [2001:db8::1]:51820\nPersistentKeepalive = 25\n\n[Peer]\nPublicKey = b=\n; comment\nAllowedIPs = 10.20.0.0/16, 2001:db8::/32 ; site\n",
	"[Peer]\nAllowedIPs = 10.0.0.1/8, 8.8.8.8/32, fe80::1/64\nAllowedIPs = ::/0\n",
	"[Interface]\r\nAddress = 10.7.0.2/32\r\n[Peer]\r\nAllowedIPs = 0.0.0.0/0\r\n",
	"no sections at all\nAllowedIPs = 0.0.0.0/0",
}

func TestTransformWGConfigExcludeLAN_MatchesTheShippedTransformByteForByte(t *testing.T) {
	for i, input := range allowLANFixtures {
		want, wantErr := legacyTransformWGConfigExcludeLAN(input)
		got, err := TransformWGConfigExcludeLAN(input)
		if (err != nil) != (wantErr != nil) {
			t.Fatalf("fixture %d: err = %v, legacy err = %v", i, err, wantErr)
		}
		if got != want {
			t.Errorf("fixture %d: output drifted from the shipped transform\n got: %q\nwant: %q", i, got, want)
		}
	}
	if _, err := TransformWGConfigExcludeLAN("[Peer]\nAllowedIPs = nonsense\n"); err == nil {
		t.Error("an unparseable AllowedIPs entry must still be an error")
	}
}

func allowedPrefixes(t *testing.T, out string) []netip.Prefix {
	t.Helper()
	var prefixes []netip.Prefix
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "AllowedIPs") {
			continue
		}
		for _, part := range splitCSV(stripInlineComment(value)) {
			prefixes = append(prefixes, netip.MustParsePrefix(part))
		}
	}
	return prefixes
}

func covered(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func TestTransformWGConfigExclude_SplitOnlyLeavesIPv6AndLANAlone(t *testing.T) {
	input := "[Interface]\nAddress = 10.7.0.2/32\n\n[Peer]\nAllowedIPs = 0.0.0.0/0, ::/0\n"
	out, err := TransformWGConfigExclude(input, []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ", ::/0\n") {
		t.Fatalf("::/0 must pass through untouched without IPv6 excludes; got:\n%s", out)
	}
	prefixes := allowedPrefixes(t, out)
	if covered(prefixes, netip.MustParseAddr("203.0.113.77")) {
		t.Errorf("excluded range still routed into the tunnel:\n%s", out)
	}
	for _, ip := range []string{"8.8.8.8", "10.1.2.3", "192.168.1.1", "203.0.112.255", "203.0.114.0", "fe80::1"} {
		if !covered(prefixes, netip.MustParseAddr(ip)) {
			t.Errorf("%s must stay in the tunnel when only 203.0.113.0/24 is excluded:\n%s", ip, out)
		}
	}
}

func TestTransformWGConfigExclude_KeepsTunnelAddressesAndResolvers(t *testing.T) {
	input := "[Interface]\nAddress = 10.7.0.2/32\nDNS = 10.7.0.1\n\n[Peer]\nAllowedIPs = 0.0.0.0/0\n"
	keep := []netip.Prefix{netip.MustParsePrefix("10.9.9.9/32"), netip.MustParsePrefix("203.0.113.53/32")}
	excludes := append(LANExcludePrefixes(), netip.MustParsePrefix("203.0.113.0/24"))
	out, err := TransformWGConfigExclude(input, excludes, LANExcludePrefixesV6(), keep)
	if err != nil {
		t.Fatal(err)
	}
	prefixes := allowedPrefixes(t, out)
	for _, ip := range []string{"10.7.0.2", "10.7.0.1", "10.9.9.9", "203.0.113.53", "1.1.1.1"} {
		if !covered(prefixes, netip.MustParseAddr(ip)) {
			t.Errorf("%s must stay routed into the tunnel; got:\n%s", ip, out)
		}
	}
	for _, ip := range []string{"10.5.5.5", "203.0.113.54", "192.168.0.1"} {
		if covered(prefixes, netip.MustParseAddr(ip)) {
			t.Errorf("%s must be excluded; got:\n%s", ip, out)
		}
	}
}

// Every address is either still routed into the tunnel or excluded, never both and never neither.
func TestTransformWGConfigExclude_MultipleRangesPartitionTheSpace(t *testing.T) {
	excludes := []netip.Prefix{
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("198.51.100.7/32"),
		netip.MustParsePrefix("192.0.2.128/25"),
		netip.MustParsePrefix("45.0.0.0/8"),
	}
	out, err := TransformWGConfigExclude("[Peer]\nAllowedIPs = 0.0.0.0/0\n", excludes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	prefixes := allowedPrefixes(t, out)
	samples := []netip.Addr{
		netip.MustParseAddr("198.51.100.6"), netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("198.51.100.8"),
		netip.MustParseAddr("192.0.2.127"), netip.MustParseAddr("192.0.2.128"), netip.MustParseAddr("192.0.2.255"),
		netip.MustParseAddr("44.255.255.255"), netip.MustParseAddr("45.0.0.0"), netip.MustParseAddr("46.0.0.0"),
	}
	rng := rand.New(rand.NewSource(1))
	for range 2000 {
		var b [4]byte
		rng.Read(b[:])
		samples = append(samples, netip.AddrFrom4(b))
	}
	for _, addr := range samples {
		inTunnel, excluded := 0, false
		for _, p := range prefixes {
			if p.Contains(addr) {
				inTunnel++
			}
		}
		for _, ex := range excludes {
			excluded = excluded || ex.Contains(addr)
		}
		if excluded == (inTunnel > 0) || inTunnel > 1 {
			t.Fatalf("%s: excluded=%t, matched %d tunnel prefixes; got:\n%s", addr, excluded, inTunnel, out)
		}
	}
}

func TestTransformWGConfigExclude_NothingToExcludeReturnsTheConfig(t *testing.T) {
	input := "[Peer]\nAllowedIPs=0.0.0.0/0,::/0"
	out, err := TransformWGConfigExclude(input, nil, nil, []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32")})
	if err != nil || out != input {
		t.Fatalf("got %q, %v; want the input unchanged", out, err)
	}
}

func TestCountExcludeRoutes(t *testing.T) {
	lanOnly := CountExcludeRoutes(nil)
	if want := len(subtractRanges([]netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}, lanExcludeRanges)); lanOnly != want {
		t.Fatalf("CountExcludeRoutes(nil) = %d, want the LAN-only %d", lanOnly, want)
	}
	if got := CountExcludeRoutes([]netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")}); got != lanOnly {
		t.Errorf("a range inside the LAN set costs no routes: got %d, want %d", got, lanOnly)
	}
	if got := CountExcludeRoutes([]netip.Prefix{netip.MustParsePrefix("203.0.113.9/24")}); got <= lanOnly || got > lanOnly+24 {
		t.Errorf("one unmasked /24 = %d routes, want at most %d", got, lanOnly+24)
	}

	var scattered []netip.Prefix
	for i := range 256 {
		scattered = append(scattered, netip.PrefixFrom(netip.AddrFrom4([4]byte{byte(i), byte(i * 7), byte(i * 13), 1}), 32))
	}
	if got := CountExcludeRoutes(scattered); got <= 1024 {
		t.Fatalf("256 scattered /32s need %d routes; the worst case must exceed the 1024 budget", got)
	}
}

func TestCountAllowedIPv4(t *testing.T) {
	input := "[Interface]\nAddress = 10.0.0.2/32\n[Peer]\nAllowedIPs = 0.0.0.0/1, 128.0.0.0/1, ::/0 # c\n[Peer]\nAllowedIPs = 10.9.0.1\n"
	if got, err := CountAllowedIPv4(input); err != nil || got != 3 {
		t.Fatalf("CountAllowedIPv4 = %d, %v; want 3", got, err)
	}
	if got, err := CountAllowedIPv4("[Peer]\nAllowedIPs = ::/0\n"); err != nil || got != 0 {
		t.Fatalf("v6-only = %d, %v; want 0", got, err)
	}
	if _, err := CountAllowedIPv4("[Peer]\nAllowedIPs = bogus\n"); err == nil {
		t.Fatal("an invalid entry must be an error")
	}
}

func TestTunnelInfoFor(t *testing.T) {
	parsed, err := parseUserlandConfig("[Interface]\nPrivateKey = " + splitTestPrivateKey +
		"\nAddress = 10.7.0.2/32\nDNS = 10.7.0.1\nMTU = 9000\n\n[Peer]\nPublicKey = " + splitTestPeerKey + "\nAllowedIPs = 0.0.0.0/0\n")
	if err != nil {
		t.Fatal(err)
	}
	parsed.dnsServers = mergeDNSServers(parsed.dnsServers, []string{"1.1.1.1", "10.7.0.1"})
	info := tunnelInfoFor("pangea0", parsed)
	want := TunnelInfo{
		Name:      "pangea0",
		Addresses: []netip.Addr{netip.MustParseAddr("10.7.0.2")},
		DNS:       []netip.Addr{netip.MustParseAddr("10.7.0.1"), netip.MustParseAddr("1.1.1.1")},
		MTU:       maxWireGuardMTU,
	}
	if info.Name != want.Name || info.MTU != want.MTU || !slices.Equal(info.Addresses, want.Addresses) || !slices.Equal(info.DNS, want.DNS) {
		t.Fatalf("tunnelInfoFor = %+v, want %+v", info, want)
	}
	if got := firstIPv4Address([]string{"bogus", "fd00::2/64", "10.7.0.3/24"}); got != netip.MustParseAddr("10.7.0.3") {
		t.Fatalf("firstIPv4Address = %v, want 10.7.0.3", got)
	}
}

// The daemon reaches both through optional-interface assertions on what NewManager returns.
func TestNewManager_OffersTheSplitTunnelCapabilities(t *testing.T) {
	m := NewManager(state.NewLogStore(8))
	if _, ok := m.(interface{ SetSplitTunnelHook(SplitTunnelHook) }); !ok {
		t.Errorf("%T has no SetSplitTunnelHook", m)
	}
	if _, ok := m.(interface {
		ApplyAllowedIPs(context.Context, state.WireGuardProfile) error
	}); !ok {
		t.Errorf("%T has no ApplyAllowedIPs", m)
	}
}

type fakeSplitWrapper struct {
	tun.Device
	info    TunnelInfo
	updates []TunnelInfo
	closed  atomic.Bool
}

func (w *fakeSplitWrapper) TunnelAddr() netip.Addr {
	if len(w.info.Addresses) == 0 {
		return netip.Addr{}
	}
	return w.info.Addresses[0]
}

func (w *fakeSplitWrapper) UpdateTunnelInfo(info TunnelInfo) { w.updates = append(w.updates, info) }

func (w *fakeSplitWrapper) Close() error {
	w.closed.Store(true)
	return w.Device.Close()
}

type fakeSplitHook struct {
	wrapped []*fakeSplitWrapper
}

func (h *fakeSplitHook) WrapTUN(dev tun.Device, info TunnelInfo) tun.Device {
	w := &fakeSplitWrapper{Device: dev, info: info}
	h.wrapped = append(h.wrapped, w)
	return w
}

func TestTunnelAddrChanged(t *testing.T) {
	plain := tuntest.NewChannelTUN().TUN()
	if tunnelAddrChanged(plain, []string{"10.7.0.9/32"}) {
		t.Fatal("an unwrapped device has no address to protect")
	}
	wrapped := &fakeSplitWrapper{Device: plain, info: TunnelInfo{Addresses: []netip.Addr{netip.MustParseAddr("10.7.0.2")}}}
	if tunnelAddrChanged(wrapped, []string{"10.7.0.2/32"}) {
		t.Fatal("the same address must allow an in-place switch")
	}
	if !tunnelAddrChanged(wrapped, []string{"10.7.0.9/32"}) {
		t.Fatal("a new address must force a rebuild")
	}
	if !tunnelAddrChanged(wrapped, nil) {
		t.Fatal("losing the address must force a rebuild")
	}
}

func useChannelBind(t *testing.T) {
	t.Helper()
	prev := newDeviceBind
	newDeviceBind = func() conn.Bind { return bindtest.NewChannelBinds()[0] }
	t.Cleanup(func() { newDeviceBind = prev })
}

func splitTestConfig(t *testing.T, extraInterface string) parsedUserlandConfig {
	t.Helper()
	parsed, err := parseUserlandConfig("[Interface]\nPrivateKey = " + splitTestPrivateKey +
		"\nAddress = 10.7.0.2/32\nDNS = 10.7.0.1\n" + extraInterface + "\n\n[Peer]\nPublicKey = " + splitTestPeerKey + "\nAllowedIPs = 0.0.0.0/0\n")
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestCreateInProcessDevice_WrapsTheTUNBeforeWireGuardReadsIt(t *testing.T) {
	useChannelBind(t)
	hook := &fakeSplitHook{}
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	m.SetSplitTunnelHook(hook)

	parsed := splitTestConfig(t, "MTU = 9000")
	var factoryMTU int
	raw := tuntest.NewChannelTUN().TUN()
	dev, tunDev, err := m.createInProcessDeviceWithFactory("pangea0", parsed.mtu, parsed.wgConfig,
		func(_ string, mtu int) (tun.Device, error) { factoryMTU = mtu; return raw, nil },
		tunnelInfoFor("pangea0", parsed))
	if err != nil {
		t.Fatal(err)
	}
	if len(hook.wrapped) != 1 || tunDev != tun.Device(hook.wrapped[0]) {
		t.Fatalf("the session must keep the wrapper; got %T", tunDev)
	}
	info := hook.wrapped[0].info
	if info.Name != "loopbackTun1" || info.MTU != maxWireGuardMTU || factoryMTU != maxWireGuardMTU {
		t.Fatalf("info = %+v (factory mtu %d), want the live name and the clamped MTU", info, factoryMTU)
	}
	if !slices.Equal(info.Addresses, []netip.Addr{netip.MustParseAddr("10.7.0.2")}) || !slices.Equal(info.DNS, []netip.Addr{netip.MustParseAddr("10.7.0.1")}) {
		t.Fatalf("info = %+v, want the config's address and DNS", info)
	}
	dev.Close()
	if !hook.wrapped[0].closed.Load() {
		t.Fatal("closing the device must close the wrapper it reads from")
	}
}

func TestCreateInProcessDevice_WithoutAHookKeepsTheRawTUN(t *testing.T) {
	useChannelBind(t)
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	m.SetSplitTunnelHook(&fakeSplitHook{})
	m.SetSplitTunnelHook(nil)

	parsed := splitTestConfig(t, "")
	raw := tuntest.NewChannelTUN().TUN()
	dev, tunDev, err := m.createInProcessDeviceWithFactory("pangea0", parsed.mtu, parsed.wgConfig,
		func(string, int) (tun.Device, error) { return raw, nil }, tunnelInfoFor("pangea0", parsed))
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	if tunDev != raw {
		t.Fatalf("got %T, want the raw TUN after the hook was cleared", tunDev)
	}
}

func TestAllowedIPsUAPI(t *testing.T) {
	parsed, err := parseUserlandConfig("[Interface]\nPrivateKey = " + splitTestPrivateKey + "\nListenPort = 51820\nAddress = 10.7.0.2/32\n\n" +
		"[Peer]\nPublicKey = " + splitTestPeerKey + "\nEndpoint = 127.0.0.1:9\nAllowedIPs = 0.0.0.0/1, ::/0\nAllowedIPs = 128.0.0.0/1\nPersistentKeepalive = 25\n\n" +
		"[Peer]\nPublicKey = " + splitTestPeerKey2 + "\nAllowedIPs = 10.20.0.0/16\n")
	if err != nil {
		t.Fatal(err)
	}
	uapi, keys, err := allowedIPsUAPI(parsed.wgConfig)
	if err != nil {
		t.Fatal(err)
	}
	key1, _ := base64ToHex(splitTestPeerKey)
	key2, _ := base64ToHex(splitTestPeerKey2)
	want := "public_key=" + key1 + "\nupdate_only=true\nreplace_allowed_ips=true\nallowed_ip=0.0.0.0/1\nallowed_ip=128.0.0.0/1\n" +
		"public_key=" + key2 + "\nupdate_only=true\nreplace_allowed_ips=true\nallowed_ip=10.20.0.0/16\n"
	if uapi != want {
		t.Fatalf("uapi =\n%s\nwant\n%s", uapi, want)
	}
	if !slices.Equal(keys, []string{key1, key2}) {
		t.Fatalf("keys = %v", keys)
	}
	for _, bad := range []string{"[Interface]\nPrivateKey = " + splitTestPrivateKey + "\n", "[Peer]\nAllowedIPs = 0.0.0.0/0\n"} {
		if _, _, err := allowedIPsUAPI(bad); err == nil {
			t.Errorf("allowedIPsUAPI(%q) must fail", bad)
		}
	}
}

func newTestDevice(t *testing.T, config string) *device.Device {
	t.Helper()
	dev := device.NewDevice(tuntest.NewChannelTUN().TUN(), bindtest.NewChannelBinds()[0], device.NewLogger(device.LogLevelSilent, ""))
	t.Cleanup(dev.Close)
	parsed, err := parseUserlandConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	uapi, err := wgConfigToUAPI(parsed.wgConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.IpcSet(uapi); err != nil {
		t.Fatal(err)
	}
	return dev
}

func deviceAllowedIPs(t *testing.T, dev *device.Device) (allowed []string, endpoint string) {
	t.Helper()
	current, err := dev.IpcGet()
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(current, "\n") {
		if v, ok := strings.CutPrefix(line, "allowed_ip="); ok {
			allowed = append(allowed, v)
		}
		if v, ok := strings.CutPrefix(line, "endpoint="); ok {
			endpoint = v
		}
	}
	slices.Sort(allowed)
	return allowed, endpoint
}

const liveSplitConfig = "[Interface]\nPrivateKey = " + splitTestPrivateKey + "\nAddress = 10.7.0.2/32\n\n" +
	"[Peer]\nPublicKey = " + splitTestPeerKey + "\nEndpoint = 127.0.0.1:7\nAllowedIPs = 0.0.0.0/0\n"

func TestApplyDeviceAllowedIPs_SwapsOnlyTheAllowedIPs(t *testing.T) {
	dev := newTestDevice(t, liveSplitConfig)
	parsed, err := parseUserlandConfig(strings.Replace(liveSplitConfig, "Endpoint = 127.0.0.1:7", "Endpoint = 127.0.0.1:9", 1) + "AllowedIPs = 10.0.0.0/8\n")
	if err != nil {
		t.Fatal(err)
	}
	parsed.wgConfig = strings.Replace(parsed.wgConfig, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 0.0.0.0/1, 128.0.0.0/2", 1)
	uapi, keys, err := allowedIPsUAPI(parsed.wgConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDeviceAllowedIPs(dev, uapi, keys); err != nil {
		t.Fatal(err)
	}
	allowed, endpoint := deviceAllowedIPs(t, dev)
	if want := []string{"0.0.0.0/1", "10.0.0.0/8", "128.0.0.0/2"}; !slices.Equal(allowed, want) {
		t.Fatalf("allowed = %v, want %v", allowed, want)
	}
	if endpoint != "127.0.0.1:7" {
		t.Fatalf("endpoint = %q; the transport's endpoint must be left alone", endpoint)
	}
}

func TestApplyDeviceAllowedIPs_RefusesAPeerTheDeviceDoesNotHave(t *testing.T) {
	dev := newTestDevice(t, liveSplitConfig)
	other := strings.Replace(liveSplitConfig, splitTestPeerKey, splitTestPeerKey2, 1)
	other = strings.Replace(other, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 10.0.0.0/8", 1)
	parsed, err := parseUserlandConfig(other)
	if err != nil {
		t.Fatal(err)
	}
	uapi, keys, err := allowedIPsUAPI(parsed.wgConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDeviceAllowedIPs(dev, uapi, keys); err == nil {
		t.Fatal("a peer missing from the device must be an error, not a silent no-op")
	}
	if allowed, _ := deviceAllowedIPs(t, dev); !slices.Equal(allowed, []string{"0.0.0.0/0"}) {
		t.Fatalf("allowed = %v; a refused apply must change nothing", allowed)
	}
}

func TestApplyAllowedIPs_RefusesWithoutALiveSessionOrIPv4Routes(t *testing.T) {
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	profile := state.WireGuardProfile{TunnelName: "pangea0", ConfigText: liveSplitConfig}
	if err := m.ApplyAllowedIPs(context.Background(), profile); err == nil {
		t.Fatal("no session must be an error")
	}
	m.storeSession(sanitizeTunnelName("pangea0"), &tunnelSession{interfaceName: "pangea0", device: newTestDevice(t, liveSplitConfig)})
	noRoutes := profile
	noRoutes.ConfigText = strings.Replace(liveSplitConfig, "AllowedIPs = 0.0.0.0/0\n", "", 1)
	if err := m.ApplyAllowedIPs(context.Background(), noRoutes); err == nil {
		t.Fatal("an apply that would route nothing into the tunnel must be refused")
	}
	if err := m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{ConfigText: liveSplitConfig}); err == nil {
		t.Fatal("a missing tunnel name must be an error")
	}
}

// splitDefaultRoute mirrors how darwin installs 0.0.0.0/0, as two /1 routes.
func splitDefaultRoute(prefixes []string) []string {
	var out []string
	for _, p := range prefixes {
		if p == "0.0.0.0/0" {
			out = append(out, "0.0.0.0/1", "128.0.0.0/1")
			continue
		}
		out = append(out, p)
	}
	return out
}

type routeCallRecorder struct{ calls []string }

func (r *routeCallRecorder) add(p []string) error {
	r.calls = append(r.calls, "add "+strings.Join(p, ","))
	return nil
}

func (r *routeCallRecorder) remove(p []string) {
	r.calls = append(r.calls, "remove "+strings.Join(p, ","))
}

func TestSyncTrackedAllowedIPs_AddsBeforeRemovingAndTracksOnlyInstalledRoutes(t *testing.T) {
	rec := &routeCallRecorder{}
	tracked := []string{"0.0.0.0/1", "128.0.0.0/1"}

	want := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/2"}
	if err := syncTrackedAllowedIPs(context.Background(), &tracked, want, splitDefaultRoute, rec.add, rec.remove); err != nil {
		t.Fatal(err)
	}
	if wantCalls := []string{"add 128.0.0.0/2", "add 192.0.0.0/2", "remove 128.0.0.0/1"}; !slices.Equal(rec.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", rec.calls, wantCalls)
	}
	if !slices.Equal(tracked, want) {
		t.Fatalf("tracked = %v, want %v", tracked, want)
	}

	rec.calls = nil
	failing := func(p []string) error {
		if p[0] == "224.0.0.0/3" {
			return errors.New("route add failed")
		}
		return rec.add(p)
	}
	next := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3", "224.0.0.0/3"}
	if err := syncTrackedAllowedIPs(context.Background(), &tracked, next, splitDefaultRoute, failing, rec.remove); err == nil {
		t.Fatal("expected the add failure")
	}
	if wantCalls := []string{"add 192.0.0.0/3"}; !slices.Equal(rec.calls, wantCalls) {
		t.Fatalf("nothing may be removed after a failed add: %v", rec.calls)
	}
	if wantTracked := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/2", "192.0.0.0/3"}; !slices.Equal(tracked, wantTracked) {
		t.Fatalf("tracked = %v, want the old routes plus the one installed %v", tracked, wantTracked)
	}
}

// fakeRouteTable installs like addDarwinAllowedIPRoutes: route by route, stopping at the first failure.
type fakeRouteTable struct {
	expand func([]string) []string
	routes map[string]bool
	failOn string
}

func newFakeRouteTable(expand func([]string) []string, installed ...string) *fakeRouteTable {
	k := &fakeRouteTable{expand: expand, routes: map[string]bool{}}
	for _, r := range expand(installed) {
		k.routes[r] = true
	}
	return k
}

func (k *fakeRouteTable) add(p []string) error {
	for _, r := range k.expand(p) {
		if r == k.failOn {
			return errors.New("route add " + r + " failed")
		}
		k.routes[r] = true
	}
	return nil
}

func (k *fakeRouteTable) remove(p []string) {
	for _, r := range k.expand(p) {
		delete(k.routes, r)
	}
}

func (k *fakeRouteTable) list() []string {
	var out []string
	for r := range k.routes {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

// checkTracksKernel: tracked must list exactly the installed routes, so teardown misses none and a retry skips none.
func checkTracksKernel(t *testing.T, k *fakeRouteTable, tracked []string) {
	t.Helper()
	want := slices.Compact(slices.Sorted(slices.Values(k.expand(tracked))))
	if got := k.list(); !slices.Equal(got, want) {
		t.Fatalf("installed routes %v, tracked routes %v", got, want)
	}
}

// checkConverges fails one add, then retries the same want: the retry must leave exactly want's routes installed.
func checkConverges(t *testing.T, k *fakeRouteTable, tracked *[]string, want []string, failOn string) {
	t.Helper()
	k.failOn = failOn
	if err := syncTrackedAllowedIPs(context.Background(), tracked, want, k.expand, k.add, k.remove); err == nil {
		t.Fatalf("expected the add of %s to fail", failOn)
	}
	checkTracksKernel(t, k, *tracked)
	k.failOn = ""
	if err := syncTrackedAllowedIPs(context.Background(), tracked, want, k.expand, k.add, k.remove); err != nil {
		t.Fatal(err)
	}
	wantRoutes := slices.Sorted(slices.Values(k.expand(want)))
	if got := k.list(); !slices.Equal(got, wantRoutes) {
		t.Fatalf("routes after the retry = %v, want %v", got, wantRoutes)
	}
	if !slices.Equal(*tracked, want) {
		t.Fatalf("tracked = %v, want %v", *tracked, want)
	}
}

func TestSyncTrackedAllowedIPs_RetryInstallsWhatAFailedAddSkipped(t *testing.T) {
	carved := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3", "224.0.0.0/3"}
	k := newFakeRouteTable(splitDefaultRoute, "0.0.0.0/0")
	tracked := []string{"0.0.0.0/0"}
	checkConverges(t, k, &tracked, carved, "192.0.0.0/3")
	checkConverges(t, k, &tracked, []string{"0.0.0.0/0"}, "128.0.0.0/1")
}

// A user operation cancels a live apply: the loop stops between routes and still tracks exactly what it left.
func TestSyncTrackedAllowedIPs_StopsBetweenRoutesOnceCancelled(t *testing.T) {
	carved := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3", "224.0.0.0/3"}
	k := newFakeRouteTable(splitDefaultRoute, "0.0.0.0/0")
	tracked := []string{"0.0.0.0/0"}
	var ops []string
	record := func(op string, p []string) { ops = append(ops, op+" "+strings.Join(p, ",")) }

	addCtx, cancelAdd := context.WithCancel(context.Background())
	defer cancelAdd()
	add := func(p []string) error { record("add", p); cancelAdd(); return k.add(p) }
	remove := func(p []string) { record("remove", p); k.remove(p) }
	if err := syncTrackedAllowedIPs(addCtx, &tracked, carved, splitDefaultRoute, add, remove); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if want := []string{"add 128.0.0.0/2"}; !slices.Equal(ops, want) {
		t.Fatalf("route ops = %v, want only %v before the cancel took", ops, want)
	}
	checkTracksKernel(t, k, tracked)
	if err := syncTrackedAllowedIPs(context.Background(), &tracked, carved, splitDefaultRoute, k.add, k.remove); err != nil {
		t.Fatal(err)
	}

	ops = nil
	removeCtx, cancelRemove := context.WithCancel(context.Background())
	defer cancelRemove()
	remove = func(p []string) { record("remove", p); cancelRemove(); k.remove(p) }
	if err := syncTrackedAllowedIPs(removeCtx, &tracked, []string{"0.0.0.0/0"}, splitDefaultRoute, k.add, remove); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if want := []string{"remove 128.0.0.0/2"}; !slices.Equal(ops, want) {
		t.Fatalf("route ops = %v, want only %v before the cancel took", ops, want)
	}
	checkTracksKernel(t, k, tracked)
	if err := syncTrackedAllowedIPs(context.Background(), &tracked, carved, splitDefaultRoute, k.add, k.remove); err != nil {
		t.Fatal(err)
	}
	if want := slices.Sorted(slices.Values(carved)); !slices.Equal(k.list(), want) {
		t.Fatalf("routes = %v, want %v", k.list(), want)
	}
}

// Carving a range out of 0.0.0.0/0 keeps its 0.0.0.0/1 half: removing the old entry must not take that route.
func TestSyncTrackedAllowedIPs_ComparesInstalledRoutesNotEntries(t *testing.T) {
	rec := &routeCallRecorder{}
	tracked := []string{"0.0.0.0/0"}
	carved := []string{"0.0.0.0/1", "192.0.0.0/2", "128.0.0.0/3", "160.0.0.0/3"}
	if err := syncTrackedAllowedIPs(context.Background(), &tracked, carved, splitDefaultRoute, rec.add, rec.remove); err != nil {
		t.Fatal(err)
	}
	if want := []string{"add 192.0.0.0/2", "add 128.0.0.0/3", "add 160.0.0.0/3", "remove 128.0.0.0/1"}; !slices.Equal(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}

	rec.calls = nil
	if err := syncTrackedAllowedIPs(context.Background(), &tracked, []string{"0.0.0.0/0"}, splitDefaultRoute, rec.add, rec.remove); err != nil {
		t.Fatal(err)
	}
	if want := []string{"add 128.0.0.0/1", "remove 192.0.0.0/2", "remove 128.0.0.0/3", "remove 160.0.0.0/3"}; !slices.Equal(rec.calls, want) {
		t.Fatalf("undo calls = %v, want %v", rec.calls, want)
	}
	if !slices.Equal(tracked, []string{"0.0.0.0/0"}) {
		t.Fatalf("tracked = %v", tracked)
	}
}
