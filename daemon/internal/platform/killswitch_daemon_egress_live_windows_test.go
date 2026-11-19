//go:build windows && (amd64 || arm64)

package platform

import (
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const liveTarget = "1.1.1.1"

func liveDial(addr string) error {
	conn, err := net.DialTimeout("tcp4", addr, 5*time.Second)
	if err == nil {
		conn.Close()
	}
	return err
}

func curlReaches(target string) bool {
	return exec.Command("curl.exe", "-s", "-o", "NUL", "--connect-timeout", "5", "https://"+target).Run() == nil
}

// The real BFE must let the daemon image (this test binary) through a daemon-only permit
// and keep every other process (curl) blocked from the same shared address.
func TestLiveWFPDaemonEgressAdmitsOnlyTheDaemonImage(t *testing.T) {
	if os.Getenv("PANGEA_LIVE_LOCK") != "1" {
		t.Skip("set PANGEA_LIVE_LOCK=1 on a disposable admin machine")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("needs admin")
	}
	if err := liveDial(liveTarget + ":443"); err != nil || !curlReaches(liveTarget) {
		t.Skipf("%s is not reachable from this machine without filters (%v)", liveTarget, err)
	}

	engine, err := wfpOpen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = engine.deleteFiltersInSublayer(pangeaVPNSublayerKey, anyFilter)
		_ = engine.deleteSublayerByKey(pangeaVPNSublayerKey)
		_ = engine.close()
	})
	if err := engine.addSublayer(); err != nil {
		t.Fatal(err)
	}
	addrMask, err := parseV4CIDRAddrMask(liveTarget + "/32")
	if err != nil {
		t.Fatal(err)
	}
	block := []wtFwpmFilterCondition0{{
		fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
		matchType: cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_V4_ADDR_MASK,
			value: uintptr(unsafe.Pointer(&addrMask)),
		},
	}}
	if _, err := engine.addFilter(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "PangeaVPN Live Test Block "+liveTarget, weightBlockAll, cFWP_ACTION_BLOCK, block); err != nil {
		t.Fatal(err)
	}
	if err := liveDial(liveTarget + ":443"); err == nil {
		t.Fatal("the test block did not block; the rest of the test would prove nothing")
	}

	appID, free, err := daemonAppID()
	if err != nil {
		t.Fatal(err)
	}
	ids, err := engine.addPermitDaemonEgress(appID, liveTarget)
	free()
	if err != nil {
		t.Fatal(err)
	}
	if err := liveDial(liveTarget + ":443"); err != nil {
		t.Fatalf("the daemon image was refused through its own permit: %v", err)
	}
	if curlReaches(liveTarget) {
		t.Fatal("another process reached the shared address through the daemon-only permit")
	}

	for _, id := range ids {
		if err := engine.deleteFilter(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := liveDial(liveTarget + ":443"); err == nil {
		t.Fatal("still reached after the permit was retired")
	}
}
