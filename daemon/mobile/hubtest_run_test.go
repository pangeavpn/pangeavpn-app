//go:build android || ios

package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// recordingProxy stands in for a started sing-box proxy and counts its stops.
type recordingProxy struct{ stops atomic.Int32 }

func (p *recordingProxy) Stop(context.Context) error    { p.stops.Add(1); return nil }
func (p *recordingProxy) Credentials() (string, string) { return "u", "p" }

// decodeHubTest is curried so TestHubMethod's two results can be passed straight in.
func decodeHubTest(t *testing.T) func(string, error) hubTestResult {
	return func(raw string, err error) hubTestResult {
		t.Helper()
		if err != nil {
			t.Fatalf("TestHubMethod: %v", err)
		}
		var out hubTestResult
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return out
	}
}

func installRoute(route *hubPath) {
	hubMu.Lock()
	activeHubPath = route
	hubMu.Unlock()
}

func swapHubTestResolve(t *testing.T, fn func() (string, error)) {
	t.Helper()
	previous := hubTestResolve
	hubTestResolve = fn
	t.Cleanup(func() { hubTestResolve = previous })
}

// closedPort is a loopback port nothing listens on, so a dial fails at once.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func TestTestHubMethodRejectsUnknown(t *testing.T) {
	if _, err := TestHubMethod("carrier-pigeon"); err == nil || err.Error() != `unknown hub method "carrier-pigeon"` {
		t.Fatalf("got %v, want an unknown method error", err)
	}
}

func TestTestHubMethodReportsBusy(t *testing.T) {
	hubTestBusy.Store(true)
	t.Cleanup(func() { hubTestBusy.Store(false) })
	got, err := TestHubMethod("reality")
	if err != nil || got != `{"method":"reality","ok":false,"ms":0,"unavailable":"busy"}` {
		t.Fatalf("got %s, %v, want busy", got, err)
	}
}

func TestTestHubMethodBorrowsTheActiveRoute(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	proxy := &recordingProxy{}
	route := &hubPath{kind: "reality", detail: "fake", base: h.srv.URL, client: h.srv.Client(), proxy: proxy}
	installRoute(route)

	got := decodeHubTest(t)(TestHubMethod("reality"))
	if !got.OK || got.Detail != "fake" || got.Method != "reality" || got.Unavailable != "" {
		t.Fatalf("got %+v, want ok over the borrowed route", got)
	}
	if currentHubRoute() != route || proxy.stops.Load() != 0 {
		t.Fatalf("route %p (want %p), %d stops: the borrowed route was disturbed", currentHubRoute(), route, proxy.stops.Load())
	}
	if probes := h.requestsTo(probeRoute); len(probes) != 1 {
		t.Fatalf("got %d probes, want 1 over the borrowed route", len(probes))
	}
}

func TestTestHubMethodNeverClosesTheActiveRoute(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(probeRoute, replyWith(http.StatusOK, []serverInfo{}))
	// The node fails the test's probe, then recovers, as a flaky route does.
	var failing atomic.Bool
	failing.Store(true)
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		h.srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(flaky.Close)
	proxy := &recordingProxy{}
	route := &hubPath{kind: "reality", detail: "node", base: flaky.URL, client: flaky.Client(), proxy: proxy}
	installRoute(route)

	got := decodeHubTest(t)(TestHubMethod("reality"))
	if got.OK || got.Detail != "" {
		t.Fatalf("got %+v, want a failure", got)
	}
	if currentHubRoute() != route || proxy.stops.Load() != 0 {
		t.Fatalf("route %p (want %p), %d stops: a failed test tore down the live route", currentHubRoute(), route, proxy.stops.Load())
	}
	hubMu.Lock()
	failures := hubTrack
	hubMu.Unlock()
	if failures != (hubTracker{}) {
		t.Fatalf("got tracker %+v, want a test to leave it alone", failures)
	}

	failing.Store(false)
	if err := hubRequest(http.MethodGet, probeRoute, nil, nil); err != nil {
		t.Fatalf("the route the test failed on is unusable afterwards: %v", err)
	}
	if currentHubRoute() != route || len(h.requestsTo(probeRoute)) != 1 {
		t.Fatalf("route %p (want %p), %d requests: the request did not ride the same route",
			currentHubRoute(), route, len(h.requestsTo(probeRoute)))
	}
}

