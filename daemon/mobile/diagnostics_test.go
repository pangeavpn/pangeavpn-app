package mobile

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestFormatLogEntriesMatchesTheDesktopRing(t *testing.T) {
	entries := []state.LogEntry{
		{TS: time.Date(2026, 9, 13, 9, 59, 0, 0, time.UTC).UnixMilli(), Level: state.LogWarn, Source: state.SourceDaemon, Msg: "reality: tunnel came up but did not carry traffic"},
		{TS: time.Date(2026, 9, 13, 10, 0, 1, 123e6, time.UTC).UnixMilli(), Level: state.LogInfo, Source: state.SourceWireGuard, Msg: "handshake ok"},
	}
	want := "2026-09-13T09:59:00.000Z [warn] [daemon] reality: tunnel came up but did not carry traffic\n" +
		"2026-09-13T10:00:01.123Z [info] [wireguard] handshake ok"
	if got := formatLogEntries(entries); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := formatLogEntries(nil); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestTailTextCutsOnARuneBoundary(t *testing.T) {
	cases := []struct {
		name, text string
		max        int
		want       string
	}{
		{"under the cap", "short", 64, "short"},
		{"mid rune", strings.Repeat("é", 10), 5, "[...truncated, showing last 4 bytes of 20]\néé"},
		{"partial line dropped", "line one\nline two\nline three", 12, "[...truncated, showing last 10 bytes of 28]\nline three"},
		{"cut on a line start", "line one\nline two\n", 9, "[...truncated, showing last 9 bytes of 18]\nline two\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tailText(c.text, c.max)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("got invalid UTF-8 %q", got)
			}
		})
	}
}

func sectionOf(name string, size int) diagnosticsSection {
	return newDiagnosticsSection(name, strings.Repeat("x", size))
}

func TestCapSectionsTruncatesAndOmits(t *testing.T) {
	all := capSections([]diagnosticsSection{sectionOf("a", 100), sectionOf("b", 300), sectionOf("c", 50)}, 700)
	for i, want := range []int{100, 300, 50} {
		if all[i].Bytes != want {
			t.Fatalf("section %d: got %d bytes, want %d untouched", i, all[i].Bytes, want)
		}
	}

	omitted := capSections([]diagnosticsSection{sectionOf("a", 100), sectionOf("b", 300), sectionOf("c", 50)}, 350)
	if omitted[0].Bytes != 100 {
		t.Fatalf("got %d bytes, want a kept whole", omitted[0].Bytes)
	}
	for i, name := range []string{"b", "c"} {
		want := "<" + name + " omitted: payload size limit reached>"
		if got := omitted[i+1]; got.Text != want || got.Bytes != len(want) || got.Name != name {
			t.Fatalf("got %+v, want %q", got, want)
		}
	}

	cut := capSections([]diagnosticsSection{sectionOf("a", 100), newDiagnosticsSection("b", strings.Repeat("é", 300)), sectionOf("c", 50)}, 500)
	if want := "[...truncated to fit the report]\n" + strings.Repeat("é", 200); cut[1].Text != want {
		t.Fatalf("got %q, want the last 400 bytes behind the marker", cut[1].Text)
	}
	if cut[1].Bytes != len(cut[1].Text) {
		t.Fatalf("got bytes %d for a %d-byte text", cut[1].Bytes, len(cut[1].Text))
	}
	if !strings.Contains(cut[2].Text, "omitted") {
		t.Fatalf("got %q, want c omitted", cut[2].Text)
	}

	odd := capSections([]diagnosticsSection{newDiagnosticsSection("b", strings.Repeat("é", 300))}, 301)
	if !utf8.ValidString(odd[0].Text) || !strings.HasSuffix(odd[0].Text, "\n"+strings.Repeat("é", 150)) {
		t.Fatalf("got %q, want a cut on a rune boundary", odd[0].Text)
	}
}

