//go:build android || ios

package mobile

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *memoryStore) Get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key]
}

func (s *memoryStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

// initForTest runs Init on a fresh store and directory, resetting every global afterwards.
func initForTest(t *testing.T, values map[string]string) (*memoryStore, string) {
	t.Helper()
	s := &memoryStore{values: map[string]string{}}
	for key, value := range values {
		s.values[key] = value
	}
	dir := t.TempDir()
	Init(s, nil, nil, nil, dir)
	t.Cleanup(resetGlobals)
	return s, dir
}

func resetGlobals() {
	invalidateHubPath()
	mu.Lock()
	defer mu.Unlock()
	store, protector, sink, netKeys = nil, nil, nil, nil
	licenseKey, identity, servers, prepared = "", nil, nil, nil
	preferredTransport, transportMem, transportMemPath = "", nil, ""
	settings = defaultConfig()
	activeTunnel, starting, wgLogs = nil, nil, nil
	currentStatus = statusJSON{}
}

func mustIdentity(t *testing.T) *identityKeyPair {
	t.Helper()
	ident, err := generateIdentity()
	if err != nil {
		t.Fatalf("generateIdentity: %v", err)
	}
	return ident
}

func installHubRoute() *hubPath {
	route := &hubPath{kind: "normal", client: &http.Client{}}
	hubMu.Lock()
	activeHubPath = route
	hubMu.Unlock()
	return route
}

func currentHubRoute() *hubPath {
	hubMu.Lock()
	defer hubMu.Unlock()
	return activeHubPath
}

func newTestDevice() *device.Device {
	return device.NewDevice(tuntest.NewChannelTUN().TUN(), conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
}

// pipeFD stands in for the host's TUN fd; the test owns the write end.
func pipeFD(t *testing.T) int {
	t.Helper()
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(fds[1]) })
	return fds[0]
}

func fdIsOpen(fd int) bool {
	_, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	return err == nil
}

type loadedState struct {
	licenseKey, identityPub, preferred string
	settings                           config
}

func snapshotLoaded() loadedState {
	mu.Lock()
	defer mu.Unlock()
	return loadedState{licenseKey, identity.publicKey(), preferredTransport, settings}
}

func TestReloadPicksUpWhatAnotherProcessWrote(t *testing.T) {
	s, _ := initForTest(t, nil)
	ident := mustIdentity(t)
	s.Set(keyLicense, " lk-2 \n")
	s.Set(keyIdentity, ident.marshal())
	s.Set(keyConfig, `{"preferredTransport":"hysteria2","mtu":1400,"customDns":["9.9.9.9"]}`)

	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := snapshotLoaded()
	if got.licenseKey != "lk-2" || got.identityPub != ident.pubB64 {
		t.Fatalf("session = %q/%q, want lk-2/%q", got.licenseKey, got.identityPub, ident.pubB64)
	}
	if PreferredTransport() != "hysteria2" {
		t.Fatalf("PreferredTransport() = %q, want hysteria2", PreferredTransport())
	}
	raw, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg := decodeConfig(raw); cfg.MTU != 1400 || !reflect.DeepEqual(cfg.CustomDNS, []string{"9.9.9.9"}) {
		t.Fatalf("config = %+v, want mtu 1400 and dns 9.9.9.9", cfg)
	}
}

func TestReloadMatchesAFreshInit(t *testing.T) {
	ident := mustIdentity(t)
	cases := map[string]map[string]string{
		"signed in":  {keyLicense: "lk", keyIdentity: ident.marshal(), keyConfig: `{"preferredTransport":"cloak","allowLan":true}`},
		"signed out": {keyLicense: "", keyIdentity: "", keyConfig: ""},
		"corrupt":    {keyLicense: "lk", keyIdentity: "garbage", keyConfig: "{not json"},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			s, dir := initForTest(t, map[string]string{keyLicense: "old", keyConfig: `{"mtu":1300}`})
			for key, value := range values {
				s.Set(key, value)
			}
			_ = Reload()
			reloaded := snapshotLoaded()

			Init(s, nil, nil, nil, dir)
			if fresh := snapshotLoaded(); !reflect.DeepEqual(reloaded, fresh) {
				t.Fatalf("Reload loaded %+v, Init loads %+v", reloaded, fresh)
			}
		})
	}
}

