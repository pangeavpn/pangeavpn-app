//go:build android || ios

package mobile

// Logs and SendDiagnostics: this runtime's log ring and the anonymous report.
// Mirrors apps/desktop/src/main/diagnosticsReport.ts and diagnosticsUpload.ts.

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	clientInfoMu     sync.Mutex
	clientAppVersion string
	clientOSVersion  string
)

// SetClientInfo records what Go cannot learn itself; blanks fall back to
// "unknown" and the kernel release.
func SetClientInfo(appVersion, osVersion string) {
	clientInfoMu.Lock()
	defer clientInfoMu.Unlock()
	clientAppVersion = strings.TrimSpace(appVersion)
	clientOSVersion = strings.TrimSpace(osVersion)
}

// Logs is this runtime's recent log ring, redacted, newest last, its raw
// text capped at 64 KiB.
func Logs() string {
	text := coreRingText()
	if text == "" {
		return ""
	}
	return redactDiagnostics(tailText(text, diagSectionMaxBytes), currentHome())
}

// SendDiagnostics uploads an anonymous, redacted report and returns its code.
// It blocks on the network for up to a hub search plus one request.
func SendDiagnostics(note, extraLogs string) (string, error) {
	home := currentHome()
	sections := []diagnosticsSection{
		ringSection("core-ring", coreRingText(), home),
		ringSection("tunnel-ring", extraLogs, home),
		ringSection("status", statusText(), home),
	}
	body, err := buildDiagnosticsBody(clientMeta(time.Now()), note, sections, home)
	if err != nil {
		return "", err
	}
	status, answer, err := anonymousHubRequest(http.MethodPost, diagnosticsRoute, body)
	if err != nil {
		return "", fmt.Errorf("DIAGNOSTICS_UNREACHABLE: %w", err)
	}
	return parseReportCode(status, answer)
}

// anonymousHubRequest rides the same sealed route as hubRequest but never
// sends the license key, so the report cannot be tied to an account.
func anonymousHubRequest(method, route string, body []byte) (int, []byte, error) {
	answer, status, err := hubFetch(route, method, map[string]string{"Content-Type": "application/json"}, body)
	return status, answer, err
}

func coreRingText() string {
	mu.Lock()
	logs := wgLogs
	mu.Unlock()
	if logs == nil {
		return ""
	}
	return formatLogEntries(logs.Since(0))
}

func statusText() string {
	config, err := GetConfig()
	if err != nil {
		config = fmt.Sprintf("<config unavailable: %v>", err)
	}
	return "state: " + State() + "\nconfig: " + config
}

func clientMeta(now time.Time) diagnosticsMeta {
	clientInfoMu.Lock()
	appVersion, osVersion := clientAppVersion, clientOSVersion
	clientInfoMu.Unlock()
	if osVersion == "" {
		osVersion = kernelRelease()
	}
	return diagnosticsMeta{
		AppVersion: appVersion,
		Platform:   runtime.GOOS,
		Arch:       runtime.GOARCH,
		OSRelease:  osVersion,
		CreatedAt:  now,
	}
}

// kernelRelease is what desktop sends as os.release(): Darwin's on iOS.
func kernelRelease() string {
	var name unix.Utsname
	if err := unix.Uname(&name); err != nil {
		return ""
	}
	return unix.ByteSliceToString(name.Release[:])
}