func TestNormalizeNoteTrimsCapsAndRedacts(t *testing.T) {
	cases := []struct{ in, want string }{
		{"   ", ""},
		{"", ""},
		{" cannot sign in ", "cannot sign in"},
		{"mail me at dana@example.com", "mail me at [redacted:email]"},
	}
	for _, c := range cases {
		if got := normalizeNote(c.in, ""); got != c.want {
			t.Errorf("normalizeNote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := normalizeNote(strings.Repeat("z", 900), ""); len(got) != 500 {
		t.Fatalf("got %d chars, want 500", len(got))
	}
	if got := normalizeNote(strings.Repeat("é", 900), ""); utf8.RuneCountInString(got) != 500 || !utf8.ValidString(got) {
		t.Fatalf("got %d runes, want 500 whole ones", utf8.RuneCountInString(got))
	}
}

func TestNormalizeNoteRedactsBeforeItCuts(t *testing.T) {
	const account = "K7M2-Q9X4-C8V1-B6N3-R5T0-W2Y7"
	for pad := 470; pad <= 500; pad++ {
		note := strings.Repeat("a", pad-1) + " " + account + " thanks"
		got := normalizeNote(note, "")
		if n := utf8.RuneCountInString(got); n > diagMaxNoteChars {
			t.Fatalf("pad %d: got %d runes, want at most %d", pad, n, diagMaxNoteChars)
		}
		for _, piece := range []string{"K7M2", "Q9X4", "W2Y7", "K7M", "K7"} {
			if strings.Contains(got, piece) {
				t.Fatalf("pad %d: the cut note leaked %q: %q", pad, piece, got)
			}
		}
		if open := strings.LastIndex(got, "["); open >= 0 && !strings.Contains(got[open:], "]") {
			t.Fatalf("pad %d: the note ends in a partial marker: %q", pad, got[open:])
		}
	}
	whole := normalizeNote("my number "+account, "")
	if whole != "my number [redacted:accountNumber]" {
		t.Fatalf("got %q, want the whole marker kept", whole)
	}
	if got := normalizeNote("see [log] and [redact", ""); got != "see [log] and [redact" {
		t.Fatalf("got %q, want a short note's own brackets kept", got)
	}
}

func TestRingSectionRedactsAndCaps(t *testing.T) {
	if got := ringSection("tunnel-ring", " \n ", ""); got.Text != "<tunnel-ring empty>" || got.Bytes != len(got.Text) {
		t.Fatalf("got %+v, want an empty marker", got)
	}
	got := ringSection("tunnel-ring", "dial 203.0.113.9 failed", "")
	if got.Text != "dial [redacted:ipv4] failed" || got.Bytes != len(got.Text) {
		t.Fatalf("got %+v, want the address redacted", got)
	}
	long := ringSection("core-ring", strings.Repeat("a line of log\n", 10000), "")
	if long.Bytes > diagSectionMaxBytes+100 || !strings.HasPrefix(long.Text, "[...truncated, showing last ") {
		t.Fatalf("got %d bytes, want a tail of at most %d", long.Bytes, diagSectionMaxBytes)
	}
	if bad := ringSection("x", "ok \xff end", ""); !utf8.ValidString(bad.Text) {
		t.Fatalf("got invalid UTF-8 %q", bad.Text)
	}
}

func TestParseReportCode(t *testing.T) {
	code, err := parseReportCode(http.StatusCreated, []byte(`{"reportCode":" K7M2-9QXD "}`))
	if err != nil || code != "K7M2-9QXD" {
		t.Fatalf("got %q, %v, want K7M2-9QXD", code, err)
	}
	for _, body := range []string{`{"reportCode":"bad code"}`, `{}`, `{"reportCode":7}`, `{"reportCode":"  "}`, `{"reportCode":"not a code!"}`, ``, `null`} {
		if _, err := parseReportCode(http.StatusOK, []byte(body)); err == nil || err.Error() != "DIAGNOSTICS_REJECTED: no usable report code" {
			t.Errorf("body %q: got %v, want no usable report code", body, err)
		}
	}
	rejected := []struct {
		status int
		body   string
		want   string
	}{
		{429, `{"error":"Too many requests","code":"RATE_LIMITED","requestId":"r1"}`, "DIAGNOSTICS_REJECTED: hub answered 429 (RATE_LIMITED)"},
		{413, `{"error":"too large","code":"PAYLOAD_TOO_LARGE"}`, "DIAGNOSTICS_REJECTED: hub answered 413 (PAYLOAD_TOO_LARGE)"},
		{400, `{"code":"INVALID_REPORT"}`, "DIAGNOSTICS_REJECTED: hub answered 400 (INVALID_REPORT)"},
		{500, `<html>oops</html>`, "DIAGNOSTICS_REJECTED: hub answered 500"},
		{502, `{"code":"has spaces <b>"}`, "DIAGNOSTICS_REJECTED: hub answered 502"},
	}
	for _, c := range rejected {
		if _, err := parseReportCode(c.status, []byte(c.body)); err == nil || err.Error() != c.want {
			t.Errorf("status %d: got %v, want %q", c.status, err, c.want)
		}
	}
}

func testMeta() diagnosticsMeta {
	return diagnosticsMeta{
		AppVersion: "0.8.2",
		Platform:   "ios",
		Arch:       "arm64",
		OSRelease:  "24.5.0",
		CreatedAt:  time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC),
	}
}

func TestDiagnosticsBodyCarriesTheHubContract(t *testing.T) {
	body, err := buildDiagnosticsBody(testMeta(), "  ", []diagnosticsSection{ringSection("core-ring", "hello", "")}, "")
	if err != nil {
		t.Fatalf("buildDiagnosticsBody: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{
		"appVersion": "0.8.2", "platform": "ios", "arch": "arm64", "osRelease": "24.5.0",
		"createdAt": "2026-09-13T10:00:00.000Z",
		"sections":  []any{map[string]any{"name": "core-ring", "bytes": float64(5), "text": "hello"}},
	}
	if b, _ := json.Marshal(decoded); string(b) != mustJSON(t, want) {
		t.Fatalf("got %s, want %s", b, mustJSON(t, want))
	}

	noted, err := buildDiagnosticsBody(testMeta(), "stuck at dana@example.com", nil, "")
	if err != nil || !strings.Contains(string(noted), `"note":"stuck at [redacted:email]"`) {
		t.Fatalf("got %s, %v, want the redacted note", noted, err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestDiagnosticsMetaFallsBackAndCaps(t *testing.T) {
	meta := diagnosticsMeta{AppVersion: " ", OSRelease: strings.Repeat("9", 300), CreatedAt: time.Unix(0, 0)}
	body, err := buildDiagnosticsBody(meta, "", nil, "")
	if err != nil {
		t.Fatalf("buildDiagnosticsBody: %v", err)
	}
	var decoded diagnosticsPayload
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.AppVersion != "unknown" || decoded.Platform != "unknown" || len(decoded.OSRelease) != 64 {
		t.Fatalf("got %+v, want unknown fallbacks and a 64-char release", decoded)
	}
	if decoded.Sections == nil {
		t.Fatalf("got null sections, want an array: the hub rejects anything else")
	}
}

// The inner request is HTML-escaped, sealed and base64'd under the hub's 1 MiB body limit.
func TestDiagnosticsPayloadFitsTheHubLimit(t *testing.T) {
	heavy := strings.Repeat("<>&\x01", 512<<10)
	sections := []diagnosticsSection{
		ringSection("core-ring", heavy, ""),
		ringSection("tunnel-ring", heavy, ""),
		ringSection("status", heavy, ""),
	}
	body, err := buildDiagnosticsBody(testMeta(), strings.Repeat("<", 900), sections, "")
	if err != nil {
		t.Fatalf("buildDiagnosticsBody: %v", err)
	}
	if len(body) > diagMaxInnerBytes {
		t.Fatalf("got %d bytes, want at most %d", len(body), diagMaxInnerBytes)
	}
	inner, err := innerRequestJSON(http.MethodPost, diagnosticsRoute, map[string]string{"Content-Type": "application/json"}, body)
	if err != nil {
		t.Fatalf("innerRequestJSON: %v", err)
	}
	if sealed := (len(inner)+2)/3*4 + 8<<10; sealed > 1<<20 {
		t.Fatalf("got a %d-byte envelope, want it under the 1 MiB body limit", sealed)
	}
	var decoded diagnosticsPayload
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, s := range decoded.Sections {
		if s.Bytes != len(s.Text) {
			t.Fatalf("section %s: got bytes %d for %d bytes of text", s.Name, s.Bytes, len(s.Text))
		}
	}
}