func TestReloadReportsCorruptValues(t *testing.T) {
	s, _ := initForTest(t, map[string]string{keyConfig: `{"mtu":1400}`})
	s.Set(keyConfig, "{not json")
	s.Set(keyIdentity, "garbage")

	err := Reload()
	if err == nil {
		t.Fatal("Reload of a corrupt store returned nil")
	}
	for _, want := range []string{"decode stored config", "decode stored identity"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Reload error %q does not mention %q", err, want)
		}
	}
	if got := snapshotLoaded(); got.settings.MTU != mtuDefault || got.identityPub != "" {
		t.Fatalf("after corrupt reload: %+v, want defaults and no identity", got)
	}
}

func TestReloadSignsOutWhenTheLicenseIsGone(t *testing.T) {
	ident := mustIdentity(t)
	s, _ := initForTest(t, map[string]string{keyLicense: "lk", keyIdentity: ident.marshal()})
	running := &tunnelRuntime{serverID: "fra"}
	mu.Lock()
	servers = []serverInfo{{ID: "fra"}}
	prepared = &preparedTunnel{serverID: "fra"}
	activeTunnel = running
	mu.Unlock()

	s.Set(keyLicense, "")
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	mu.Lock()
	gotPrepared, gotServers, gotActive := prepared, servers, activeTunnel
	mu.Unlock()
	if gotPrepared != nil || gotServers != nil {
		t.Fatalf("signed-out reload kept prepared=%v servers=%v", gotPrepared, gotServers)
	}
	if gotActive != running {
		t.Fatal("Reload touched the running tunnel")
	}
	if _, err := Prepare("fra"); err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("Prepare after sign-out = %v, want not authenticated", err)
	}
}

func TestReloadDropsAPreparedConfigOnlyWhenTheSessionChanges(t *testing.T) {
	ident := mustIdentity(t)
	cases := []struct {
		name     string
		key      string
		value    string
		keepsCfg bool
	}{
		{"settings only", keyConfig, `{"mtu":1300}`, true},
		{"another account", keyLicense, "lk-other", false},
		{"another device", keyIdentity, mustIdentity(t).marshal(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := initForTest(t, map[string]string{keyLicense: "lk", keyIdentity: ident.marshal()})
			p := &preparedTunnel{serverID: "fra"}
			mu.Lock()
			prepared = p
			mu.Unlock()

			s.Set(tc.key, tc.value)
			if err := Reload(); err != nil {
				t.Fatalf("Reload: %v", err)
			}
			mu.Lock()
			kept := prepared == p
			mu.Unlock()
			if kept != tc.keepsCfg {
				t.Fatalf("prepared kept = %v, want %v", kept, tc.keepsCfg)
			}
		})
	}
}

func TestReloadInvalidatesTheHubRouteOnlyWhenMethodsChange(t *testing.T) {
	s, _ := initForTest(t, nil)
	route := installHubRoute()

	s.Set(keyConfig, `{"mtu":1300}`)
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if currentHubRoute() != route {
		t.Fatal("a reload that left hub methods alone dropped the route")
	}

	s.Set(keyConfig, `{"hubMethods":{"directIp":false,"reality":true,"shadowsocks":true,"fronted":true,"normal":false,"rev":2}}`)
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if currentHubRoute() != nil {
		t.Fatal("a reload that changed hub methods kept the old route")
	}
}

func TestReloadPicksUpTransportHistoryClearedElsewhere(t *testing.T) {
	_, dir := initForTest(t, nil)
	rememberTransport("wifi:home", "cloak")

	other, err := state.NewTransportMemoryStore(transportMemoryPath(dir))
	if err != nil {
		t.Fatalf("open history: %v", err)
	}
	if err := other.Clear(); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	mu.Lock()
	memory := transportMem
	mu.Unlock()
	if kind, ok := memory.Lookup("wifi:home"); ok {
		t.Fatalf("history still remembers %q after another process cleared it", kind)
	}
}

