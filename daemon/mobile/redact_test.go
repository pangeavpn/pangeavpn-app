package mobile

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// Ported table for table from apps/desktop/src/main/diagnosticsRedact.test.ts.

type redactCase struct {
	what string
	line string
	gone []string
	kept []string
}

const (
	redactAccountDisplay    = "K7M2-9QXD-4RTB-8WZC-3NVF-6HJP"
	redactAccountNormalized = "K7M29QXD4RTB8WZC3NVF6HJP"
	redactWGKey             = "cGFuZ2VhdGVzdGtleXBhbmdlYXRlc3RrZXkxMjM0NTY3OD0="
)

var (
	redactMLKEM = strings.Repeat("A", 40) + "bQ9" + strings.Repeat("z", 60) + "8Kx" + "="
	redactHex64 = "9f2c" + strings.Repeat("a", 56) + "1b7e"
	anyRedacted = regexp.MustCompile(`\[redacted:[a-zA-Z0-9]+\]`)
)

var redactCases = []redactCase{
	{
		what: "account number in display form",
		line: "token login rejected for " + redactAccountDisplay + " (401)",
		gone: []string{redactAccountDisplay, "K7M2-9QXD"},
		kept: []string{"token login rejected for", "(401)"},
	},
	{
		what: "account number in normalised form",
		line: "rememberAccountNumber wrote " + redactAccountNormalized + " to remembered-account.dat",
		gone: []string{redactAccountNormalized},
		kept: []string{"rememberAccountNumber wrote", "remembered-account.dat"},
	},
	{
		what: "legacy 16-digit token",
		line: "auth:login called with legacy token 4929100288371625",
		gone: []string{"4929100288371625"},
		kept: []string{"auth:login called with legacy token"},
	},
	{
		what: "bearer token",
		line: "Token login failed (401): Authorization: Bearer eyJhbGci.eyJzdWIiOjF9.QmFk-Sig_99",
		gone: []string{"eyJhbGci", "QmFk-Sig_99"},
		kept: []string{"Token login failed (401)", "Authorization: Bearer"},
	},
	{
		what: "license key header value",
		line: "hub rejected header X-License-Key: LIC-9f2ca7be-prod and returned 403",
		gone: []string{"LIC-9f2ca7be-prod"},
		kept: []string{"hub rejected header X-License-Key:", "returned 403"},
	},
	{
		what: "license key in a JSON body",
		line: `{"licenseKey":"LIC-9f2ca7be-prod","region":"fra"}`,
		gone: []string{"LIC-9f2ca7be-prod"},
		kept: []string{`"region":"fra"`},
	},
	{
		what: "WireGuard base64 key",
		line: "[wg] peer add failed for key " + redactWGKey + " on utun6",
		gone: []string{redactWGKey},
		kept: []string{"[wg] peer add failed for key", "on utun6"},
	},
	{
		what: "WireGuard config assignment",
		line: "PrivateKey = qKp0RmVyeVNlY3JldEtleVZhbHVlSGVyZTEyMzQ1Ng=",
		gone: []string{"qKp0RmVyeVNlY3JldEtleVZhbHVl"},
		kept: []string{"PrivateKey ="},
	},
	{
		what: "longer ML-KEM post-quantum material",
		line: "[pq] offer parse failed for " + redactMLKEM + " (truncated)",
		gone: []string{redactMLKEM},
		kept: []string{"[pq] offer parse failed for", "(truncated)"},
	},
	{
		what: "64-hex daemon token",
		line: "daemon token " + redactHex64 + " rejected by /v1/status",
		gone: []string{redactHex64},
		kept: []string{"daemon token", "rejected by /v1/status"},
	},
	{
		what: "UUID",
		line: "device 8f14e45f-ceea-467a-9c3f-1b2d4e5a6c7d renamed to Work Laptop",
		gone: []string{"8f14e45f-ceea-467a-9c3f-1b2d4e5a6c7d"},
		kept: []string{"device", "renamed to Work Laptop"},
	},
	{
		what: "email address",
		line: "auth0 profile loaded for dana.reyes+vpn@example.co.uk (sub google-oauth2)",
		gone: []string{"dana.reyes+vpn@example.co.uk", "example.co.uk"},
		kept: []string{"auth0 profile loaded for", "(sub google-oauth2)"},
	},
	{
		what: "public IPv4",
		line: "[DoH] https://1.1.1.1/dns-query resolved api.pangeavpn.org to 203.0.113.44",
		gone: []string{"203.0.113.44", "1.1.1.1"},
		kept: []string{"[DoH] https://", "/dns-query resolved api.pangeavpn.org to"},
	},
	{
		what: "public IPv6",
		line: "handshake to [2606:4700:4700::1111]:51820 timed out",
		gone: []string{"2606:4700:4700::1111"},
		kept: []string{"handshake to [", "]:51820 timed out"},
	},
}

