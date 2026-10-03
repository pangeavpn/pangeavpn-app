package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
	"golang.zx2c4.com/wireguard/tun"
)

// callLog records calls across fakes in the order they happened; nil records nothing.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

func (l *callLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

func (l *callLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = nil
}

// fakeSplitController stands in for splittunnel.Controller, recording what the service asks of it.
type fakeSplitController struct {
	mu         sync.Mutex
	apps       [][]string
	never      [][]string
	netChanges int
	status     splittunnel.Status
}

func (f *fakeSplitController) SetRules(apps, never []string) []procmatch.RuleError {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = append(f.apps, slices.Clone(apps))
	f.never = append(f.never, slices.Clone(never))
	return nil
}

func (f *fakeSplitController) NetworkChanged() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.netChanges++
}

func (f *fakeSplitController) Status() splittunnel.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeSplitController) setStatus(st splittunnel.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = st
}

func (f *fakeSplitController) lastRules() (apps, never []string, calls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.apps) == 0 {
		return nil, nil, 0
	}
	return f.apps[len(f.apps)-1], f.never[len(f.never)-1], len(f.apps)
}

func (f *fakeSplitController) networkChanges() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.netChanges
}

// stubSplitAppPolicy replaces the host's rule policy with a fixed one, so the service
// tests read the same on every OS: "rel/" is relative, "sys/" a system process.
func stubSplitAppPolicy(t *testing.T) {
	t.Helper()
	original := compileSplitApps
	compileSplitApps = func(apps, never []string) []procmatch.RuleError {
		var errs []procmatch.RuleError
		for i, app := range apps {
			switch {
			case strings.HasPrefix(app, "rel/"):
				errs = append(errs, procmatch.RuleError{Index: i, Code: procmatch.CodeNotAbsolute})
			case strings.HasPrefix(app, "sys/"):
				errs = append(errs, procmatch.RuleError{Index: i, Code: procmatch.CodeSystemProcess})
			case slices.Contains(never, app):
				errs = append(errs, procmatch.RuleError{Index: i, Code: procmatch.CodeOwnImage})
			}
		}
		return errs
	}
	t.Cleanup(func() { compileSplitApps = original })
}

type splitHarness struct {
	svc     *Service
	wg      *fakeWGManager
	ks      *fakeKillSwitch
	cloak   *fakeCloakManager
	ctl     *fakeSplitController
	events  *callLog
	path    string
	profile state.Profile
}