func TestLogoutClearsTransportHistory(t *testing.T) {
	_, dir := initForTest(t, map[string]string{keyLicense: "lk"})
	rememberTransport("wifi:home", "cloak")

	Logout()

	mu.Lock()
	memory := transportMem
	mu.Unlock()
	if _, ok := memory.Lookup("wifi:home"); ok {
		t.Fatal("Logout left the network history in memory")
	}
	onDisk, err := state.NewTransportMemoryStore(transportMemoryPath(dir))
	if err != nil {
		t.Fatalf("reopen history: %v", err)
	}
	if _, ok := onDisk.Lookup("wifi:home"); ok {
		t.Fatal("Logout left the network history on disk")
	}
}

func TestNetworkChangedDropsTheHubRouteAndCooldown(t *testing.T) {
	initForTest(t, nil)
	installHubRoute()
	running := &tunnelRuntime{serverID: "fra"}
	mu.Lock()
	activeTunnel = running
	mu.Unlock()
	hubMu.Lock()
	hubTrack.searchFailed(time.Now())
	generation := hubTrack.generation
	hubMu.Unlock()

	NetworkChanged()

	hubMu.Lock()
	wait, gotGeneration := hubTrack.retryIn(time.Now()), hubTrack.generation
	hubMu.Unlock()
	if currentHubRoute() != nil {
		t.Fatal("NetworkChanged kept the previous network's hub route")
	}
	if wait != 0 {
		t.Fatalf("NetworkChanged kept a %v search cooldown", wait)
	}
	if gotGeneration != generation+1 {
		t.Fatal("NetworkChanged did not discard a search still running")
	}
	mu.Lock()
	gotActive := activeTunnel
	mu.Unlock()
	if gotActive != running {
		t.Fatal("NetworkChanged touched the running tunnel")
	}
}

func TestStopKeepsThePreparedConfigForAnotherStart(t *testing.T) {
	initForTest(t, nil)
	p := &preparedTunnel{serverID: "fra", serverName: "Frankfurt", profile: &state.Profile{}}
	mu.Lock()
	prepared = p
	activeTunnel = &tunnelRuntime{dev: newTestDevice(), serverID: "fra"}
	mu.Unlock()

	Stop()

	mu.Lock()
	gotPrepared, gotActive := prepared, activeTunnel
	mu.Unlock()
	if gotActive != nil || gotPrepared != p {
		t.Fatalf("after Stop: active=%v prepared kept=%v", gotActive, gotPrepared == p)
	}
	var status statusJSON
	if err := json.Unmarshal([]byte(State()), &status); err != nil || status.State != string(state.StateDisconnected) {
		t.Fatalf("State() after Stop = %+v (%v), want disconnected", status, err)
	}

	// This profile has no REALITY config, so Start stops at selection, before any tun device.
	SetPreferredTransport("reality")
	fd := pipeFD(t)
	err := Start(fd)
	if err == nil || !strings.Contains(err.Error(), "no reality configuration") {
		t.Fatalf("second Start = %v, want it to reach transport selection", err)
	}
	checkHostFDAfterRefusedStart(t, fd)
}

func TestStartReleasesTheFDItRefuses(t *testing.T) {
	cases := []struct {
		name    string
		prepare *preparedTunnel
		active  *tunnelRuntime
		want    string
	}{
		{"nothing prepared", nil, nil, "no prepared tunnel"},
		{"already running", &preparedTunnel{serverID: "fra"}, &tunnelRuntime{serverID: "fra"}, "already running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initForTest(t, nil)
			mu.Lock()
			prepared, activeTunnel = tc.prepare, tc.active
			mu.Unlock()

			fd := pipeFD(t)
			if err := Start(fd); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Start = %v, want %q", err, tc.want)
			}
			checkHostFDAfterRefusedStart(t, fd)
		})
	}
}