var redactHomeCases = []redactCase{
	{
		what: "Windows profile path with a space in the username",
		line: `describeApps: stat failed for C:\Users\Dana Reyes\AppData\Local\Discord\app-1.0.9\Discord.exe`,
		gone: []string{"Dana Reyes", "Dana"},
		kept: []string{`describeApps: stat failed for C:\Users\`, `\AppData\Local\Discord\app-1.0.9\Discord.exe`},
	},
	{
		what: "JSON-escaped profile path inside a daemon error",
		line: `daemon request failed (400): {"ok":false,"apps":["C:\\Users\\dana\\Games\\x.exe"]}`,
		gone: []string{"dana"},
		kept: []string{"daemon request failed (400)", `\\Games\\x.exe`},
	},
	{
		what: "forward-slash Windows profile path",
		line: "icon lookup C:/Users/dana/AppData/Roaming/app.exe",
		gone: []string{"dana"},
		kept: []string{"icon lookup C:/Users/", "/AppData/Roaming/app.exe"},
	},
	{
		what: "kernel device path to a profile",
		line: `owner \Device\HarddiskVolume3\Users\dana\scoop\apps\x.exe`,
		gone: []string{"dana"},
		kept: []string{`\scoop\apps\x.exe`},
	},
	{
		what: "legacy Documents and Settings profile",
		line: `C:\Documents and Settings\dana\Desktop\tool.exe`,
		gone: []string{"dana"},
		kept: []string{`\Desktop\tool.exe`},
	},
	{
		what: "macOS home",
		line: "bundle /Users/dana/Applications/Foo.app not found",
		gone: []string{"dana"},
		kept: []string{"bundle /Users/", "/Applications/Foo.app not found"},
	},
	{
		what: "Linux home",
		line: "desktop entry /home/dana/.local/share/applications/foo.desktop skipped",
		gone: []string{"dana"},
		kept: []string{"desktop entry /home/", "/.local/share/applications/foo.desktop skipped"},
	},
	{
		what: "root's home",
		line: "reading /root/.config/pangeavpn/settings.json",
		gone: []string{"/root/"},
		kept: []string{"/.config/pangeavpn/settings.json"},
	},
}

func checkRedactCase(t *testing.T, c redactCase, out string) {
	t.Helper()
	for _, secret := range c.gone {
		if strings.Contains(out, secret) {
			t.Errorf("leaked %q in: %s", secret, out)
		}
	}
	for _, context := range c.kept {
		if !strings.Contains(out, context) {
			t.Errorf("lost context %q in: %s", context, out)
		}
	}
}

func TestRedactRemovesEachSecretAndKeepsTheLine(t *testing.T) {
	for _, c := range redactCases {
		t.Run(c.what, func(t *testing.T) {
			out := redactDiagnostics(c.line, currentHome())
			checkRedactCase(t, c, out)
			if !anyRedacted.MatchString(out) {
				t.Fatalf("got %q, want a [redacted:...] marker", out)
			}
		})
	}
}

func TestRedactKeepsTunnelAddresses(t *testing.T) {
	line := "route 10.8.0.2/32 via 192.168.1.1, daemon listening on 127.0.0.1:7890, link-local 169.254.3.9"
	if got := redactDiagnostics(line, currentHome()); got != line {
		t.Fatalf("got %q, want %q", got, line)
	}
	probe := "hub health probe to 172.16.4.7 ok"
	if got := redactDiagnostics(probe, currentHome()); got != probe {
		t.Fatalf("got %q, want %q", got, probe)
	}
	for ip, want := range map[string]bool{"10.0.0.1": true, "172.31.255.254": true, "172.32.0.1": false, "8.8.8.8": false} {
		if got := isKeptIPv4(ip); got != want {
			t.Errorf("isKeptIPv4(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestRedactKeepsLocalIPv6(t *testing.T) {
	line := "bound ::1 and fe80::1%utun6 and fd00::5"
	if got := redactDiagnostics(line, currentHome()); got != line {
		t.Fatalf("got %q, want %q", got, line)
	}
	for ip, want := range map[string]bool{"::1": true, "fe80::abcd": true, "fd12:3456::1": true, "2001:db8::1": false} {
		if got := isKeptIPv6(ip); got != want {
			t.Errorf("isKeptIPv6(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestRedactLeavesTimestampsAndVersions(t *testing.T) {
	line := "2026-09-13T10:22:31.123Z [warn] PangeaVPN 0.7.1 daemon exited code=1 signal=null"
	if got := redactDiagnostics(line, currentHome()); got != line {
		t.Fatalf("got %q, want %q", got, line)
	}
}

func TestRedactLeavesFilePaths(t *testing.T) {
	for _, line := range []string{
		`could not open C:\ProgramData\PangeaVPN\daemon.log (EACCES)`,
		"reading /Library/Application Support/PangeaVPN/daemon-elevated.log",
	} {
		if got := redactDiagnostics(line, currentHome()); got != line {
			t.Errorf("got %q, want %q", got, line)
		}
	}
}

func TestRedactKeepsLineCount(t *testing.T) {
	block := strings.Join([]string{
		"2026-09-13T10:22:31.000Z [log] connecting to fra-01",
		"2026-09-13T10:22:31.400Z [log] peer key " + redactWGKey,
		"2026-09-13T10:22:33.900Z [log] handshake ok via 10.8.0.1",
	}, "\n")
	out := redactDiagnostics(block, currentHome())
	if got := len(strings.Split(out, "\n")); got != 3 {
		t.Fatalf("got %d lines, want 3", got)
	}
	if strings.Contains(out, redactWGKey) {
		t.Fatalf("leaked the key in: %s", out)
	}
	for _, want := range []string{"connecting to fra-01", "handshake ok via 10.8.0.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q in: %s", want, out)
		}
	}
}

func TestRedactLeavesProse(t *testing.T) {
	line := "Hub unreachable; retrying in 20s (method=normal, attempt 2)"
	if got := redactDiagnostics(line, currentHome()); got != line {
		t.Fatalf("got %q, want %q", got, line)
	}
}

func TestRedactRemovesHomeDirectories(t *testing.T) {
	for _, c := range redactHomeCases {
		t.Run(c.what, func(t *testing.T) {
			out := redactDiagnostics(c.line, "")
			checkRedactCase(t, c, out)
			if !strings.Contains(out, "[redacted:user]") {
				t.Fatalf("got %q, want [redacted:user]", out)
			}
		})
	}
}

func TestRedactRedirectedHomeInEverySpelling(t *testing.T) {
	home := `D:\Profiles\dana`
	cases := []struct{ in, want string }{
		{`open D:\Profiles\dana\AppData\x.exe failed`, `open [redacted:user]\AppData\x.exe failed`},
		{`{"path":"d:\\profiles\\DANA\\x.exe"}`, `{"path":"[redacted:user]\\x.exe"}`},
		{"D:/Profiles/dana/x.exe", "[redacted:user]/x.exe"},
	}
	for _, c := range cases {
		if got := redactDiagnostics(c.in, home); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestRedactHomeStopsAtAPathBoundary(t *testing.T) {
	got := redactDiagnostics("/srv/people/dan/x and /srv/people/dana/y", "/srv/people/dan")
	if want := "[redacted:user]/x and /srv/people/dana/y"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := redactDiagnostics("unchanged", `C:\`); got != "unchanged" {
		t.Fatalf("got %q, want %q", got, "unchanged")
	}
}

func TestRedactKeepsEveryLineWithProfilePaths(t *testing.T) {
	block := strings.Join([]string{
		"2026-10-02T10:00:00.000Z [log] catalog: 41 apps, 3 skipped",
		`2026-10-02T10:00:01.000Z [warn] C:\Users\dana\AppData\Local\x.exe`,
		"2026-10-02T10:00:02.000Z [log] /home/dana/.local/bin/y",
		"2026-10-02T10:00:03.000Z [log] done",
	}, "\n")
	out := redactDiagnostics(block, "")
	if got := len(strings.Split(out, "\n")); got != 4 {
		t.Fatalf("got %d lines, want 4", got)
	}
	if strings.Contains(out, "dana") {
		t.Fatalf("leaked the user name in: %s", out)
	}
	if !strings.Contains(out, "catalog: 41 apps, 3 skipped") || !strings.HasSuffix(out, "[log] done") {
		t.Fatalf("lost context in: %s", out)
	}
}

func TestRedactLeavesSystemPaths(t *testing.T) {
	for _, line := range []string{
		`C:\ProgramData\PangeaVPN\daemon.log`,
		`C:\Program Files\PangeaVPN\PangeaVPN.exe`,
		"/Library/Application Support/PangeaVPN/daemon-elevated.log",
		"/usr/share/applications/firefox.desktop",
		"/opt/google/chrome/",
	} {
		if got := redactDiagnostics(line, ""); got != line {
			t.Errorf("got %q, want %q", got, line)
		}
	}
}

func TestRedactIsIdempotent(t *testing.T) {
	var lines []string
	for _, c := range append(append([]redactCase{}, redactCases...), redactHomeCases...) {
		lines = append(lines, c.line)
	}
	lines = append(lines, `open D:\Profiles\dana\AppData\x.exe failed`, "X-License-Key: abc and Bearer xyz")
	for _, line := range lines {
		once := redactDiagnostics(line, `D:\Profiles\dana`)
		if twice := redactDiagnostics(once, `D:\Profiles\dana`); twice != once {
			t.Errorf("second pass changed %q into %q", once, twice)
		}
	}
}

// Expectations below come from running the desktop redactor on the same input.
func TestRedactBoundaryRetry(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.2.3.4.5", "1.2.3.4.5"},
		{"x203.0.113.4", "x[redacted:ipv4]"},
		{"1203.0.113.4", "1203.0.113.4"},
		{"203.0.113.4.5 and 198.51.100.7", "203.0.113.4.5 and [redacted:ipv4]"},
		{"203.0.113.4:443", "[redacted:ipv4]:443"},
		{"//root/x and x/root and /rooted and /root", "//[redacted:user]/x and x/root and /rooted and /[redacted:user]"},
		{"a.b/root", "a.b/root"},
		{"k=" + redactWGKey + " " + redactWGKey, "k=" + redactWGKey + " [redacted:key]"},
		{redactWGKey + "==", redactWGKey + "=="},
		{"x" + redactWGKey, "[redacted:key]"},
		{"n " + redactAccountNormalized + "Q", "n " + redactAccountNormalized + "Q"},
		{"t 49291002883716251", "t 49291002883716251"},
		{"t 4929100288371625.", "t [redacted:accountNumber]."},
		{"v6 2001:db8:0:0:0:0:0:1 and 2001:db8::1:x", "v6 [redacted:ipv6] and 2001:db8::1:x"},
		{"v6 1:2:3:4:5:6:7:8::9", "v6 1:2:3:4:5:6:7:8::9"},
	}
	for _, c := range cases {
		if got := redactDiagnostics(c.in, ""); got != c.want {
			t.Errorf("redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The hub folds case, spaces, dashes and I/L/O before matching a number, so
// every spelling it accepts is redacted; desktop's two rules miss most.
func TestRedactCatchesEverySpellingTheHubAccepts(t *testing.T) {
	cases := []struct {
		line   string
		groups []string
	}{
		{"my account k7m2-q9x4-c8v1-b6n3-r5t0-w2y7 thanks", nil},
		{"my account K7M2 Q9X4 C8V1 B6N3 R5T0 W2Y7 thanks", nil},
		{"acct K7M2Q9X4C8V1B6N3R5TOW2Y7", nil},
		{"acct k7m2q9x4c8v1b6n3r5t0w2y7.", nil},
		{"lookalikes K7M2-Q9X4-C8VI-B6N3-R5TO-W2Y7 and C8VL too", nil},
		{"acct K7M2 - Q9X4 - C8V1 - B6N3 - R5T0 - W2Y7", nil},
		{"acct K7M2Q9X4 C8V1B6N3 R5T0W2Y7 end", nil},
		{"note abcd K7M2 Q9X4 C8V1 B6N3 R5T0 W2Y7 thanks", nil},
		{"id K7M2-Q9X4-C8V1-B6N3-R5T0-W2Y7-", nil},
		{"id K7M2-Q9X4-C8V1-B6N3-R5T0-W2Y7-X", nil},
		{"{\"n\":\"K7M2\tQ9X4\tC8V1\tB6N3\tR5T0\tW2Y7\"}", nil},
		{"one digit is enough spaced ABCD EFGH JKMN PQRS TVWX YZA7", []string{"ABCD", "YZA7"}},
		{"dashed with no digit ABCD-EFGH-JKMN-PQRS-TVWX-YZAB", []string{"ABCD", "YZAB"}},
		{"lower dashed with no digit abcd-efgh-jkmn-pqrs-tvwx-yzab.", []string{"ABCD", "YZAB"}},
		{"desktop's unbroken rule ABCDEFGHJKMNPQRSTVWXYZAB", []string{"ABCD", "YZAB"}},
		{"desktop's dashed rule ABCD-EFGH-JKMN-PQRS-TVWX-YZAB", []string{"ABCD", "YZAB"}},
	}
	for _, tc := range cases {
		groups := tc.groups
		if groups == nil {
			groups = []string{"K7M2", "Q9X4", "C8V1", "B6N3", "R5T0", "W2Y7"}
		}
		out := strings.ToUpper(redactDiagnostics(tc.line, ""))
		for _, group := range groups {
			if strings.Contains(out, group) {
				t.Errorf("redact(%q) kept %s: %s", tc.line, group, out)
			}
		}
		if !strings.Contains(out, "[REDACTED:ACCOUNTNUMBER]") {
			t.Errorf("redact(%q) = %s, want an accountNumber marker", tc.line, out)
		}
	}
}

// Prose is often six four-letter words in a row; only the dashed form the app
// shows is redacted on its shape alone.
func TestRedactLeavesRunsThatAreNotAccountNumbers(t *testing.T) {
	for _, line := range []string{
		"n " + redactAccountNormalized + "Q",
		"x" + redactAccountNormalized,
		"md5 d41d8cd98f00b204e9800998ecf8427e ok",
		"five groups K7M2 Q9X4 C8V1 B6N3 R5T0.",
		"this does work with wifi only",
		"This Does Work With Wifi Only, then that fails over mobile data",
		"this does work with wifi-only mode",
		"\tthis\tdoes\twork\twith\twifi\tonly",
		"internationalizationsxyz is long",
	} {
		if got := redactDiagnostics(line, ""); got != line {
			t.Errorf("redact(%q) = %q, want it unchanged", line, got)
		}
	}
}

// Rejected matches must not rescan their own run, or a long one goes quadratic.
func TestRedactSurvivesLongRunsQuickly(t *testing.T) {
	line := "blob=" + strings.Repeat("Ab1", 40000) + "=== and " + strings.Repeat("1.", 40000)
	start := time.Now()
	if got := redactDiagnostics(line, ""); got != line {
		t.Fatalf("got a %d-byte result, want the %d-byte line unchanged", len(got), len(line))
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("took %v, want well under 10s", elapsed)
	}
}
