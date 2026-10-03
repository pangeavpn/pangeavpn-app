package egress

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(s))
	for _, a := range s {
		out = append(out, netip.MustParseAddr(a))
	}
	return out
}

func TestIdentityEqual(t *testing.T) {
	a := Identity{Index: 3, Name: "eth0", Addrs: addrs("192.0.2.1", "192.0.2.2")}
	if !a.Equal(Identity{Index: 3, Name: "eth0", Addrs: addrs("192.0.2.1", "192.0.2.2")}) {
		t.Fatal("identical identities differ")
	}
	for _, b := range []Identity{
		{Index: 4, Name: "eth0", Addrs: a.Addrs},
		{Index: 3, Name: "eth1", Addrs: a.Addrs},
		{Index: 3, Name: "eth0", Addrs: addrs("192.0.2.1")},
	} {
		if a.Equal(b) {
			t.Fatalf("%+v equals %+v", a, b)
		}
	}
	if !(Identity{Index: 1}).Equal(Identity{Index: 1, Addrs: []netip.Addr{}}) {
		t.Fatal("nil and empty address lists differ")
	}
}

func TestSortedV4(t *testing.T) {
	got := sortedV4(addrs("192.0.2.9", "::ffff:192.0.2.1", "2001:db8::1", "192.0.2.9", "10.0.0.1"))
	want := addrs("10.0.0.1", "192.0.2.1", "192.0.2.9")
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sortedV4 = %v, want %v", got, want)
	}
}

func TestIdentityCacheRefresh(t *testing.T) {
	type step struct {
		id  Identity
		err error
	}
	a := Identity{Index: 5, Name: "Wi-Fi", Addrs: addrs("192.0.2.10")}
	b := Identity{Index: 9, Name: "Ethernet", Addrs: addrs("198.51.100.4")}
	var next step
	c := newIdentityCache(func() (Identity, error) { return next.id, next.err })

	if _, err := c.current(); !errors.Is(err, ErrNoInterface) {
		t.Fatalf("unresolved current err = %v, want ErrNoInterface", err)
	}
	check := func(s step, wantOld, wantCur Identity, wantChanged bool) {
		t.Helper()
		next = s
		old, cur, changed, err := c.refresh()
		if !errors.Is(err, s.err) || !old.Equal(wantOld) || !cur.Equal(wantCur) || changed != wantChanged {
			t.Fatalf("refresh = (%+v, %+v, %v, %v), want (%+v, %+v, %v, %v)", old, cur, changed, err, wantOld, wantCur, wantChanged, s.err)
		}
	}
	check(step{err: errors.New("table read failed")}, Identity{}, Identity{}, false)
	check(step{id: a}, Identity{}, a, true)
	check(step{id: Identity{Index: 5, Name: "Wi-Fi", Addrs: addrs("192.0.2.10")}}, a, a, false)
	check(step{err: os.ErrDeadlineExceeded}, a, a, false)
	if got, err := c.current(); err != nil || !got.Equal(a) {
		t.Fatalf("transient lookup failure dropped the identity: %+v, %v", got, err)
	}
	check(step{id: b}, a, b, true)
	check(step{err: ErrNoInterface}, b, Identity{}, true)
	if _, err := c.current(); !errors.Is(err, ErrNoInterface) {
		t.Fatalf("current after loss = %v, want ErrNoInterface", err)
	}
	check(step{err: ErrNoInterface}, Identity{}, Identity{}, false)
	check(step{id: b}, Identity{}, b, true)

	got, err := c.current()
	if err != nil || !got.Equal(b) {
		t.Fatalf("current = %+v, %v", got, err)
	}
	got.Addrs[0] = netip.MustParseAddr("203.0.113.1")
	if again, _ := c.current(); !again.Equal(b) {
		t.Fatal("caller mutation leaked into the cache")
	}
}

