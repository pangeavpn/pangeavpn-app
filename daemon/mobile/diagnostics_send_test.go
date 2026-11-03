//go:build android || ios

package mobile

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func answerReport(code string) func(fakeRequest) (int, any) {
	return func(fakeRequest) (int, any) {
		return http.StatusCreated, map[string]string{"reportCode": code}
	}
}

func testLogs(t *testing.T) *state.LogStore {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if wgLogs == nil {
		t.Fatal("no log ring: Init has not run")
	}
	return wgLogs
}

func sentReport(t *testing.T, h *fakeHub) diagnosticsPayload {
	t.Helper()
	sent := h.requestsTo(diagnosticsRoute)
	if len(sent) != 1 {
		t.Fatalf("got %d diagnostics requests, want 1", len(sent))
	}
	var payload diagnosticsPayload
	if err := json.Unmarshal(sent[0].Body, &payload); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	return payload
}

func reportSection(t *testing.T, payload diagnosticsPayload, name string) string {
	t.Helper()
	for _, section := range payload.Sections {
		if section.Name == name {
			return section.Text
		}
	}
	t.Fatalf("missing section %s in %+v", name, payload.Sections)
	return ""
}

func TestSendDiagnosticsIsAnonymous(t *testing.T) {
	ident := mustIdentity(t)
	const license = "LK-7f3a-not-for-the-hub"
	initForTest(t, map[string]string{keyLicense: license, keyIdentity: ident.marshal()})
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, answerReport("K7M2-9QXD"))

	code, err := SendDiagnostics("cannot connect", "")
	if err != nil || code != "K7M2-9QXD" {
		t.Fatalf("got %q, %v, want K7M2-9QXD", code, err)
	}
	sent := h.requestsTo(diagnosticsRoute)
	if len(sent) != 1 || sent[0].Method != http.MethodPost {
		t.Fatalf("got %+v, want one POST", sent)
	}
	if len(sent[0].Headers) != 1 || sent[0].Headers["Content-Type"] != "application/json" {
		t.Fatalf("got headers %v, want only Content-Type", sent[0].Headers)
	}
	for _, secret := range []string{license, ident.pubB64} {
		if strings.Contains(string(sent[0].Body), secret) {
			t.Fatalf("report carries %q", secret)
		}
	}
}

func TestSendDiagnosticsWorksSignedOut(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, answerReport("AB12-CD34"))
	if code, err := SendDiagnostics("", ""); err != nil || code != "AB12-CD34" {
		t.Fatalf("got %q, %v, want AB12-CD34", code, err)
	}
	if payload := sentReport(t, h); payload.Note != "" {
		t.Fatalf("got note %q, want it omitted", payload.Note)
	}
}

func TestSendDiagnosticsRedactsTheTunnelLogs(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, answerReport("K7M2-9QXD"))
	testLogs(t).Add(state.LogInfo, state.SourceDaemon, "reached the hub over directIp (198.51.100.7)")

	if _, err := SendDiagnostics("mail dana@example.com", "dial 203.0.113.9 failed"); err != nil {
		t.Fatalf("SendDiagnostics: %v", err)
	}
	payload := sentReport(t, h)
	if tunnel := reportSection(t, payload, "tunnel-ring"); tunnel != "dial [redacted:ipv4] failed" {
		t.Fatalf("got tunnel ring %q, want the address redacted", tunnel)
	}
	core := reportSection(t, payload, "core-ring")
	if !strings.Contains(core, "[info] [daemon] reached the hub over directIp ([redacted:ipv4])") {
		t.Fatalf("got core ring %q, want the redacted line", core)
	}
	if payload.Note != "mail [redacted:email]" {
		t.Fatalf("got note %q, want it redacted", payload.Note)
	}
	if status := reportSection(t, payload, "status"); !strings.Contains(status, `"hubMethods"`) || !strings.Contains(status, `"state"`) {
		t.Fatalf("got status %q, want the state and config", status)
	}
}