func TestTestHubMethodProbesADirectAddressWithoutSavingIt(t *testing.T) {
	s, _ := initForTest(t, map[string]string{keyHubIP: "not-an-ip"})
	h := newFakeHub(t)
	direct := httptest.NewTLSServer(h.srv.Config.Handler)
	t.Cleanup(direct.Close)
	invalidateHubPath()
	addr := strings.TrimPrefix(direct.URL, "https://")
	swapHubTestResolve(t, func() (string, error) { return addr, nil })

	got := decodeHubTest(t)(TestHubMethod("directIp"))
	if !got.OK || got.Detail != addr {
		t.Fatalf("got %+v, want ok via %s", got, addr)
	}
	if stored := s.Get(keyHubIP); stored != "not-an-ip" {
		t.Fatalf("got stored hub IP %q, want it untouched", stored)
	}
	if currentHubRoute() != nil {
		t.Fatal("a method test installed its path as the live route")
	}
}

func TestTestHubMethodReportsNoAddress(t *testing.T) {
	initForTest(t, nil)
	swapHubTestResolve(t, func() (string, error) { return "", errors.New("DoH resolution failed for all providers") })
	got := decodeHubTest(t)(TestHubMethod("directIp"))
	if got.OK || got.Unavailable != "noAddress" {
		t.Fatalf("got %+v, want noAddress", got)
	}
}

func TestTestProxyMethodStopsEveryProxyItStarts(t *testing.T) {
	initForTest(t, nil)
	if got := testProxyMethod("reality", nil, nil, nil); got.Unavailable != "noCredentials" {
		t.Fatalf("got %+v, want noCredentials", got)
	}

	failed, unbound, unreachable := &recordingProxy{}, &recordingProxy{}, &recordingProxy{}
	port := closedPort(t)
	starts := []func() (hubProxy, int, error){
		func() (hubProxy, int, error) { return failed, 0, errors.New("handshake timeout") },
		func() (hubProxy, int, error) { return unbound, 0, nil },
		func() (hubProxy, int, error) { return unreachable, port, nil },
		func() (hubProxy, int, error) { return nil, 0, errors.New("no manager") },
	}
	got := testProxyMethod("reality", []string{"a", "b", "c", "d"}, func(i int) (hubProxy, int, error) { return starts[i]() }, nil)
	if got.OK || got.Unavailable != "" {
		t.Fatalf("got %+v, want a plain failure", got)
	}
	for name, p := range map[string]*recordingProxy{"failed": failed, "unbound": unbound, "unreachable": unreachable} {
		if n := p.stops.Load(); n != 1 {
			t.Errorf("%s proxy stopped %d times, want once", name, n)
		}
	}
}

func TestTestFrontedMethodWithoutRelays(t *testing.T) {
	if got := testFrontedMethod(nil); got.OK || got.Unavailable != "noRelay" {
		t.Fatalf("got %+v, want noRelay", got)
	}
}

func TestHubStatusNamesTheActiveRoute(t *testing.T) {
	initForTest(t, nil)
	installHubRoute()
	if got := HubStatus(); !strings.HasSuffix(got, `"active":"normal","detail":null}`) {
		t.Fatalf("got %s, want normal with no detail", got)
	}
	newFakeHub(t)
	if got := HubStatus(); !strings.HasSuffix(got, `"active":"normal","detail":"fake"}`) {
		t.Fatalf("got %s, want normal via fake", got)
	}
	invalidateHubPath()
	if got := HubStatus(); !strings.HasSuffix(got, `"active":null,"detail":null}`) {
		t.Fatalf("got %s, want no active route", got)
	}
}

func TestHubStatusReportsTheSwitches(t *testing.T) {
	initForTest(t, nil)
	if _, err := SetHubMethod("normal", true); err != nil {
		t.Fatalf("SetHubMethod: %v", err)
	}
	if _, err := SetHubMethod("fronted", false); err != nil {
		t.Fatalf("SetHubMethod: %v", err)
	}
	raw, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	var cfg struct {
		HubMethods json.RawMessage `json:"hubMethods"`
	}
	var status struct {
		Methods json.RawMessage `json:"methods"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if err := json.Unmarshal([]byte(HubStatus()), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if string(status.Methods) != string(cfg.HubMethods) {
		t.Fatalf("got methods %s, want GetConfig's %s", status.Methods, cfg.HubMethods)
	}
}