func newSplitHarness(t *testing.T, stored *splitTunnelSettings, profiles ...state.Profile) *splitHarness {
	t.Helper()
	if len(profiles) == 0 {
		profiles = []state.Profile{testProfile()}
	}
	events := &callLog{}
	h := &splitHarness{
		wg:      &fakeWGManager{events: events},
		ks:      &fakeKillSwitch{events: events},
		cloak:   &fakeCloakManager{},
		ctl:     &fakeSplitController{},
		events:  events,
		path:    filepath.Join(t.TempDir(), splitTunnelFile),
		profile: profiles[0],
	}
	if stored != nil {
		data, err := json.Marshal(stored)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(h.path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stubSplitAppPolicy(t)
	h.svc = newTestService(t, h.cloak, &fakeNaiveManager{}, h.wg, h.ks, profiles...)
	h.svc.splitTunnel.strictRPFn = func() (bool, error) { return false, nil }
	h.svc.SetSplitTunnel(h.ctl, h.path)
	return h
}

func (h *splitHarness) connect(t *testing.T, opts ConnectOptions) {
	t.Helper()
	if err := h.svc.Connect(context.Background(), h.profile.ID, opts); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if current, _ := h.svc.machine.Get(); current != state.StateConnected {
		t.Fatalf("state after Connect = %s, want connected", current)
	}
}

func (h *splitHarness) update(t *testing.T, update splitTunnelUpdate) splitTunnelView {
	t.Helper()
	view, invalid, err := h.svc.UpdateSplitTunnel(update)
	if err != nil || len(invalid) > 0 {
		t.Fatalf("UpdateSplitTunnel() = invalid %v, err %v", invalid, err)
	}
	return view
}

func ranges(cidrs ...string) splitTunnelUpdate {
	return splitTunnelUpdate{Enabled: true, Apps: []string{}, CIDRs: cidrs}
}

func (h *splitHarness) applyState() (int, string) {
	h.wg.mu.Lock()
	defer h.wg.mu.Unlock()
	return h.wg.applyCount, h.wg.lastApplyConfig
}

func (h *splitHarness) startConfig() string {
	h.wg.mu.Lock()
	defer h.wg.mu.Unlock()
	return h.wg.lastStartConfig
}

func (h *splitHarness) eventsWith(prefixes ...string) []string {
	var out []string
	for _, call := range h.events.list() {
		for _, p := range prefixes {
			if strings.HasPrefix(call, p) {
				out = append(out, call)
				break
			}
		}
	}
	return out
}

// allowedIPv4 reads every IPv4 AllowedIPs entry out of a WireGuard config.
func allowedIPv4(t *testing.T, config string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for line := range strings.SplitSeq(config, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "AllowedIPs") {
			continue
		}
		for part := range strings.SplitSeq(value, ",") {
			p, err := netip.ParsePrefix(strings.TrimSpace(part))
			if err != nil {
				t.Fatalf("AllowedIPs entry %q: %v", part, err)
			}
			if p.Addr().Is4() {
				out = append(out, p)
			}
		}
	}
	return out
}

func tunnelled(prefixes []netip.Prefix, addr string) bool {
	a := netip.MustParseAddr(addr)
	return slices.ContainsFunc(prefixes, func(p netip.Prefix) bool { return p.Contains(a) })
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// hostAbsPath turns a POSIX test path into one this host calls absolute.
func hostAbsPath(p string) string {
	if runtime.GOOS == "windows" {
		return `C:\` + strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", `\`)
	}
	return p
}

const splitTestToken = "0123456789abcdef"

func serveSplit(ctx context.Context, handler http.Handler, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(ctx, method, "http://127.0.0.1/split-tunnel", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+splitTestToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// Store and validation

func TestSplitTunnelStore_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), splitTunnelFile)
	st, err := openSplitTunnelStore(path)
	if err != nil {
		t.Fatalf("open on a missing file: %v", err)
	}
	cfg, gen := st.snapshot()
	if cfg.Enabled || len(cfg.Apps) != 0 || len(cfg.CIDRs) != 0 {
		t.Fatalf("missing file loaded as %+v, want split tunnelling off and empty", cfg)
	}
	want := splitTunnelSettings{Enabled: true, Apps: []string{`C:\Games\Foo\`}, CIDRs: []string{"203.0.113.0/24"}, Protect: []string{"/opt/pangea/app"}}
	next, err := st.save(want)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if next != gen+1 {
		t.Errorf("gen after save = %d, want %d", next, gen+1)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Errorf("stored file %s carries no schema version", data)
	}
	reopened, err := openSplitTunnelStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, _ := reopened.snapshot()
	if got.Enabled != want.Enabled || !slices.Equal(got.Apps, want.Apps) || !slices.Equal(got.CIDRs, want.CIDRs) || !slices.Equal(got.Protect, want.Protect) {
		t.Errorf("reloaded %+v, want %+v", got, want)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("settings dir holds %d entries, want only the settings file (no temp left behind)", len(entries))
	}
}

func TestSplitTunnelStore_CorruptFileMeansOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), splitTunnelFile)
	if err := os.WriteFile(path, []byte(`{"enabled":true,"apps":["/x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := openSplitTunnelStore(path)
	if err == nil {
		t.Fatal("a truncated file loaded without complaint")
	}
	if cfg, _ := st.snapshot(); cfg.Enabled || len(cfg.Apps) != 0 {
		t.Fatalf("corrupt file loaded as %+v, want split tunnelling off", cfg)
	}
	if _, err := st.save(splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}}); err != nil {
		t.Fatalf("save over a corrupt file: %v", err)
	}
	if _, err := openSplitTunnelStore(path); err != nil {
		t.Errorf("file still unreadable after a save: %v", err)
	}
}

func TestSplitTunnelStore_StoredRangesAreRevalidated(t *testing.T) {
	path := filepath.Join(t.TempDir(), splitTunnelFile)
	if err := os.WriteFile(path, []byte(`{"version":1,"enabled":true,"apps":[],"cidrs":["10.0.0.0/4","203.0.113.9/24"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := openSplitTunnelStore(path)
	if err == nil {
		t.Error("an out-of-policy stored range was dropped silently")
	}
	cfg, _ := st.snapshot()
	if !slices.Equal(cfg.CIDRs, []string{"203.0.113.0/24"}) {
		t.Errorf("stored ranges = %v, want only the valid one, masked", cfg.CIDRs)
	}
}

func TestForgetSplitTunnel_RemovesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), splitTunnelFile)
	original := splitTunnelPathFn
	splitTunnelPathFn = func() (string, error) { return path, nil }
	t.Cleanup(func() { splitTunnelPathFn = original })
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ForgetSplitTunnel(); err != nil {
		t.Fatalf("ForgetSplitTunnel: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("settings file still there after an uninstall clear (stat err %v)", err)
	}
	if err := ForgetSplitTunnel(); err != nil {
		t.Errorf("second ForgetSplitTunnel on a missing file: %v", err)
	}
}

func TestNormalizeSplitCIDRList(t *testing.T) {
	manyHosts := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("198.%d.%d.1", 18+i/250, i%250)
		}
		return out
	}
	tests := []struct {
		name    string
		in      []string
		want    []string
		invalid []splitTunnelInvalid
	}{
		{name: "bare address is a host", in: []string{"203.0.113.7"}, want: []string{"203.0.113.7/32"}},
		{name: "host bits are masked", in: []string{"10.1.2.3/8"}, want: []string{"10.0.0.0/8"}},
		{name: "duplicates and covered ranges go", in: []string{"10.9.0.0/16", "203.0.113.7", "10.0.0.0/8", "203.0.113.7/32"}, want: []string{"203.0.113.7/32", "10.0.0.0/8"}},
		{name: "prefix floor", in: []string{"10.0.0.0/7"}, invalid: []splitTunnelInvalid{{"cidrs", 0, splitCodePrefixTooShort}}},
		{name: "every bad token is reported", in: []string{"203.0.113.0/24", "2001:db8::/32", "nope", "::ffff:10.0.0.1", "1.2.3.4/33"}, invalid: []splitTunnelInvalid{
			{"cidrs", 1, splitCodeNotIPv4}, {"cidrs", 2, splitCodeNotIPv4}, {"cidrs", 3, splitCodeNotIPv4}, {"cidrs", 4, splitCodeNotIPv4},
		}},
		{name: "entry cap counts what survives", in: manyHosts(maxSplitCIDRs + 1), invalid: []splitTunnelInvalid{{"cidrs", maxSplitCIDRs, splitCodeTooMany}}},
		{name: "empty is fine", in: []string{}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, invalid := normalizeSplitCIDRList(tt.in)
			if !slices.Equal(invalid, tt.invalid) {
				t.Fatalf("invalid = %v, want %v", invalid, tt.invalid)
			}
			if tt.invalid == nil && !slices.Equal(got, tt.want) {
				t.Errorf("normalized = %v, want %v", got, tt.want)
			}
		})
	}
}

// Scattered hosts cost ~31 routes each once carved out of 0.0.0.0/0; the budget is on
// the routes, not only the entry count.
func TestNormalizeSplitCIDRList_RouteBudget(t *testing.T) {
	var hosts []string
	var prefixes []netip.Prefix
	for i := range maxSplitCIDRs {
		host := fmt.Sprintf("%d.%d.7.9/32", 11+i, (i*37)%256)
		hosts = append(hosts, host)
		prefixes = append(prefixes, netip.MustParsePrefix(host))
	}
	if n := wg.CountExcludeRoutes(prefixes); n <= maxSplitRoutes {
		t.Fatalf("fixture needs only %d routes; it must exceed %d to prove anything", n, maxSplitRoutes)
	}
	_, invalid := normalizeSplitCIDRList(hosts)
	if len(invalid) != 1 || invalid[0].Code != splitCodeTooManyRoutes {
		t.Fatalf("invalid = %v, want one tooManyRoutes", invalid)
	}
}

// ---------------------------------------------------------------------------
// Routes

func TestSplitTunnelRoute_NotWiredIsMissing(t *testing.T) {
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
	handler := NewHandler(splitTestToken, svc)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := serveSplit(context.Background(), handler, method, `{"enabled":false,"apps":[],"cidrs":[]}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s on an unwired daemon = %d, want 404 so the app reads it as unsupported", method, rec.Code)
		}
	}
	if st := svc.Status(context.Background()); st.SplitTunnel != nil {
		t.Errorf("status carries a split tunnel block %+v on an unwired daemon", st.SplitTunnel)
	}
	data, _ := json.Marshal(svc.Status(context.Background()))
	if strings.Contains(string(data), "splitTunnel") {
		t.Errorf("status JSON %s names splitTunnel; older clients expect it absent", data)
	}
}

func TestSplitTunnelRoute_GetShape(t *testing.T) {
	h := newSplitHarness(t, nil)
	rec := serveSplit(context.Background(), NewHandler(splitTestToken, h.svc), http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"enabled", "apps", "cidrs", "appsSupported", "unavailableReason", "active", "pending"} {
		if _, ok := got[key]; !ok {
			t.Errorf("GET reply %s lacks %q", rec.Body, key)
		}
	}
	if apps, ok := got["apps"].([]any); !ok || len(apps) != 0 {
		t.Errorf("apps = %#v, want an empty list, not null", got["apps"])
	}
	if got["appsSupported"] != true {
		t.Errorf("appsSupported = %v with an engine wired in", got["appsSupported"])
	}
}

func TestSplitTunnelRoute_RejectsWithoutEchoingEntries(t *testing.T) {
	h := newSplitHarness(t, nil)
	handler := NewHandler(splitTestToken, h.svc)
	body := `{"enabled":true,"apps":["rel/secret-name.exe","sys/host.exe","/fine/app"],"cidrs":["10.0.0.0/4","nope-token"]}`
	rec := serveSplit(context.Background(), handler, http.MethodPost, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST = %d %s, want 400", rec.Code, rec.Body)
	}
	var got splitTunnelInvalidResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := []splitTunnelInvalid{
		{"apps", 0, procmatch.CodeNotAbsolute},
		{"apps", 1, procmatch.CodeSystemProcess},
		{"cidrs", 0, splitCodePrefixTooShort},
		{"cidrs", 1, splitCodeNotIPv4},
	}
	if got.OK || got.Error != "invalid_split_tunnel" || !slices.Equal(got.Invalid, want) {
		t.Errorf("400 body = %+v, want ok=false error=invalid_split_tunnel invalid=%v", got, want)
	}
	for _, secret := range []string{"secret-name", "host.exe", "nope-token", "10.0.0.0"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("400 body %s echoes %q", rec.Body, secret)
		}
	}
	if _, _, calls := h.ctl.lastRules(); calls != 1 {
		t.Errorf("SetRules called %d times, want only the boot load: a rejected POST applies nothing", calls)
	}
	if cfg, _ := h.svc.splitTunnel.store.snapshot(); cfg.Enabled {
		t.Error("a rejected POST was saved")
	}
}

func TestSplitTunnelRoute_RefusesMalformedBodies(t *testing.T) {
	h := newSplitHarness(t, nil)
	handler := NewHandler(splitTestToken, h.svc)
	for name, body := range map[string]string{
		"unknown field":   `{"enabled":true,"apps":[],"cidrs":[],"appsSupported":true}`,
		"missing cidrs":   `{"enabled":true,"apps":[]}`,
		"null apps":       `{"enabled":true,"apps":null,"cidrs":[]}`,
		"truncated":       `{"enabled":true,"apps":[`,
		"protect invalid": `{"enabled":true,"apps":[],"cidrs":[],"protect":["relative/app"]}`,
	} {
		rec := serveSplit(context.Background(), handler, http.MethodPost, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: POST = %d, want 400", name, rec.Code)
		}
	}
	if rec := serveSplit(context.Background(), handler, http.MethodPut, `{}`); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT = %d, want 405", rec.Code)
	}
	if _, _, calls := h.ctl.lastRules(); calls != 1 {
		t.Errorf("SetRules called %d times after only refused POSTs, want 1 (boot)", calls)
	}
}

func TestSplitTunnelRoute_SavesNormalisedAndAppliesAppsLive(t *testing.T) {
	h := newSplitHarness(t, nil)
	handler := NewHandler(splitTestToken, h.svc)
	protect := hostAbsPath("/opt/pangea/pangea")
	protectJSON, _ := json.Marshal([]string{protect})
	body := `{"enabled":true,"apps":["/opt/game/","/opt/game/","/usr/local/bin/tool"],"cidrs":["10.9.0.0/16","203.0.113.7","10.1.2.3/8"],"protect":` + string(protectJSON) + `}`
	rec := serveSplit(context.Background(), handler, http.MethodPost, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	var view splitTunnelView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	wantApps := []string{"/opt/game/", "/usr/local/bin/tool"}
	if !view.Enabled || !slices.Equal(view.Apps, wantApps) || !slices.Equal(view.CIDRs, []string{"203.0.113.7/32", "10.0.0.0/8"}) {
		t.Errorf("POST reply = %+v, want the normalised config", view)
	}
	if view.Pending {
		t.Error("pending with no session to apply to")
	}
	apps, never, _ := h.ctl.lastRules()
	if !slices.Equal(apps, wantApps) {
		t.Errorf("engine rules = %v, want %v applied at once", apps, wantApps)
	}
	if !slices.Contains(never, protect) || !slices.Contains(never, h.svc.splitTunnel.self) {
		t.Errorf("never-bypass = %v, want the desktop app and the daemon itself", never)
	}

	// protect left out keeps the stored list; disabling hands the engine no apps.
	rec = serveSplit(context.Background(), handler, http.MethodPost, `{"enabled":false,"apps":["/opt/game/"],"cidrs":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("second POST = %d %s", rec.Code, rec.Body)
	}
	apps, never, _ = h.ctl.lastRules()
	if len(apps) != 0 {
		t.Errorf("engine rules with split tunnelling off = %v, want none", apps)
	}
	if !slices.Contains(never, protect) {
		t.Errorf("never-bypass = %v after a POST without protect, want the stored one kept", never)
	}
	reloaded, err := openSplitTunnelStore(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := reloaded.snapshot(); cfg.Enabled || !slices.Equal(cfg.Apps, []string{"/opt/game/"}) || !slices.Equal(cfg.Protect, []string{protect}) {
		t.Errorf("persisted %+v", cfg)
	}
}

func TestSplitTunnelRoute_OwnImageIsRefused(t *testing.T) {
	h := newSplitHarness(t, nil)
	self := h.svc.splitTunnel.self
	if self == "" {
		t.Skip("no executable path on this host")
	}
	_, invalid, err := h.svc.UpdateSplitTunnel(splitTunnelUpdate{Enabled: true, Apps: []string{"/fine", self}, CIDRs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(invalid, []splitTunnelInvalid{{"apps", 1, procmatch.CodeOwnImage}}) {
		t.Errorf("invalid = %v, want the daemon's own image refused", invalid)
	}
}

// The real per-OS policy, end to end: the codes the desktop maps come from procmatch.
func TestSplitTunnelUpdate_UsesTheHostRulePolicy(t *testing.T) {
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
	svc.splitTunnel.strictRPFn = func() (bool, error) { return false, nil }
	svc.SetSplitTunnel(&fakeSplitController{}, filepath.Join(t.TempDir(), splitTunnelFile))
	valid := map[string]string{"windows": `C:\Games\Foo\foo.exe`, "darwin": "/Applications/Foo.app"}[runtime.GOOS]
	if valid == "" {
		valid = "/opt/foo/foo"
	}
	apps := []string{valid, "relative-app"}
	if svc.splitTunnel.self != "" {
		apps = append(apps, svc.splitTunnel.self)
	}
	_, invalid, err := svc.UpdateSplitTunnel(splitTunnelUpdate{Enabled: true, Apps: apps, CIDRs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	want := []splitTunnelInvalid{{"apps", 1, procmatch.CodeNotAbsolute}}
	if len(apps) == 3 {
		want = append(want, splitTunnelInvalid{"apps", 2, procmatch.CodeOwnImage})
	}
	if !slices.Equal(invalid, want) {
		t.Errorf("invalid = %v, want %v", invalid, want)
	}
}

func TestSplitTunnelRoute_AnswersWhileOpMuIsHeld(t *testing.T) {
	h := newSplitHarness(t, nil)
	handler := NewHandler(splitTestToken, h.svc)
	h.svc.opMu.Lock()
	defer h.svc.opMu.Unlock()
	done := make(chan int, 2)
	go func() {
		done <- serveSplit(context.Background(), handler, http.MethodGet, "").Code
		done <- serveSplit(context.Background(), handler, http.MethodPost, `{"enabled":true,"apps":[],"cidrs":["203.0.113.0/24"]}`).Code
	}()
	for range 2 {
		select {
		case code := <-done:
			if code != http.StatusOK {
				t.Fatalf("reply = %d, want 200", code)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the route waited on opMu; a long connect would time the app out")
		}
	}
}

// ---------------------------------------------------------------------------
// Bring-up

func TestSplitTunnelConnect_PermitsRangesBeforeTheTunnelComesUp(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.connect(t, ConnectOptions{})

	got := h.eventsWith("ks:", "wg:start")
	want := []string{"ks:enable", "ks:cidrs 203.0.113.0/24", "wg:start"}
	if !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v: the lock must permit a range before the routes send it off-tunnel", got, want)
	}
	allowed := allowedIPv4(t, h.startConfig())
	if tunnelled(allowed, "203.0.113.7") {
		t.Errorf("AllowedIPs %v still route the excluded range into the tunnel", allowed)
	}
	if !tunnelled(allowed, "8.8.8.8") {
		t.Errorf("AllowedIPs %v lost the rest of the internet", allowed)
	}
	st := h.svc.Status(context.Background()).SplitTunnel
	if st == nil || st.Pending || st.CIDRCount != 1 {
		t.Errorf("status split block = %+v, want 1 range applied, nothing pending", st)
	}
}

func TestSplitTunnelConnect_PermitFailureKeepsRangesInTheTunnel(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.ks.splitCIDRErr = errors.New("filter add refused")
	h.connect(t, ConnectOptions{})

	if allowed := allowedIPv4(t, h.startConfig()); !tunnelled(allowed, "203.0.113.7") {
		t.Errorf("AllowedIPs %v route a range the lock refused to permit around the tunnel: it would be blackholed", allowed)
	}
	if st := h.svc.Status(context.Background()).SplitTunnel; st == nil || !st.Pending {
		t.Errorf("status split block = %+v, want the unapplied range reported pending", st)
	}
}

func TestSplitTunnelConnect_DisabledCarvesNothing(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: false, CIDRs: []string{"203.0.113.0/24"}})
	h.connect(t, ConnectOptions{})
	if _, calls := h.ks.splitState(); calls != 0 {
		t.Errorf("SetSplitCIDRs called %d times with split tunnelling off", calls)
	}
	if allowed := allowedIPv4(t, h.startConfig()); !slices.Equal(allowed, []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}) {
		t.Errorf("AllowedIPs = %v, want the config untouched", allowed)
	}
}

// A server whose AllowedIPs are already fragmented cannot take more routes: every range
// stays in the tunnel, with neither routes nor permits.
func TestSplitTunnelConnect_RangesThatDoNotFitStayInTheTunnel(t *testing.T) {
	profile := testProfile()
	var allowed []string
	for i := range 1100 {
		allowed = append(allowed, fmt.Sprintf("100.%d.%d.0/24", 64+i/256, i%256))
	}
	profile.WireGuard.ConfigText = strings.Replace(profile.WireGuard.ConfigText, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = "+strings.Join(allowed, ", "), 1)
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}}, profile)
	h.connect(t, ConnectOptions{})

	if cidrs, _ := h.ks.splitState(); len(cidrs) != 0 {
		t.Errorf("lock permits %v for ranges the routes keep in the tunnel", cidrs)
	}
	if got := len(allowedIPv4(t, h.startConfig())); got != len(allowed) {
		t.Errorf("AllowedIPs carry %d entries, want the server's own %d", got, len(allowed))
	}
	if st := h.svc.Status(context.Background()).SplitTunnel; st == nil || !st.CIDRsDropped || st.Pending {
		t.Errorf("status split block = %+v, want cidrsDropped and nothing pending", st)
	}
}

func TestWireGuardProfileFor_SplitKeepsResolversAndAddress(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.ConfigText = "[Interface]\nPrivateKey = YWJjZGVmZw==\nAddress = 10.66.0.2/32\nDNS = 10.64.0.1\n\n[Peer]\nPublicKey = eHl6MTIzNDU=\nEndpoint = 10.0.0.1:51820\nAllowedIPs = 0.0.0.0/0, ::/0\n"
	profile.WireGuard.DNS = []string{"198.51.100.53"}
	split := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("198.51.100.0/24")}

	wgp, err := wireGuardProfileFor(profile, false, split)
	if err != nil {
		t.Fatal(err)
	}
	allowed := allowedIPv4(t, wgp.ConfigText)
	for _, keep := range []string{"10.66.0.2", "10.64.0.1", "198.51.100.53"} {
		if !tunnelled(allowed, keep) {
			t.Errorf("AllowedIPs %v carve %s out of the tunnel; addresses and resolvers must stay in it", allowed, keep)
		}
	}
	for _, out := range []string{"10.1.2.3", "198.51.100.7"} {
		if tunnelled(allowed, out) {
			t.Errorf("AllowedIPs %v still route excluded %s", allowed, out)
		}
	}
	if !strings.Contains(wgp.ConfigText, "::/0") {
		t.Errorf("split-only carve touched IPv6: %s", wgp.ConfigText)
	}

	lanOnly, err := wireGuardProfileFor(profile, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := wg.TransformWGConfigExcludeLAN(withTransportBypassHosts(profile).ConfigText)
	if err != nil {
		t.Fatal(err)
	}
	if lanOnly.ConfigText != want {
		t.Errorf("Allow LAN without ranges changed output:\n%s\nwant\n%s", lanOnly.ConfigText, want)
	}
}

// A failed bring-up may leave a kept device without the routes it was about to get,
// so whatever adopts or reconciles it next must not trust the record.
func TestSplitTunnelConnect_FailedBringUpForgetsTheRoutes(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.wg.startErr = errors.New("adapter busy")
	if err := h.svc.Connect(context.Background(), h.profile.ID, ConnectOptions{}); err == nil {
		t.Fatal("Connect() succeeded with a failing device")
	}
	if h.svc.appliedSplitSnapshot().known {
		t.Error("the failed bring-up's routes are still recorded as live")
	}
}

func TestSplitTunnelAdoption_MovesTheDeviceOntoTheRanges(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.wg.running = true
	h.connect(t, ConnectOptions{})

	count, config := h.applyState()
	if count != 1 {
		t.Fatalf("ApplyAllowedIPs called %d times on adoption, want 1: the adopted device keeps what it was built with", count)
	}
	if tunnelled(allowedIPv4(t, config), "203.0.113.7") {
		t.Error("adopted device still routes the excluded range into the tunnel")
	}
	if cidrs, _ := h.ks.splitState(); !slices.Equal(cidrs, []string{"203.0.113.0/24"}) {
		t.Errorf("lock permits %v, want the stored range", cidrs)
	}
	if h.svc.splitPending() {
		t.Error("adoption left the ranges pending")
	}
}

// ---------------------------------------------------------------------------
// Reconciler

func TestSplitTunnelPOST_DuringConnectAnswersAtOnceAndAppliesAfter(t *testing.T) {
	h := newSplitHarness(t, nil)
	loopCtx, stopLoop := context.WithCancel(context.Background())
	defer stopLoop()
	go h.svc.splitReconcileLoop(loopCtx)

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.cloak.startHook = func(context.Context) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	connectErr := make(chan error, 1)
	go func() { connectErr <- h.svc.Connect(context.Background(), h.profile.ID, ConnectOptions{}) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("connect never reached the transport")
	}

	reqCtx, abort := context.WithCancel(context.Background())
	started := time.Now()
	rec := serveSplit(reqCtx, NewHandler(splitTestToken, h.svc), http.MethodPost, `{"enabled":true,"apps":["/opt/game/"],"cidrs":["203.0.113.0/24"]}`)
	abort()
	if rec.Code != http.StatusOK {
		t.Fatalf("POST during connect = %d %s", rec.Code, rec.Body)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("POST took %s behind a running connect", elapsed)
	}
	var view splitTunnelView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if !view.Pending {
		t.Error("POST reply not pending while the session still runs on the old ranges")
	}
	if apps, _, _ := h.ctl.lastRules(); !slices.Equal(apps, []string{"/opt/game/"}) {
		t.Errorf("app rules = %v, want them live before the connect ends", apps)
	}
	if count, _ := h.applyState(); count != 0 {
		t.Fatal("ranges applied while the connect still held the session")
	}

	close(release)
	if err := <-connectErr; err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	waitUntil(t, 5*time.Second, "the ranges to reach the live session", func() bool { return !h.svc.splitPending() })

	count, config := h.applyState()
	if count != 1 {
		t.Errorf("ApplyAllowedIPs called %d times, want 1", count)
	}
	if tunnelled(allowedIPv4(t, config), "203.0.113.7") {
		t.Error("applied AllowedIPs still route the excluded range into the tunnel")
	}
	if cidrs, _ := h.ks.splitState(); !slices.Equal(cidrs, []string{"203.0.113.0/24"}) {
		t.Errorf("lock permits %v after the apply", cidrs)
	}
	assertSessionUntouched(t, h)
}

// A range edit is not a reconnect: no state change, no rebuild, no server rotation.
func assertSessionUntouched(t *testing.T, h *splitHarness) {
	t.Helper()
	if current, detail := h.svc.machine.Get(); current != state.StateConnected || detail != "tunnel active" {
		t.Errorf("state = %s %q, want the session left as it was", current, detail)
	}
	if h.svc.transportsAreExhausted() || h.svc.recoveryPending() {
		t.Error("a range edit was booked against the session's recovery")
	}
	h.wg.mu.Lock()
	starts, stops := h.wg.startCount, h.wg.stopCount
	h.wg.mu.Unlock()
	if starts != 1 || stops != 0 {
		t.Errorf("wireguard starts=%d stops=%d, want the device kept (1, 0)", starts, stops)
	}
}

func TestSplitTunnelPOST_BurstCoalescesIntoOneApply(t *testing.T) {
	h := newSplitHarness(t, nil)
	h.connect(t, ConnectOptions{})
	loopCtx, stopLoop := context.WithCancel(context.Background())
	defer stopLoop()
	go h.svc.splitReconcileLoop(loopCtx)

	h.svc.opMu.Lock()
	for i := 1; i <= 5; i++ {
		h.update(t, ranges(fmt.Sprintf("203.0.113.%d/32", i)))
	}
	h.svc.opMu.Unlock()
	waitUntil(t, 5*time.Second, "the burst to apply", func() bool { return !h.svc.splitPending() })
	time.Sleep(50 * time.Millisecond)

	if count, _ := h.applyState(); count != 1 {
		t.Errorf("ApplyAllowedIPs called %d times for one burst, want 1", count)
	}
	if cidrs, _ := h.ks.splitState(); !slices.Equal(cidrs, []string{"203.0.113.5/32"}) {
		t.Errorf("lock permits %v, want only the last edit", cidrs)
	}
	assertSessionUntouched(t, h)
}

func TestSplitTunnelReconcile_PermitsBothThenRoutesThenNarrows(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"198.51.100.0/24"}})
	h.connect(t, ConnectOptions{})
	h.events.reset()

	h.update(t, ranges("203.0.113.0/24"))
	h.svc.reconcileSplit(context.Background())

	got := h.eventsWith("ks:", "wg:")
	want := []string{"ks:cidrs 198.51.100.0/24,203.0.113.0/24", "wg:apply", "ks:cidrs 203.0.113.0/24"}
	if !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v: neither range may lose its permit while its route moves", got, want)
	}
	assertSessionUntouched(t, h)
}

func TestSplitTunnelReconcile_FailedApplyKeepsBothPermitsAndRetries(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"198.51.100.0/24"}})
	h.connect(t, ConnectOptions{})
	h.wg.mu.Lock()
	h.wg.applyErr = errors.New("route add failed")
	h.wg.mu.Unlock()

	h.update(t, ranges("203.0.113.0/24"))
	h.svc.reconcileSplit(context.Background())

	if cidrs, _ := h.ks.splitState(); !slices.Equal(cidrs, []string{"198.51.100.0/24", "203.0.113.0/24"}) {
		t.Errorf("lock permits %v after a failed apply, want both sets while the routes are unknown", cidrs)
	}
	if !h.svc.splitPending() {
		t.Error("a failed apply is not reported pending")
	}
	assertSessionUntouched(t, h)

	select {
	case <-h.svc.splitTunnel.kick:
	default:
	}
	h.svc.splitTick()
	if len(h.svc.splitTunnel.kick) != 0 {
		t.Error("the health tick retried at once; a failing apply must back off")
	}
	h.svc.splitTunnel.mu.Lock()
	h.svc.splitTunnel.retryAt = time.Time{}
	h.svc.splitTunnel.mu.Unlock()
	h.svc.splitTick()
	if len(h.svc.splitTunnel.kick) != 1 {
		t.Fatal("the health tick did not retry a pending apply once due")
	}

	h.wg.mu.Lock()
	h.wg.applyErr = nil
	h.wg.mu.Unlock()
	h.svc.reconcileSplit(context.Background())
	if cidrs, _ := h.ks.splitState(); !slices.Equal(cidrs, []string{"203.0.113.0/24"}) {
		t.Errorf("lock permits %v after the retry, want the new range only", cidrs)
	}
	if h.svc.splitPending() {
		t.Error("still pending after a successful retry")
	}
}

func TestSplitTunnelReconcile_ErrorStateMovesPermitsOnly(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"198.51.100.0/24"}})
	h.connect(t, ConnectOptions{})
	h.svc.machine.Set(state.StateError, "connection lost")

	h.update(t, ranges("203.0.113.0/24"))
	h.svc.reconcileSplit(context.Background())

	if count, _ := h.applyState(); count != 0 {
		t.Error("routes moved on a session that is not connected; the next bring-up does that")
	}
	if cidrs, _ := h.ks.splitState(); !slices.Equal(cidrs, []string{"203.0.113.0/24"}) {
		t.Errorf("lock permits %v, want only the new set: no route moves without a device", cidrs)
	}
	if current, detail := h.svc.machine.Get(); current != state.StateError || detail != "connection lost" {
		t.Errorf("state = %s %q, want it untouched", current, detail)
	}
}

// Recovery takes opMu with TryLock; a reconciler woken every tick would keep beating it.
func TestSplitTunnelTick_LeavesARecoveringSessionAlone(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"198.51.100.0/24"}})
	h.connect(t, ConnectOptions{})
	h.svc.machine.Set(state.StateError, "connection lost")
	h.update(t, ranges("203.0.113.0/24"))
	select {
	case <-h.svc.splitTunnel.kick:
	default:
	}
	h.svc.splitTick()
	if len(h.svc.splitTunnel.kick) != 0 {
		t.Error("the health tick woke the reconciler for a session in ERROR")
	}
}

func TestSplitTunnelReconcile_NothingWithoutASession(t *testing.T) {
	h := newSplitHarness(t, nil)
	view := h.update(t, ranges("203.0.113.0/24"))
	h.svc.reconcileSplit(context.Background())
	if _, calls := h.ks.splitState(); calls != 0 {
		t.Errorf("SetSplitCIDRs called %d times with no session", calls)
	}
	if view.Pending || h.svc.splitPending() {
		t.Error("pending with no session to apply to")
	}
}

func TestSplitTunnelPOST_NeverInterruptsARecovery(t *testing.T) {
	h := newSplitHarness(t, nil)
	recoveryCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := h.svc.claimRecoveryCancel(cancel)
	if release == nil {
		t.Fatal("fixture: could not register the recovery")
	}
	defer release()
	h.update(t, ranges("203.0.113.0/24"))
	h.update(t, splitTunnelUpdate{Enabled: true, Apps: []string{"/opt/game/"}, CIDRs: []string{}})
	if recoveryCtx.Err() != nil {
		t.Error("a split-tunnel save cancelled a running recovery")
	}
}

func TestSplitTunnelPOST_AppOnlyEditIsNotPending(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.connect(t, ConnectOptions{})
	h.svc.opMu.Lock()
	defer h.svc.opMu.Unlock()
	view := h.update(t, splitTunnelUpdate{Enabled: true, Apps: []string{"/opt/game/"}, CIDRs: []string{"203.0.113.0/24"}})
	if view.Pending {
		t.Error("an app-only edit reads as pending; app rules apply live")
	}
}

// The reconciler holds opMu like a rebuild does, so a user's Disconnect must be able
// to cut it short rather than queue behind it.
func TestSplitTunnelReconcile_DisconnectInterruptsIt(t *testing.T) {
	h := newSplitHarness(t, nil)
	h.connect(t, ConnectOptions{})
	h.update(t, ranges("203.0.113.0/24"))

	entered := make(chan struct{})
	var once sync.Once
	h.wg.mu.Lock()
	h.wg.applyHook = func(ctx context.Context) error {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return ctx.Err()
	}
	h.wg.mu.Unlock()
	done := make(chan struct{})
	go func() {
		h.svc.reconcileSplit(context.Background())
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the reconciler never reached the apply")
	}

	started := time.Now()
	if err := h.svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("Disconnect waited %s behind a range apply", elapsed)
	}
	<-done
	if cidrs, _ := h.ks.splitState(); len(cidrs) != 0 {
		t.Errorf("cleared lock still records split permits %v", cidrs)
	}
}

// ---------------------------------------------------------------------------
// Teardown and invariants

func TestSplitTunnelDisconnect_LockdownDropsRangePermits(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.connect(t, ConnectOptions{Lockdown: true})
	h.events.reset()
	if err := h.svc.Disconnect(context.Background(), true); err != nil {
		t.Fatalf("lockdown disconnect: %v", err)
	}
	if cidrs, _ := h.ks.splitState(); len(cidrs) != 0 {
		t.Errorf("idle Lockdown lock still permits %v", cidrs)
	}
	if got := h.eventsWith("ks:"); !slices.Equal(got, []string{"ks:cidrs ", "ks:enable"}) {
		t.Errorf("calls = %v, want the ranges dropped before the lock narrows to the hub", got)
	}
	if h.svc.splitPending() {
		t.Error("pending after the session ended")
	}
}

func TestSplitTunnelDisconnect_LockdownPermitDropFailureIsIncomplete(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.connect(t, ConnectOptions{Lockdown: true})
	h.ks.mu.Lock()
	h.ks.splitCIDRErr = errors.New("filter delete refused")
	h.ks.mu.Unlock()
	if err := h.svc.Disconnect(context.Background(), true); !errors.Is(err, ErrDisconnectIncomplete) {
		t.Errorf("Disconnect() = %v, want ErrDisconnectIncomplete: the idle lock is wider than Lockdown promises", err)
	}
}

func TestSplitTunnelInvariant_IdleLocksNeverHoldRanges(t *testing.T) {
	t.Run("engaged while disconnected", func(t *testing.T) {
		h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
		if err := h.svc.EngageKillSwitch(context.Background(), "", false); err != nil {
			t.Fatal(err)
		}
		h.update(t, ranges("198.51.100.0/24"))
		h.svc.reconcileSplit(context.Background())
		if cidrs, calls := h.ks.splitState(); calls != 0 || len(cidrs) != 0 {
			t.Errorf("idle lock got split permits %v (%d calls)", cidrs, calls)
		}
	})
	t.Run("orphaned lock after a restart", func(t *testing.T) {
		h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
		h.ks.active = true
		h.svc.recoverRecordedSession(platform.KillSwitchState{Active: true})
		if current, detail := h.svc.machine.Get(); current != state.StateError || detail != orphanedLockDetail {
			t.Fatalf("state = %s %q, want the orphaned hold", current, detail)
		}
		h.svc.reconcileSplit(context.Background())
		h.svc.splitTick()
		if cidrs, calls := h.ks.splitState(); calls != 0 || len(cidrs) != 0 {
			t.Errorf("orphaned lock got split permits %v (%d calls)", cidrs, calls)
		}
	})
	t.Run("after a lockdown disconnect", func(t *testing.T) {
		h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
		h.connect(t, ConnectOptions{Lockdown: true})
		if err := h.svc.Disconnect(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		h.update(t, ranges("198.51.100.0/24"))
		h.svc.reconcileSplit(context.Background())
		if cidrs, _ := h.ks.splitState(); len(cidrs) != 0 {
			t.Errorf("idle Lockdown lock got split permits %v from an edit", cidrs)
		}
	})
	t.Run("user disconnect forgets the session's ranges", func(t *testing.T) {
		h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
		h.connect(t, ConnectOptions{})
		if err := h.svc.Disconnect(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if cidrs, _ := h.ks.splitState(); len(cidrs) != 0 {
			t.Errorf("cleared lock still records split permits %v", cidrs)
		}
		h.connect(t, ConnectOptions{})
		if cidrs, _ := h.ks.splitState(); !slices.Equal(cidrs, []string{"203.0.113.0/24"}) {
			t.Errorf("reconnect permits %v, want the stored range again", cidrs)
		}
	})
}

// ---------------------------------------------------------------------------
// Network events and status

func TestSplitTunnelNetworkChanged_RunsBeforeTheEarlyReturn(t *testing.T) {
	h := newSplitHarness(t, nil)
	h.svc.networkKey = func() string { return "" }
	if h.svc.networkLooksUsable() {
		t.Fatal("fixture: the network must look unusable so onNetworkChanged returns early")
	}
	h.svc.onNetworkChanged()
	if got := h.ctl.networkChanges(); got != 1 {
		t.Fatalf("NetworkChanged called %d times, want 1 even when the network just went", got)
	}
	h.svc.onSystemResume(context.Background(), "test resume")
	if got := h.ctl.networkChanges(); got != 2 {
		t.Errorf("NetworkChanged after resume = %d, want 2", got)
	}
	h.svc.splitTick()
	if got := h.ctl.networkChanges(); got != 3 {
		t.Errorf("NetworkChanged after a health tick = %d, want 3", got)
	}
}

func TestSplitTunnelStatus_ReportsCountsAndReasons(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, Apps: []string{"/opt/a", "/opt/b"}, CIDRs: []string{"203.0.113.0/24"}})
	h.ctl.setStatus(splittunnel.Status{AppsActive: true, BypassTCP: 2, BypassUDP: 1})

	st := h.svc.Status(context.Background()).SplitTunnel
	want := state.SplitTunnelStatus{Enabled: true, AppCount: 2, CIDRCount: 1, AppsActive: true, BypassFlows: 3}
	if st == nil || *st != want {
		t.Fatalf("status split block = %+v, want %+v", st, want)
	}

	h.ctl.setStatus(splittunnel.Status{UnavailableReason: splittunnel.ReasonPermitFailed})
	if st := h.svc.Status(context.Background()).SplitTunnel; st.UnavailableReason != splittunnel.ReasonPermitFailed {
		t.Errorf("reason = %q, want the engine's", st.UnavailableReason)
	}

	h.ctl.setStatus(splittunnel.Status{})
	h.svc.splitTunnel.strictRPFn = func() (bool, error) { return true, nil }
	h.svc.refreshStrictReversePath(true)
	if st := h.svc.Status(context.Background()).SplitTunnel; st.UnavailableReason != splittunnel.ReasonStrictReversePath {
		t.Errorf("reason = %q, want strictReversePath", st.UnavailableReason)
	}
	view, _ := h.svc.SplitTunnel()
	if view.UnavailableReason != splittunnel.ReasonStrictReversePath {
		t.Errorf("GET reason = %q, want strictReversePath", view.UnavailableReason)
	}

	h.update(t, splitTunnelUpdate{Enabled: false, Apps: []string{"/opt/a"}, CIDRs: []string{}})
	h.ctl.setStatus(splittunnel.Status{AppsActive: true})
	if st := h.svc.Status(context.Background()).SplitTunnel; st.AppsActive || st.UnavailableReason != "" || st.Enabled {
		t.Errorf("disabled status = %+v, want nothing active and no reason", st)
	}
}

// ---------------------------------------------------------------------------
// Review fixes

// retrySplitNow runs the health tick's retry of a pending apply as if its backoff had passed.
func retrySplitNow(t *testing.T, h *splitHarness) {
	t.Helper()
	h.svc.splitTunnel.mu.Lock()
	h.svc.splitTunnel.retryAt = time.Time{}
	h.svc.splitTunnel.mu.Unlock()
	select {
	case <-h.svc.splitTunnel.kick:
	default:
	}
	h.svc.splitTick()
	select {
	case <-h.svc.splitTunnel.kick:
	default:
		t.Fatal("the health tick did not retry the pending apply")
	}
	h.svc.reconcileSplit(context.Background())
}

func (h *splitHarness) failSplitCIDRs(err error) {
	h.ks.mu.Lock()
	defer h.ks.mu.Unlock()
	h.ks.splitCIDRErr = err
}

// Every kill switch keeps its old ranges when a change fails, so a retry must ask
// again rather than trust the narrower set it asked for.
func TestSplitTunnelReconcile_FailedNarrowingIsRetried(t *testing.T) {
	for name, tc := range map[string]struct {
		stored []string
		update splitTunnelUpdate
		want   []string
	}{
		"range removed":               {[]string{"198.51.100.0/24", "203.0.113.0/24"}, ranges("203.0.113.0/24"), []string{"203.0.113.0/24"}},
		"split tunnelling turned off": {[]string{"203.0.113.0/24"}, splitTunnelUpdate{Enabled: false, Apps: []string{}, CIDRs: []string{"203.0.113.0/24"}}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: tc.stored})
			h.connect(t, ConnectOptions{})
			h.failSplitCIDRs(errors.New("transaction aborted"))
			h.update(t, tc.update)
			h.svc.reconcileSplit(context.Background())
			if !h.svc.splitPending() {
				t.Fatal("a failed narrowing is not reported pending")
			}

			h.failSplitCIDRs(nil)
			_, before := h.ks.splitState()
			retrySplitNow(t, h)
			cidrs, after := h.ks.splitState()
			if after != before+1 || !slices.Equal(cidrs, tc.want) {
				t.Errorf("lock permits %v after the retry (%d new calls), want %v from one call", cidrs, after-before, tc.want)
			}
			if h.svc.splitPending() {
				t.Error("still pending after a successful retry")
			}
		})
	}
}

// A bring-up whose narrowing fails keeps the removed range in the tunnel, and stays
// pending until the lock has really dropped it.
func TestSplitTunnelRebuild_FailedNarrowingKeepsTheRangeTunnelledAndRetries(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"198.51.100.0/24"}})
	h.connect(t, ConnectOptions{})
	h.update(t, splitTunnelUpdate{Enabled: true, Apps: []string{}, CIDRs: []string{}})
	h.failSplitCIDRs(errors.New("transaction aborted"))
	if err := h.svc.rebuildSilentSession(context.Background(), h.profile); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if !tunnelled(allowedIPv4(t, h.startConfig()), "198.51.100.7") {
		t.Error("the rebuilt tunnel routes a removed range around itself")
	}
	if !h.svc.splitPending() {
		t.Fatal("a failed narrowing at bring-up is not reported pending")
	}

	h.failSplitCIDRs(nil)
	_, before := h.ks.splitState()
	retrySplitNow(t, h)
	if cidrs, after := h.ks.splitState(); after == before || len(cidrs) != 0 {
		t.Errorf("lock permits %v after the retry (%d new calls), want nothing", cidrs, after-before)
	}
	if h.svc.splitPending() {
		t.Error("still pending after a successful retry")
	}
}

// With no route to move, the lock goes straight to the new ranges: a range the user
// just removed must not stay open until the next bring-up.
func TestSplitTunnelReconcile_ErrorStateNarrowsTheLockAtOnce(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"198.51.100.0/24"}})
	h.connect(t, ConnectOptions{})
	h.svc.machine.Set(state.StateError, "connection lost")
	h.update(t, splitTunnelUpdate{Enabled: false, Apps: []string{}, CIDRs: []string{"198.51.100.0/24"}})
	h.svc.reconcileSplit(context.Background())

	if cidrs, _ := h.ks.splitState(); len(cidrs) != 0 {
		t.Errorf("lock still permits %v after split tunnelling was turned off", cidrs)
	}
	if !h.svc.splitPending() {
		t.Error("the device's routes have not moved, yet nothing is pending")
	}

	h.svc.machine.Set(state.StateConnected, "tunnel active")
	retrySplitNow(t, h)
	if count, _ := h.applyState(); count != 1 {
		t.Errorf("ApplyAllowedIPs called %d times once connected, want 1", count)
	}
	if h.svc.splitPending() {
		t.Error("still pending after the routes moved")
	}
}

func assertSplitPending(t *testing.T, h *splitHarness, want bool, when string) {
	t.Helper()
	if st := h.svc.Status(context.Background()).SplitTunnel; st == nil || st.Pending != want {
		t.Errorf("%s: status split block = %+v, want pending=%v", when, st, want)
	}
	if view, _ := h.svc.SplitTunnel(); view.Pending != want {
		t.Errorf("%s: GET pending = %v, want %v", when, view.Pending, want)
	}
}

// restartedSplitHarness is a daemon back up under an armed lock with a recorded
// session; *online says whether the host has a route out.
func restartedSplitHarness(t *testing.T, stored *splitTunnelSettings) (*splitHarness, *bool) {
	t.Helper()
	h := newSplitHarness(t, stored)
	stubSessionRecordStore(t).set(sessionRecord{ProfileID: h.profile.ID})
	h.ks.active = true
	online := new(bool)
	h.svc.hostInternet = func() (bool, bool) { return *online, true }
	h.svc.physicalRoute = func() (string, string, error) {
		if *online {
			return "eth0", "192.0.2.1", nil
		}
		return "", "", platform.ErrNoDefaultRoute
	}
	h.svc.recoverRecordedSession(platform.KillSwitchState{Active: true})
	return h, online
}

// After a restart nothing the user saved is waiting, whether split tunnelling is used
// or not; only an edit made before the first bring-up is.
func TestSplitTunnelRestart_NothingPendingUntilAnEdit(t *testing.T) {
	for name, stored := range map[string]*splitTunnelSettings{
		"never used":  nil,
		"with ranges": {Enabled: true, CIDRs: []string{"203.0.113.0/24"}},
	} {
		t.Run(name, func(t *testing.T) {
			h, online := restartedSplitHarness(t, stored)
			h.svc.runHealthCheck(context.Background())
			if current, _ := h.svc.machine.Get(); current != state.StateError {
				t.Fatalf("state = %s, want the restart held in ERROR while offline", current)
			}
			assertSplitPending(t, h, false, "before the first bring-up")

			h.update(t, ranges("198.51.100.0/24"))
			assertSplitPending(t, h, true, "after an edit")

			*online = true
			h.svc.runHealthCheck(context.Background())
			if current, _ := h.svc.machine.Get(); current != state.StateConnected {
				t.Fatalf("state = %s, want the session rebuilt", current)
			}
			assertSplitPending(t, h, false, "after the rebuild")
		})
	}
}

func TestSplitTunnelRestart_FailedPermitAtBringUpStaysPending(t *testing.T) {
	h, online := restartedSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
	h.failSplitCIDRs(errors.New("filter add refused"))
	*online = true
	h.svc.runHealthCheck(context.Background())
	if current, _ := h.svc.machine.Get(); current != state.StateConnected {
		t.Fatalf("state = %s, want the session rebuilt", current)
	}
	if !tunnelled(allowedIPv4(t, h.startConfig()), "203.0.113.7") {
		t.Error("a range the lock refused is routed around the tunnel")
	}
	assertSplitPending(t, h, true, "after a refused permit")
}

// A rebuild that loses opMu to the reconciler must leave the reconciler's cancel in
// place, or a Disconnect queues behind the whole apply.
func TestSplitTunnelReconcile_BusyRebuildKeepsItInterruptible(t *testing.T) {
	h := newSplitHarness(t, nil)
	h.connect(t, ConnectOptions{})
	h.update(t, ranges("203.0.113.0/24"))
	entered := make(chan struct{})
	var once sync.Once
	h.wg.mu.Lock()
	h.wg.applyHook = func(ctx context.Context) error {
		once.Do(func() { close(entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}
	h.wg.mu.Unlock()
	done := make(chan struct{})
	go func() {
		h.svc.reconcileSplit(context.Background())
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the reconciler never reached the apply")
	}

	if err := h.svc.rebuildSilentSession(context.Background(), h.profile); !errors.Is(err, errRebuildBusy) {
		t.Fatalf("rebuild during a reconcile = %v, want errRebuildBusy", err)
	}
	started := time.Now()
	if err := h.svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("Disconnect waited %s behind a range apply after a busy rebuild", elapsed)
	}
	<-done
}

// An engine failure is no news while there is no app to exclude.
func TestSplitTunnelStatus_NoReasonWithoutAppsToExclude(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: false, Apps: []string{"/opt/a"}})
	h.ctl.setStatus(splittunnel.Status{UnavailableReason: splittunnel.ReasonEgressFailed})
	check := func(want, when string) {
		t.Helper()
		if st := h.svc.Status(context.Background()).SplitTunnel; st == nil || st.UnavailableReason != want {
			t.Errorf("%s: status split block = %+v, want reason %q", when, st, want)
		}
		if view, _ := h.svc.SplitTunnel(); view.UnavailableReason != want {
			t.Errorf("%s: GET reason = %q, want %q", when, view.UnavailableReason, want)
		}
	}
	check("", "split tunnelling off")
	h.update(t, ranges("203.0.113.0/24"))
	check("", "no apps")
	h.update(t, splitTunnelUpdate{Enabled: true, Apps: []string{"/opt/a"}, CIDRs: []string{}})
	check(splittunnel.ReasonEgressFailed, "apps excluded")
}

// A lock never asked for ranges has none to drop: tearing a session down must not
// re-render it, nor fail on a render it never needed.
func TestSplitTunnelDisconnect_NoRangeCallWhenTheLockHoldsNone(t *testing.T) {
	for name, stored := range map[string]*splitTunnelSettings{
		"never used": nil,
		"off":        {Enabled: false, CIDRs: []string{"203.0.113.0/24"}},
	} {
		t.Run(name+"/lockdown", func(t *testing.T) {
			h := newSplitHarness(t, stored)
			h.connect(t, ConnectOptions{Lockdown: true})
			h.failSplitCIDRs(errors.New("nft: transient"))
			if err := h.svc.Disconnect(context.Background(), true); err != nil {
				t.Errorf("Lockdown disconnect = %v; the lock never held a range", err)
			}
			if _, calls := h.ks.splitState(); calls != 0 {
				t.Errorf("SetSplitCIDRs called %d times", calls)
			}
		})
		t.Run(name+"/failed clear", func(t *testing.T) {
			h := newSplitHarness(t, stored)
			h.connect(t, ConnectOptions{})
			h.ks.mu.Lock()
			h.ks.clearErr = errors.New("remove iptables rules: timeout")
			h.ks.mu.Unlock()
			_ = h.svc.Disconnect(context.Background(), false)
			if _, calls := h.ks.splitState(); calls != 0 {
				t.Errorf("SetSplitCIDRs called %d times, re-rendering a half-cleared lock", calls)
			}
		})
	}
	t.Run("a failed narrowing still gets dropped", func(t *testing.T) {
		h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
		h.connect(t, ConnectOptions{Lockdown: true})
		h.failSplitCIDRs(errors.New("transaction aborted"))
		h.update(t, splitTunnelUpdate{Enabled: false, Apps: []string{}, CIDRs: []string{}})
		h.svc.reconcileSplit(context.Background())
		h.failSplitCIDRs(nil)
		if err := h.svc.Disconnect(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		if cidrs, _ := h.ks.splitState(); len(cidrs) != 0 {
			t.Errorf("idle Lockdown lock still permits %v the failed narrowing left", cidrs)
		}
	})
	t.Run("ranges already dropped live", func(t *testing.T) {
		h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"203.0.113.0/24"}})
		h.connect(t, ConnectOptions{Lockdown: true})
		h.update(t, splitTunnelUpdate{Enabled: false, Apps: []string{}, CIDRs: []string{}})
		h.svc.reconcileSplit(context.Background())
		_, before := h.ks.splitState()
		if err := h.svc.Disconnect(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		if _, after := h.ks.splitState(); after != before {
			t.Errorf("SetSplitCIDRs called %d more times for a lock already holding none", after-before)
		}
	})
}

// idleTUN is a tunnel device nobody reads, enough for the engine to count a live device.
type idleTUN struct {
	events chan tun.Event
	closed chan struct{}
	once   sync.Once
}

func newIdleTUN() *idleTUN {
	return &idleTUN{events: make(chan tun.Event), closed: make(chan struct{})}
}

func (d *idleTUN) File() *os.File { return nil }
func (d *idleTUN) Read([][]byte, []int, int) (int, error) {
	<-d.closed
	return 0, os.ErrClosed
}
func (d *idleTUN) Write(bufs [][]byte, _ int) (int, error) { return len(bufs), nil }
func (d *idleTUN) MTU() (int, error)                       { return 1420, nil }
func (d *idleTUN) Name() (string, error)                   { return "idle0", nil }
func (d *idleTUN) Events() <-chan tun.Event                { return d.events }
func (d *idleTUN) BatchSize() int                          { return 1 }
func (d *idleTUN) Close() error {
	d.once.Do(func() { close(d.closed) })
	return nil
}

// The health tick must not cut a failed egress permit's backoff short; a real network
// change still retries it at once.
func TestSplitTunnelTick_LeavesAFailedPermitToItsBackoff(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return attempts
	}
	ctl := splittunnel.NewController(splittunnel.ControllerOptions{
		SetEgressPermit: func(context.Context, bool) error {
			mu.Lock()
			defer mu.Unlock()
			attempts++
			return errors.New("filter add failed")
		},
	})
	dev := ctl.WrapTUN(newIdleTUN(), wg.TunnelInfo{Name: "idle0", Addresses: []netip.Addr{netip.MustParseAddr("10.66.0.2")}, MTU: 1420})
	t.Cleanup(func() {
		_ = dev.Close()
		_ = ctl.Close()
	})
	app := map[string]string{"windows": `C:\Games\Foo\foo.exe`, "darwin": "/Applications/Foo.app"}[runtime.GOOS]
	if app == "" {
		app = "/opt/foo/foo"
	}
	h := newSplitHarness(t, nil)
	h.svc.SetSplitTunnel(ctl, h.path)
	h.update(t, splitTunnelUpdate{Enabled: true, Apps: []string{app}, CIDRs: []string{}})
	waitUntil(t, 5*time.Second, "the egress permit to fail", func() bool {
		return ctl.Status().UnavailableReason == splittunnel.ReasonPermitFailed
	})

	base := count()
	for range 10 {
		h.svc.splitTick()
	}
	time.Sleep(100 * time.Millisecond)
	if extra := count() - base; extra != 0 {
		t.Errorf("10 health ticks made %d more permit attempts; a failed permit has its own 30 s backoff", extra)
	}
	h.svc.splitNetworkChanged()
	waitUntil(t, 5*time.Second, "a network change to retry the permit", func() bool { return count() > base })
}

func TestSplitRevertAfterFailedApplyReappliesRoutes(t *testing.T) {
	h := newSplitHarness(t, &splitTunnelSettings{Enabled: true, CIDRs: []string{"198.51.100.0/24"}})
	h.connect(t, ConnectOptions{})
	h.wg.mu.Lock()
	h.wg.applyErr = errors.New("route sync: context deadline exceeded")
	h.wg.mu.Unlock()
	h.update(t, ranges("203.0.113.0/24"))
	h.svc.reconcileSplit(context.Background())
	h.wg.mu.Lock()
	h.wg.applyErr = nil
	h.wg.mu.Unlock()
	h.events.reset()
	h.update(t, ranges("198.51.100.0/24"))
	h.svc.reconcileSplit(context.Background())
	if !slices.Contains(h.eventsWith("ks:", "wg:"), "wg:apply") {
		t.Fatalf("reverting after a failed apply never re-synced the routes: %v", h.eventsWith("ks:", "wg:"))
	}
}

func TestSplitStoreRefusesRangesThatOverflowWithAllowLANOff(t *testing.T) {
	cidrs := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		cidrs = append(cidrs, fmt.Sprintf("10.%d.%d.96/32", i*4, (i*37)%256))
	}
	_, invalid := normalizeSplitCIDRList(cidrs)
	if !slices.ContainsFunc(invalid, func(e splitTunnelInvalid) bool { return e.Code == "tooManyRoutes" }) {
		t.Fatalf("64 hosts spread over 10.0.0.0/8 passed validation but overflow the route budget with Allow LAN off: %v", invalid)
	}
}
