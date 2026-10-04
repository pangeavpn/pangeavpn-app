package egress

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeRouteTable struct {
	routes []ribRoute
	calls  [][]string
	fail   map[string]error
	logs   []string
	clock  time.Time
}

func (f *fakeRouteTable) mirror() *routeMirror {
	names := map[int]string{4: "en0", 5: "en5", 9: "utun3"}
	m := newRouteMirror(
		func() ([]ribRoute, error) { return slices.Clone(f.routes), nil },
		func(args ...string) error {
			f.calls = append(f.calls, args)
			return f.fail[args[1]]
		},
		func(i int) (string, bool) { return names[i], names[i] != "" },
		newRateLog(func(format string, args ...any) { f.logs = append(f.logs, format) }),
	)
	f.clock = time.Unix(1000, 0)
	m.now = func() time.Time { return f.clock }
	return m
}

func (f *fakeRouteTable) takeCalls() [][]string {
	calls := f.calls
	f.calls = nil
	return calls
}

var (
	mirrorGW  = netip.MustParseAddr("192.168.1.1")
	mirrorGW2 = netip.MustParseAddr("10.0.0.1")
)

func primaryRoute(index int, next netip.Addr) ribRoute {
	return ribRoute{Index: index, DefaultV4: true, Up: true, Gateway: true, NextHop: next}
}

func mirrorRoute(index int, next netip.Addr) ribRoute {
	r := primaryRoute(index, next)
	r.IfScope, r.Mirror = true, true
	return r
}

func expectCalls(t *testing.T, got, want [][]string) {
	t.Helper()
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestMirrorAddsScopedCopyOfPrimary(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{{Index: 9, Up: true}, primaryRoute(4, mirrorGW)}}
	f.mirror().sync(f.routes)
	expectCalls(t, f.calls, [][]string{{"-n", "add", "-ifscope", "en0", "-proto2", "default", "192.168.1.1"}})
}

func TestMirrorReplacesStaleOnPrimaryBeforeAdding(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(4, mirrorGW2), mirrorRoute(4, mirrorGW)}}
	f.mirror().sync(f.routes)
	expectCalls(t, f.calls, [][]string{
		{"-n", "delete", "-ifscope", "en0", "default", "192.168.1.1"},
		{"-n", "add", "-ifscope", "en0", "-proto2", "default", "10.0.0.1"},
	})
}

func TestMirrorIdleWhenInPlace(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(4, mirrorGW), mirrorRoute(4, mirrorGW)}}
	m := f.mirror()
	m.sync(f.routes)
	m.sync(f.routes)
	expectCalls(t, f.calls, nil)
}

func TestMirrorDefersDeleteOffPrimary(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(5, mirrorGW2), mirrorRoute(4, mirrorGW)}}
	m := f.mirror()
	m.sync(f.routes)
	expectCalls(t, f.takeCalls(), [][]string{{"-n", "add", "-ifscope", "en5", "-proto2", "default", "10.0.0.1"}})

	f.routes = append(f.routes, mirrorRoute(5, mirrorGW2))
	f.clock = f.clock.Add(time.Second)
	m.sync(f.routes)
	expectCalls(t, f.takeCalls(), nil)

	f.clock = f.clock.Add(mirrorStaleGrace)
	m.sync(f.routes)
	expectCalls(t, f.takeCalls(), [][]string{{"-n", "delete", "-ifscope", "en0", "default", "192.168.1.1"}})
}

func TestMirrorTakenOverOffPrimaryIsNotDeleted(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(5, mirrorGW2), mirrorRoute(5, mirrorGW2), mirrorRoute(4, mirrorGW)}}
	m := f.mirror()
	m.sync(f.routes)
	// configd's EEXIST path replaced it with its own route, which carries no RTF_PROTO2.
	configd := primaryRoute(4, mirrorGW)
	configd.IfScope = true
	f.routes = []ribRoute{primaryRoute(5, mirrorGW2), mirrorRoute(5, mirrorGW2), configd}
	f.clock = f.clock.Add(2 * mirrorStaleGrace)
	m.sync(f.routes)
	expectCalls(t, f.calls, nil)
	if len(m.staleSince) != 0 {
		t.Fatalf("stale entry kept for a mirror that is gone: %v", m.staleSince)
	}
}