func TestSendDiagnosticsCarriesTheClientInfo(t *testing.T) {
	initForTest(t, nil)
	t.Cleanup(func() { SetClientInfo("", "") })
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, answerReport("K7M2-9QXD"))

	SetClientInfo(" 0.8.2 ", "17.5")
	before := time.Now().Add(-time.Second)
	if _, err := SendDiagnostics("", ""); err != nil {
		t.Fatalf("SendDiagnostics: %v", err)
	}
	payload := sentReport(t, h)
	if payload.AppVersion != "0.8.2" || payload.OSRelease != "17.5" || payload.Platform != runtime.GOOS || payload.Arch != runtime.GOARCH {
		t.Fatalf("got %+v, want the client info and this runtime", payload)
	}
	created, err := time.Parse("2006-01-02T15:04:05.000Z", payload.CreatedAt)
	if err != nil || created.Before(before) {
		t.Fatalf("got createdAt %q, %v, want now in UTC", payload.CreatedAt, err)
	}
}

func TestSendDiagnosticsDefaultsTheClientInfo(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, answerReport("K7M2-9QXD"))
	SetClientInfo("", "")
	if _, err := SendDiagnostics("", ""); err != nil {
		t.Fatalf("SendDiagnostics: %v", err)
	}
	payload := sentReport(t, h)
	if payload.AppVersion != "unknown" || payload.OSRelease == "" || payload.OSRelease == "unknown" {
		t.Fatalf("got %+v, want unknown version and the kernel release", payload)
	}
}

func TestSendDiagnosticsReportsUnreachable(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, answerReport("K7M2-9QXD"))
	h.offline()
	_, err := SendDiagnostics("", "")
	if err == nil || !strings.HasPrefix(err.Error(), "DIAGNOSTICS_UNREACHABLE: hub unreachable: retrying in") {
		t.Fatalf("got %v, want DIAGNOSTICS_UNREACHABLE", err)
	}
	if sent := h.requestsTo(diagnosticsRoute); len(sent) != 0 {
		t.Fatalf("got %d requests, want none", len(sent))
	}
}

func TestSendDiagnosticsReportsTheHubsRefusal(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, func(fakeRequest) (int, any) {
		return http.StatusTooManyRequests, map[string]string{"error": "Too many requests", "code": "RATE_LIMITED"}
	})
	_, err := SendDiagnostics("", "")
	if err == nil || err.Error() != "DIAGNOSTICS_REJECTED: hub answered 429 (RATE_LIMITED)" {
		t.Fatalf("got %v, want the hub's refusal", err)
	}
}

func TestLogsAreRedacted(t *testing.T) {
	initForTest(t, nil)
	logs := testLogs(t)
	logs.Add(state.LogInfo, state.SourceDaemon, "reached the hub over directIp (203.0.113.9)")
	logs.Add(state.LogWarn, state.SourceWireGuard, "peer key cGFuZ2VhdGVzdGtleXBhbmdlYXRlc3RrZXkxMjM0NTY3OD0= rejected")

	got := Logs()
	if strings.Contains(got, "203.0.113.9") || strings.Contains(got, "cGFuZ2VhdGVzdGtleXBh") {
		t.Fatalf("got %q, want it redacted", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], " [info] [daemon] reached the hub over directIp ([redacted:ipv4])") ||
		!strings.HasSuffix(lines[1], " [warn] [wireguard] peer key [redacted:key] rejected") {
		t.Fatalf("got %q, want both lines in order", got)
	}
}

func TestLogsKeepTheNewestWithinTheCap(t *testing.T) {
	initForTest(t, nil)
	logs := testLogs(t)
	for i := 0; i < 500; i++ {
		logs.Add(state.LogInfo, state.SourceDaemon, strings.Repeat("x", 300))
	}
	logs.Add(state.LogInfo, state.SourceDaemon, "the newest line")
	got := Logs()
	if len(got) > diagSectionMaxBytes+100 || !strings.HasSuffix(got, "the newest line") {
		t.Fatalf("got %d bytes ending %q, want at most %d ending with the newest", len(got), got[len(got)-20:], diagSectionMaxBytes)
	}
}

func TestLogsBeforeInitAreEmpty(t *testing.T) {
	resetGlobals()
	if got := Logs(); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
