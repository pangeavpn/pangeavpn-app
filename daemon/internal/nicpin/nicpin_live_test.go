//go:build windows || (darwin && !ios)

package nicpin

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

const liveTarget = "1.1.1.1"

func dialVia(pin *Pin) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	if pin != nil {
		d.Control = pin.Control()
	}
	conn, err := d.Dial("tcp4", liveTarget+":443")
	if err == nil {
		conn.Close()
	}
	return err
}

// A real pin must reach the target through the physical NIC.
func TestLivePinReachesTheTarget(t *testing.T) {
	if os.Getenv("PANGEA_LIVE_PIN") != "1" {
		t.Skip("set PANGEA_LIVE_PIN=1 on a disposable machine")
	}
	if err := dialVia(nil); err != nil {
		t.Skipf("%s is not reachable from this machine: %v", liveTarget, err)
	}
	pin, err := Acquire(context.Background(), liveTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Release()
	t.Logf("pinned to %q (index %d)", pin.Interface, pin.index)
	if err := dialVia(pin); err != nil {
		t.Fatalf("pinned dial failed: %v", err)
	}
}
