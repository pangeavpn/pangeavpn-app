package nicpin

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"testing"
)

func stubLookup(t *testing.T, ips []net.IP, err error) *[]string {
	t.Helper()
	var asked []string
	orig := lookupIP
	t.Cleanup(func() { lookupIP = orig })
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		asked = append(asked, host)
		return ips, err
	}
	return &asked
}

func TestResolve_KeepsPublicIPv4Only(t *testing.T) {
	stubLookup(t, []net.IP{
		net.ParseIP("10.0.0.1"), net.ParseIP("2606:4700::6810:18c8"), net.ParseIP("172.67.220.125"),
		net.ParseIP("104.21.24.200"), net.ParseIP("172.67.220.125"), net.ParseIP("127.0.0.1"),
	}, nil)
	got, err := resolve(context.Background(), "sync.cdn.example")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Addr{netip.MustParseAddr("172.67.220.125"), netip.MustParseAddr("104.21.24.200")}
	if !slices.Equal(got, want) {
		t.Fatalf("resolve() = %v, want %v", got, want)
	}
}

func TestResolve_LiteralNeedsNoLookup(t *testing.T) {
	asked := stubLookup(t, nil, errors.New("must not be called"))
	got, err := resolve(context.Background(), " 104.21.24.200 ")
	if err != nil || len(got) != 1 || got[0].String() != "104.21.24.200" || len(*asked) != 0 {
		t.Fatalf("resolve() = %v, %v after %v lookups", got, err, *asked)
	}
}

// Nothing safe to pin: the dial must fail rather than go out unpinned.
func TestAcquire_FailsClosed(t *testing.T) {
	stubLookup(t, []net.IP{net.ParseIP("192.168.1.1")}, nil)
	if _, err := Acquire(context.Background(), "sync.cdn.example"); err == nil {
		t.Fatal("Acquire() = nil error for a host with only a private address")
	}

	stubLookup(t, nil, errors.New("lookup blocked"))
	if _, err := Acquire(context.Background(), "sync.cdn.example"); err == nil {
		t.Fatal("Acquire() = nil error when the lookup failed")
	}

	orig := physical
	t.Cleanup(func() { physical = orig })
	physical = func() (string, string, error) { return "", "", errors.New("no default route") }
	// Linux pins by mark, which needs no interface; elsewhere the bind needs one.
	if _, err := Acquire(context.Background(), "104.21.24.200"); err == nil && runtime.GOOS != "linux" {
		t.Fatal("Acquire() = nil error with no physical interface")
	}
}

func TestPin_ReleaseIsIdempotent(t *testing.T) {
	released := 0
	p := &Pin{release: func() { released++ }}
	p.Release()
	p.Release()
	var nilPin *Pin
	nilPin.Release()
	if released != 1 {
		t.Fatalf("release ran %d times, want once", released)
	}
}