func TestMirrorUnwantedRemovesAndReturns(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(4, mirrorGW), mirrorRoute(4, mirrorGW)}}
	m := f.mirror()
	m.setWanted(false)
	m.sync(f.routes)
	expectCalls(t, f.takeCalls(), [][]string{{"-n", "delete", "-ifscope", "en0", "default", "192.168.1.1"}})

	f.routes = []ribRoute{primaryRoute(4, mirrorGW)}
	m.sync(f.routes)
	expectCalls(t, f.takeCalls(), nil)

	m.setWanted(true)
	m.sync(f.routes)
	expectCalls(t, f.takeCalls(), [][]string{{"-n", "add", "-ifscope", "en0", "-proto2", "default", "192.168.1.1"}})
}

func TestMirrorCloseRemovesAndStopsSyncing(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(5, mirrorGW2), mirrorRoute(4, mirrorGW), mirrorRoute(5, mirrorGW2)}}
	m := f.mirror()
	m.close()
	expectCalls(t, f.takeCalls(), [][]string{
		{"-n", "delete", "-ifscope", "en0", "default", "192.168.1.1"},
		{"-n", "delete", "-ifscope", "en5", "default", "10.0.0.1"},
	})
	f.routes = []ribRoute{primaryRoute(4, mirrorGW)}
	m.sync(f.routes)
	m.close()
	expectCalls(t, f.calls, nil)
}

func TestMirrorDeleteAlwaysScoped(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(9, mirrorGW), mirrorRoute(4, mirrorGW), mirrorRoute(5, mirrorGW2)}}
	m := f.mirror()
	m.setWanted(false)
	m.sync(f.routes)
	f.clock = f.clock.Add(mirrorStaleGrace)
	m.sync(f.routes)
	m.close()
	if len(f.calls) == 0 {
		t.Fatal("expected deletes")
	}
	for _, c := range f.calls {
		i := slices.Index(c, "-ifscope")
		if i < 0 || i+1 >= len(c) || c[i+1] == "" {
			t.Fatalf("route(8) call without an interface scope: %q", c)
		}
	}
}

func TestMirrorAddFailureBacksOff(t *testing.T) {
	f := &fakeRouteTable{routes: []ribRoute{primaryRoute(4, mirrorGW)}, fail: map[string]error{"add": errors.New("refused (route: writing to routing socket: Network is unreachable)")}}
	m := f.mirror()
	m.sync(f.routes)
	if len(f.takeCalls()) != 1 || len(f.logs) != 1 || !strings.Contains(f.logs[0], "failed") {
		t.Fatalf("logs = %q, want one logged failure", f.logs)
	}
	f.clock = f.clock.Add(time.Second)
	m.sync(f.routes)
	expectCalls(t, f.takeCalls(), nil)

	f.clock = f.clock.Add(mirrorAddBackoff)
	m.sync(f.routes)
	if len(f.takeCalls()) != 1 {
		t.Fatal("add not retried after the backoff")
	}
}

func TestRouteResult(t *testing.T) {
	refused := "route: writing to routing socket: File exists\nadd net default: gateway 192.168.1.1: File exists\n"
	if err := routeResult([]byte(refused), nil); err == nil || !strings.Contains(err.Error(), "File exists") {
		t.Fatalf("a refusal that exits 0 must fail: %v", err)
	}
	if err := routeResult([]byte("add net default: gateway 192.168.1.1\n"), nil); err != nil {
		t.Fatalf("success reported as %v", err)
	}
	if err := routeResult([]byte("route: bad interface name\n"), errors.New("exit status 1")); err == nil {
		t.Fatal("a non-zero exit must fail")
	}
}
