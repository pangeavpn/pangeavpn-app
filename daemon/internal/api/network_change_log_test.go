package api

import (
	"strings"
	"testing"
)

// The network-change log is the diagnostic that tells us how fast a host's
// default route flaps (e.g. a Zscaler client shifting the local address): it must
// record a real move, stay quiet on a repeat, and name the new address.
func TestOnNetworkChanged_LogsRouteMoveOncePerChange(t *testing.T) {
	svc, _ := newSysEventsTestService(t)
	addr := "10.0.0.5"
	svc.physicalRoute = func() (string, string, error) { return "Wi-Fi", addr, nil }

	routeLogs := func() []string {
		var msgs []string
		for _, e := range svc.logs.Since(0) {
			if strings.Contains(e.Msg, "network changed: default route now via") {
				msgs = append(msgs, e.Msg)
			}
		}
		return msgs
	}

	svc.onNetworkChanged()
	if got := len(routeLogs()); got != 1 {
		t.Fatalf("after first change: route logs = %d, want 1", got)
	}

	// Same route again must not re-log, so the ring measures real flaps only.
	svc.onNetworkChanged()
	if got := len(routeLogs()); got != 1 {
		t.Fatalf("after an unchanged event: route logs = %d, want 1", got)
	}

	// A real move logs again and names the new address.
	addr = "10.0.0.9"
	svc.onNetworkChanged()
	logs := routeLogs()
	if len(logs) != 2 {
		t.Fatalf("after a route move: route logs = %d, want 2", len(logs))
	}
	if !strings.Contains(logs[len(logs)-1], "10.0.0.9") {
		t.Fatalf("latest route log = %q, want it to name 10.0.0.9", logs[len(logs)-1])
	}
}

// When the link drops, the change is logged once as a link-down, not as a route.
func TestOnNetworkChanged_LogsLinkDownOnce(t *testing.T) {
	svc, _ := newSysEventsTestService(t)
	// No usable network: force the early-return branch.
	svc.networkKey = func() string { return "" }

	downLogs := func() int {
		n := 0
		for _, e := range svc.logs.Since(0) {
			if strings.Contains(e.Msg, "no usable default route") {
				n++
			}
		}
		return n
	}

	svc.onNetworkChanged()
	svc.onNetworkChanged()
	if got := downLogs(); got != 1 {
		t.Fatalf("link-down logs = %d, want 1 (deduped)", got)
	}
}
