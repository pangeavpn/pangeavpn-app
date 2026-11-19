//go:build darwin && !ios

package nicpin

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

// fakeBroker reports the NIC it was built on from Current until Refresh reads the new one.
type fakeBroker struct{ built, now string }

func (f *fakeBroker) DialTCP(context.Context, netip.AddrPort) (net.Conn, error) {
	return nil, errors.New("fake")
}
func (f *fakeBroker) ListenUDP(context.Context) (net.PacketConn, error) {
	return nil, errors.New("fake")
}
func (f *fakeBroker) Repin(syscall.Conn) error          { return nil }
func (f *fakeBroker) Current() (egress.Identity, error) { return egress.Identity{Name: f.built}, nil }
func (f *fakeBroker) Close() error                      { return nil }
func (f *fakeBroker) Refresh() (egress.Identity, egress.Identity, bool, error) {
	old := egress.Identity{Name: f.built}
	f.built = f.now
	return old, egress.Identity{Name: f.now}, old.Name != f.now, nil
}

func stubDarwin(t *testing.T, fb *fakeBroker, physicalName string) {
	t.Helper()
	origBroker, origShared, origPhysical, origRoute := newBroker, broker, physical, scoped.route
	t.Cleanup(func() { newBroker, broker, physical, scoped.route = origBroker, origShared, origPhysical, origRoute })
	broker = nil
	newBroker = func() (egress.Dialer, error) { return fb, nil }
	physical = func() (string, string, error) { return physicalName, "192.0.2.1", nil }
	scoped.route = func(...string) error { return nil }
}

// The broker resolves its NIC once; a Mac that moved since must still pin, on the new NIC.
func TestAcquire_DarwinFollowsANICChange(t *testing.T) {
	ifc, err := net.InterfaceByName("en0")
	if err != nil || ifc.Flags&net.FlagUp == 0 {
		t.Skip("needs an up en0")
	}
	stubDarwin(t, &fakeBroker{built: "en5", now: "en0"}, "en0")
	pin, err := Acquire(context.Background(), "104.21.24.200")
	if err != nil {
		t.Fatalf("Acquire() after a NIC change = %v, want the pin on en0", err)
	}
	defer pin.Release()
	if pin.Interface != "en0" || pin.Dial == nil {
		t.Fatalf("pin = %+v, want en0 with the broker's dial", pin)
	}

	stubDarwin(t, &fakeBroker{built: "en0", now: "en5"}, "en0")
	if _, err := Acquire(context.Background(), "104.21.24.200"); err == nil {
		t.Fatal("Acquire() pinned while the broker's NIC is not the default route's")
	}
}