func TestTCPTarget(t *testing.T) {
	cases := map[string]string{
		"192.0.2.1:443":          "192.0.2.1:443",
		"[::ffff:192.0.2.1]:443": "192.0.2.1:443",
		"[2001:db8::1]:443":      "",
		"0.0.0.0:443":            "",
		"192.0.2.1:0":            "",
	}
	for in, want := range cases {
		got, err := tcpTarget(netip.MustParseAddrPort(in))
		if want == "" {
			if err == nil {
				t.Fatalf("tcpTarget(%s) = %s, want error", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Fatalf("tcpTarget(%s) = %q, %v, want %q", in, got, err, want)
		}
	}
}

func TestIsUnreachableWrapping(t *testing.T) {
	if len(unreachableErrnos) == 0 {
		t.Skip("no errno list on this OS")
	}
	for _, errno := range unreachableErrnos {
		wrapped := &net.OpError{Op: "dial", Net: "tcp4", Err: os.NewSyscallError("connect", errno)}
		if !IsUnreachable(wrapped) || !IsUnreachable(fmt.Errorf("ctx: %w", wrapped)) {
			t.Fatalf("IsUnreachable(%v) = false", wrapped)
		}
		if !IsUnreachable(responseFor(1, wrapped).error()) {
			t.Fatalf("errno %d lost across the broker response", errno)
		}
	}
	for _, err := range []error{nil, errors.New("network is unreachable"), os.ErrDeadlineExceeded, ErrNoInterface, ErrBrokerUnavailable} {
		if IsUnreachable(err) {
			t.Fatalf("IsUnreachable(%v) = true", err)
		}
	}
}

func TestBrokerFrames(t *testing.T) {
	req := brokerRequest{ID: 7, Op: brokerOpTCP, IfIndex: 4, Addr: "192.0.2.1:443", TimeoutMs: 1500}
	frame, err := encodeFrame(req)
	if err != nil {
		t.Fatal(err)
	}
	stream := append(append([]byte(nil), frame...), frame[:3]...)
	body, rest, ok, err := splitFrame(stream)
	if err != nil || !ok {
		t.Fatalf("splitFrame: ok=%v err=%v", ok, err)
	}
	got, err := decodeRequest(body)
	if err != nil || got != req {
		t.Fatalf("decoded %+v, %v; want %+v", got, err, req)
	}
	if _, _, ok, err := splitFrame(rest); ok || err != nil {
		t.Fatalf("partial frame: ok=%v err=%v", ok, err)
	}
	for _, bad := range [][]byte{{0, 0, 0, 0}, {0, 1, 0, 0, '{'}} {
		if _, _, _, err := splitFrame(bad); !errors.Is(err, errBrokerFrame) {
			t.Fatalf("splitFrame(%v) err = %v, want errBrokerFrame", bad, err)
		}
	}
	if _, err := encodeFrame(brokerRequest{Addr: strings.Repeat("x", brokerMaxFrame)}); !errors.Is(err, errBrokerFrame) {
		t.Fatalf("oversized frame err = %v", err)
	}
	if _, err := decodeResponse([]byte(`{"err":"x"}`)); err == nil {
		t.Fatal("response without id accepted")
	}
	resp, err := decodeResponse([]byte(`{"id":3,"err":"boom","errno":1}`))
	if err != nil || resp.ID != 3 || resp.error() == nil || !strings.Contains(resp.error().Error(), "boom") {
		t.Fatalf("decodeResponse = %+v, %v", resp, err)
	}
	if (brokerResponse{ID: 3}).error() != nil {
		t.Fatal("success response reported an error")
	}
}

func TestBrokerRequestValidate(t *testing.T) {
	ok := []brokerRequest{
		{ID: 1, Op: brokerOpTCP, IfIndex: 4, Addr: "192.0.2.1:443"},
		{ID: 1, Op: brokerOpUDP, IfIndex: 4},
		{ID: 1, Op: brokerOpUDP, IfIndex: 4, Addr: "ignored"},
	}
	for _, r := range ok {
		if _, err := r.validate(); err != nil {
			t.Fatalf("validate(%+v) = %v", r, err)
		}
	}
	bad := []brokerRequest{
		{ID: 1, Op: brokerOpTCP, IfIndex: 0, Addr: "192.0.2.1:443"},
		{ID: 1, Op: brokerOpTCP, IfIndex: 4, Addr: "[2001:db8::1]:443"},
		{ID: 1, Op: brokerOpTCP, IfIndex: 4, Addr: "0.0.0.0:443"},
		{ID: 1, Op: brokerOpTCP, IfIndex: 4, Addr: "192.0.2.1:0"},
		{ID: 1, Op: brokerOpTCP, IfIndex: 4, Addr: "example.com:443"},
		{ID: 1, Op: "raw", IfIndex: 4},
	}
	for _, r := range bad {
		if _, err := r.validate(); err == nil {
			t.Fatalf("validate(%+v) accepted", r)
		}
	}
	if d := (brokerRequest{}).timeout(); d != brokerDefaultTimeout {
		t.Fatalf("default timeout = %v", d)
	}
	if d := (brokerRequest{TimeoutMs: 1500}).timeout(); d != 1500*time.Millisecond {
		t.Fatalf("timeout = %v", d)
	}
	if d := (brokerRequest{TimeoutMs: 1 << 40}).timeout(); d != brokerMaxTimeout {
		t.Fatalf("capped timeout = %v", d)
	}
}
