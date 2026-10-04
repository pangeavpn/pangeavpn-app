package procmatch

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func winEnv() *ruleEnv {
	return newRuleEnv("windows", envSettings{
		systemRoots: []string{`C:\WINDOWS`},
		programDirs: []string{`C:\Program Files`, `C:\Program Files (x86)`, `C:\ProgramData`},
		usersRoots:  []string{`C:\Users`},
		own:         []string{`C:\Program Files\PangeaVPN\pangea-daemon.exe`},
	})
}

func darwinEnv() *ruleEnv {
	return newRuleEnv("darwin", envSettings{
		own: []string{"/Applications/PangeaVPN.app/Contents/MacOS/pangea-daemon"},
	})
}

func linuxEnv() *ruleEnv {
	return newRuleEnv("linux", envSettings{
		initExe: "/usr/lib/custom/init-x",
		own:     []string{"/opt/pangeavpn/pangea-daemon"},
	})
}

type ruleCase struct {
	entry string
	code  string
}

func runRuleCases(t *testing.T, env *ruleEnv, cases []ruleCase) {
	t.Helper()
	for _, tc := range cases {
		r, code := env.check(tc.entry, nil)
		if code != tc.code {
			t.Errorf("%s check(%q) = %q, want %q", env.goos, tc.entry, code, tc.code)
			continue
		}
		if code != "" {
			continue
		}
		if _, again := env.check(r.raw, nil); again != "" {
			t.Errorf("%s normalised %q (from %q) fails validation: %q", env.goos, r.raw, tc.entry, again)
		}
	}
}

func TestValidateWindowsForms(t *testing.T) {
	long := `C:\` + strings.Repeat("a", maxRuleLen-len(`C:\`)-len(".exe")) + ".exe"
	runRuleCases(t, winEnv(), []ruleCase{
		{`C:\Games\Foo\foo.exe`, ""},
		{`c:/games/foo/FOO.EXE`, ""},
		{`D:\SteamLibrary\steamapps\common\Game\`, ""},
		{`C:\Games\Foo`, CodeUnsupportedForm},
		{`C:\Games\Foo\foo.bat`, CodeUnsupportedForm},
		{`C:\Games\Foo\foo.exe:alt`, CodeUnsupportedForm},
		{`foo.exe`, CodeNotAbsolute},
		{`C:foo.exe`, CodeNotAbsolute},
		{`\Games\foo.exe`, CodeNotAbsolute},
		{`1:\Games\foo.exe`, CodeNotAbsolute},
		{``, CodeNotAbsolute},
		{`\\?\C:\Games\foo.exe`, CodeUnsupportedForm},
		{`\\.\C:\Games\foo.exe`, CodeUnsupportedForm},
		{`\??\C:\Games\foo.exe`, CodeUnsupportedForm},
		{`\\server\share\foo.exe`, CodeUnsupportedForm},
		{`//server/share/`, CodeUnsupportedForm},
		{`\\?\UNC\server\share\foo.exe`, CodeUnsupportedForm},
		{"C:\\Games\\foo\x00.exe", CodeNUL},
		{long, ""},
		{long + "x", CodeTooLong},
	})
}

func TestValidateWindowsPolicy(t *testing.T) {
	runRuleCases(t, winEnv(), []ruleCase{
		{`C:\`, CodeTooBroad},
		{`c:/`, CodeTooBroad},
		{`D:\`, CodeTooBroad},
		{`C:\..\`, CodeTooBroad},
		{`C:\Windows\`, CodeTooBroad},
		{`C:\Windows\..\`, CodeTooBroad},
		{`C:\Program Files\`, CodeTooBroad},
		{`C:\PROGRAM FILES (X86)\`, CodeTooBroad},
		{`C:\ProgramData\`, CodeTooBroad},
		{`C:\Program Files\WindowsApps\`, CodeTooBroad},
		{`C:\Users\`, CodeTooBroad},
		{`C:\Users\Public\`, CodeTooBroad},
		{`C:\Users\John Smith\`, CodeTooBroad},
		{`C:\Users\bob\AppData\`, CodeTooBroad},
		{`C:\Users\bob\AppData\Local\`, CodeTooBroad},
		{`C:\Users\bob\AppData\Roaming\`, CodeTooBroad},
		{`C:\Users\bob\AppData\Local\Programs\`, CodeTooBroad},
		{`C:\Users\bob\Downloads\`, CodeTooBroad},
		{`C:\Users\bob\Desktop\`, CodeTooBroad},
		{`C:\Users\bob\Documents\`, CodeTooBroad},
		{`C:\Users\bob\AppData\Local\Temp\`, CodeTooBroad},
		{`C:\Program Files\Common Files\`, CodeTooBroad},
		{`C:\Program Files (x86)\Common Files\`, CodeTooBroad},
		{`C:\Users\bob\Downloads\Game\`, ""},
		{`C:\Users\bob\OneDrive\`, CodeTooBroad},
		{`C:\Users\bob\OneDrive\Desktop\`, CodeTooBroad},
		{`C:\Users\bob\OneDrive - Contoso\Documents\`, CodeTooBroad},
		{`C:\Users\bob\OneDrive\Desktop\Tools\`, ""},
		{`C:\Program Files (Arm)\`, CodeTooBroad},
		{`C:\Program Files (Arm)\Vendor\`, ""},
		{`C:\Program Files\Common Files\Vendor\`, ""},
		{`C:\Users\bob\AppData\Local\Discord\`, ""},
		{`C:\Users\bob\AppData\Local\Programs\Microsoft VS Code\`, ""},
		{`C:\Users\bob\Desktop\Tools\`, ""},
		{`C:\Program Files\Steam\`, ""},
		{`C:\Program Files\WindowsApps\Microsoft.Foo_1.0_x64__8wekyb3d8bbwe\`, ""},
		{`C:\Windows\explorer.exe`, CodeSystemProcess},
		{`C:\WINDOWS\System32\svchost.exe`, CodeSystemProcess},
		{`C:\Windows\SysWOW64\dllhost.exe`, CodeSystemProcess},
		{`C:\Windows\System32\RuntimeBroker.exe`, CodeSystemProcess},
		{`C:\Windows\System32\conhost.exe`, CodeSystemProcess},
		{`C:\Windows\SystemApps\Microsoft.Windows.StartMenuExperienceHost_cw5n1h2txyewy\StartMenuExperienceHost.exe`, CodeSystemProcess},
		{`C:\Windows\SystemApps\`, CodeSystemProcess},
		{`C:\Windows\System32\`, CodeSystemProcess},
		{`C:\Windows\SysWOW64\`, CodeSystemProcess},
		{`C:\Games\..\Windows\explorer.exe`, CodeSystemProcess},
		{`C:\Windows.\explorer.exe `, CodeSystemProcess},
		{`C:\Windows\System32\cmd.exe`, ""},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, ""},
		{`C:\Program Files\PowerShell\7\pwsh.exe`, ""},
		{`C:\Windows\System32\wscript.exe`, ""},
		{`C:\Windows\System32\rundll32.exe`, ""},
		{`C:\Games\explorer.exe`, ""},
		{`C:\Program Files\PangeaVPN\pangea-daemon.exe`, CodeOwnImage},
		{`C:\Program Files\PangeaVPN\`, CodeOwnImage},
		{`C:\Program Files\PangeaVPN\resources\`, ""},
	})
}

func TestValidateWindowsCustomSystemRoot(t *testing.T) {
	env := newRuleEnv("windows", envSettings{systemRoots: []string{`D:\WINNT`}, programDirs: []string{`E:\Apps`}})
	runRuleCases(t, env, []ruleCase{
		{`D:\WINNT\`, CodeTooBroad},
		{`D:\WINNT\explorer.exe`, CodeSystemProcess},
		{`D:\WINNT\System32\lsass.exe`, CodeSystemProcess},
		{`C:\Windows\explorer.exe`, CodeSystemProcess},
		{`E:\Apps\`, CodeTooBroad},
		{`E:\Apps\Game\`, ""},
	})
}

func TestValidateDarwin(t *testing.T) {
	runRuleCases(t, darwinEnv(), []ruleCase{
		{"/Applications/Safari.app", ""},
		{"/Applications/Safari.app/", ""},
		{"/Applications/Foo.app/Contents/MacOS/Foo", ""},
		{"/opt/homebrew/bin/", ""},
		{"/Users/bob/Apps/", ""},
		{"/usr/local/bin/tool", ""},
		{"Applications/Foo.app", CodeNotAbsolute},
		{"~/Applications/Foo.app", CodeNotAbsolute},
		{"/Applications/Fo\x00o.app", CodeNUL},
		{"/", CodeTooBroad},
		{"/Applications/", CodeTooBroad},
		{"/applications/", CodeTooBroad},
		{"/System/", CodeTooBroad},
		{"/Library/", CodeTooBroad},
		{"/usr/", CodeTooBroad},
		{"/Users/", CodeTooBroad},
		{"/Users/bob/", CodeTooBroad},
		{"/Users/bob/Downloads/", CodeTooBroad},
		{"/Users/bob/Desktop/", CodeTooBroad},
		{"/Users/bob/Downloads/Tool.app", ""},
		{"/sbin/launchd", CodeSystemProcess},
		{"/sbin/", CodeSystemProcess},
		{"/usr/libexec/xpcproxy", CodeSystemProcess},
		{"/usr/libexec/", CodeSystemProcess},
		{"/System/Library/CoreServices/Finder.app", CodeSystemProcess},
		{"/System/Library/CoreServices/Finder.app/Contents/MacOS/Finder", CodeSystemProcess},
		{"/system/library/coreservices/dock.app/", CodeSystemProcess},
		{"/System/Library/CoreServices/loginwindow.app", CodeSystemProcess},
		{"/System/Library/CoreServices/", CodeSystemProcess},
		{"/Applications/PangeaVPN.app", CodeOwnImage},
		{"/Applications/PangeaVPN.app/Contents/MacOS/pangea-daemon", CodeOwnImage},
	})
}

func TestValidateLinux(t *testing.T) {
	cases := []ruleCase{
		{"/usr/bin/firefox", ""},
		{"/opt/google/chrome/", ""},
		{"/opt/google/chrome/chrome", ""},
		{"/home/bob/apps/", ""},
		{"/home/bob/Downloads/", CodeTooBroad},
		{"/home/bob/Documents/", CodeTooBroad},
		{"/home/bob/Downloads/tool/", ""},
		{"/var/lib/flatpak/app/org.mozilla.firefox/", ""},
		{"/snap/bin/firefox", ""},
		{"/snap/firefox/4793/usr/lib/firefox/firefox", ""},
		{"usr/bin/firefox", CodeNotAbsolute},
		{"/usr/bin/fire\x00fox", CodeNUL},
		{"/usr/lib/systemd/systemd", CodeSystemProcess},
		{"/usr/lib/systemd/", CodeSystemProcess},
		{"/lib/systemd/systemd", CodeSystemProcess},
		{"/sbin/init", CodeSystemProcess},
		{"/usr/lib/custom/init-x", CodeSystemProcess},
		{"/usr/lib/custom/", CodeSystemProcess},
		{"/usr/bin/gnome-shell", CodeSystemProcess},
		{"/usr/libexec/gnome-session-binary", CodeSystemProcess},
		{"/usr/bin/plasmashell", CodeSystemProcess},
		{"/usr/bin/kwin_wayland", CodeSystemProcess},
		{"/usr/bin/Xwayland", CodeSystemProcess},
		{"/usr/lib/xorg/Xorg", CodeSystemProcess},
		{"/usr/sbin/gdm3", CodeSystemProcess},
		{"/usr/bin/sddm-greeter", CodeSystemProcess},
		{"/usr/sbin/lightdm", CodeSystemProcess},
		{"/usr/bin/dbus-daemon", CodeSystemProcess},
		{"/usr/bin/dbus-broker-launch", CodeSystemProcess},
		{"/usr/libexec/xdg-desktop-portal-gnome", CodeSystemProcess},
		{"/opt/pangeavpn/pangea-daemon", CodeOwnImage},
		{"/opt/pangeavpn/", CodeOwnImage},
		{"/snap/bin/node", CodeSystemProcess},
	}
	for _, name := range []string{"sh", "bash", "dash", "zsh", "fish", "ksh", "env", "python3", "python3.12",
		"perl", "perl5.36", "ruby3.1", "node", "java", "php", "wine", "wine64-preloader", "flatpak", "bwrap",
		"snap", "snap-confine", "xdg-open", "gtk-launch", "gio", "systemd-run", "sudo", "pkexec"} {
		cases = append(cases, ruleCase{"/usr/bin/" + name, CodeSystemProcess})
	}
	for _, dir := range []string{"/", "/usr/", "/usr/local/", "/usr/bin/", "/usr/sbin/", "/usr/lib/", "/usr/lib64/",
		"/usr/libexec/", "/usr/share/", "/bin/", "/sbin/", "/lib/", "/lib64/", "/opt/", "/home/", "/home/bob/",
		"/root/", "/tmp/", "/var/", "/snap/", "/usr/../usr/bin/"} {
		cases = append(cases, ruleCase{dir, CodeTooBroad})
	}
	runRuleCases(t, linuxEnv(), cases)
}

func TestValidateRuleHost(t *testing.T) {
	var cases []ruleCase
	switch runtime.GOOS {
	case "windows":
		cases = []ruleCase{{`C:\Windows\explorer.exe`, CodeSystemProcess}, {`C:\`, CodeTooBroad}, {`relative.exe`, CodeNotAbsolute}}
	case "darwin":
		cases = []ruleCase{{"/sbin/launchd", CodeSystemProcess}, {"/", CodeTooBroad}, {"relative", CodeNotAbsolute}}
	default:
		cases = []ruleCase{{"/usr/bin/bash", CodeSystemProcess}, {"/", CodeTooBroad}, {"relative", CodeNotAbsolute}}
	}
	for _, tc := range cases {
		if got := ValidateRule(tc.entry); got != tc.code {
			t.Errorf("ValidateRule(%q) = %q, want %q", tc.entry, got, tc.code)
		}
	}
}

func TestRuleKinds(t *testing.T) {
	cases := []struct {
		env   *ruleEnv
		entry string
		kind  RuleKind
		raw   string
	}{
		{winEnv(), `C:\Games\Foo\Foo.exe`, RuleFile, `c:\games\foo\foo.exe`},
		{winEnv(), `C:/Games//Foo/./Bar/../Foo.EXE`, RuleFile, `c:\games\foo\foo.exe`},
		{winEnv(), `C:\Games\Foo\`, RuleDir, `c:\games\foo\`},
		{winEnv(), `c:\games\foo\\`, RuleDir, `c:\games\foo\`},
		{winEnv(), `C:\Games\Foo. \`, RuleDir, `c:\games\foo\`},
		{darwinEnv(), "/Applications/Foo.app", RuleBundle, "/Applications/Foo.app/"},
		{darwinEnv(), "/Applications/Foo.APP/", RuleBundle, "/Applications/Foo.APP/"},
		{darwinEnv(), "/opt/tools/", RuleDir, "/opt/tools/"},
		{darwinEnv(), "/opt/tools/bin//tool", RuleFile, "/opt/tools/bin/tool"},
		{linuxEnv(), "/opt/app/", RuleDir, "/opt/app/"},
		{linuxEnv(), "/opt/app/bin/app", RuleFile, "/opt/app/bin/app"},
		{linuxEnv(), "/snap/bin/firefox", RuleDir, "/snap/firefox/"},
		{linuxEnv(), "/snap/bin/chromium.chromedriver", RuleDir, "/snap/chromium/"},
		{linuxEnv(), "/snap/firefox/4793/usr/lib/firefox/firefox", RuleDir, "/snap/firefox/"},
		{linuxEnv(), "/snap/firefox/current/", RuleDir, "/snap/firefox/"},
	}
	for _, tc := range cases {
		r, code := tc.env.check(tc.entry, nil)
		if code != "" || r.kind != tc.kind || r.raw != tc.raw {
			t.Errorf("%s %q -> %v %q (%q), want %v %q", tc.env.goos, tc.entry, r.kind, r.raw, code, tc.kind, tc.raw)
		}
	}
}

func compileIn(t *testing.T, env *ruleEnv, entries ...string) *Rules {
	t.Helper()
	r, errs := compileRules(env, entries, nil)
	if len(errs) != 0 {
		t.Fatalf("compile %q: %v", entries, errs)
	}
	r.waitResolved()
	return r
}

type matchCase struct {
	path string
	want bool
}

func checkMatches(t *testing.T, r *Rules, cases []matchCase) {
	t.Helper()
	for _, tc := range cases {
		if got := r.MatchPath(tc.path); got != tc.want {
			t.Errorf("MatchPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestMatchWindows(t *testing.T) {
	r := compileIn(t, winEnv(), `C:\Games\`, `D:\Tools\tool.exe`)
	checkMatches(t, r, []matchCase{
		{`c:\games\x.exe`, true},
		{`C:\GAMES\Sub\Dir\y.exe`, true},
		{`\\?\C:\Games\x.exe`, true},
		{`c:/games/x.exe`, true},
		{`c:\games2\x.exe`, false},
		{`c:\games.exe`, false},
		{`c:\games`, false},
		{`d:\tools\tool.exe`, true},
		{`D:\Tools\TOOL.EXE`, true},
		{`d:\tools\tool.exe2`, false},
		{`d:\tools\tool.ex`, false},
		{`d:\tools\sub\tool.exe`, false},
		{``, false},
	})
	if !r.MatchChain([]string{`c:\other\a.exe`, `c:\games\launcher.exe`}) {
		t.Error("MatchChain misses an ancestor match")
	}
	if r.MatchChain([]string{`c:\other\a.exe`, `c:\games2\b.exe`}) || r.MatchChain(nil) {
		t.Error("MatchChain matched nothing-matching chain")
	}
}

func TestMatchDarwinBundles(t *testing.T) {
	r := compileIn(t, darwinEnv(), "/Applications/Foo.app", "/opt/tools/", "/usr/local/bin/tool")
	checkMatches(t, r, []matchCase{
		{"/Applications/Foo.app/Contents/MacOS/Foo", true},
		{"/Applications/Foo.app/Contents/Frameworks/Foo Helper.app/Contents/MacOS/Foo Helper", true},
		{"/Applications/Foo.app", false},
		{"/Applications/Foo.app2/Contents/MacOS/Foo", false},
		{"/Applications/Foo.appx/Contents/MacOS/Foo", false},
		{"/applications/foo.app/Contents/MacOS/Foo", false},
		{"/opt/tools/x", true},
		{"/opt/toolsx/x", false},
		{"/usr/local/bin/tool", true},
		{"/usr/local/bin/tool2", false},
		{"/private/var/folders/xy/abc/T/AppTranslocation/0A1B-2C3D/d/Foo.app/Contents/MacOS/Foo", true},
		{"/private/var/folders/xy/abc/T/AppTranslocation/0A1B-2C3D/d/Bar.app/Contents/MacOS/Bar", false},
		{"/private/var/folders/xy/abc/T/AppTranslocation/0A1B-2C3D/d/tool", false},
	})
}

func TestMatchLinuxSnap(t *testing.T) {
	r := compileIn(t, linuxEnv(), "/snap/bin/firefox", "/opt/app/bin/app")
	checkMatches(t, r, []matchCase{
		{"/snap/firefox/4793/usr/lib/firefox/firefox", true},
		{"/snap/firefox/5001/usr/lib/firefox/firefox", true},
		{"/snap/firefox-beta/1/usr/lib/firefox/firefox", false},
		{"/usr/bin/snap", false},
		{"/opt/app/bin/app", true},
		{"/opt/app/bin/app (deleted)", false},
	})
}

func TestCompileDedupeOrderAndCap(t *testing.T) {
	env := winEnv()
	r, errs := compileRules(env, []string{`C:\a\b.exe`, `C:\Windows\explorer.exe`, `c:/A/B.EXE`, `C:\a\.\b.exe`, `relative.exe`}, nil)
	if len(r.rules) != 1 {
		t.Fatalf("rules = %v, want one deduplicated rule", r.rules)
	}
	want := []RuleError{{1, CodeSystemProcess}, {4, CodeNotAbsolute}}
	if fmt.Sprint(errs) != fmt.Sprint(want) {
		t.Fatalf("errs = %v, want %v", errs, want)
	}

	var many []string
	for i := 0; i < MaxRules+2; i++ {
		many = append(many, fmt.Sprintf(`C:\Games\g%03d.exe`, i))
	}
	many = append(many, `C:\Games\g000.exe`)
	r, errs = compileRules(env, many, nil)
	if len(r.rules) != MaxRules {
		t.Fatalf("compiled %d rules, want %d", len(r.rules), MaxRules)
	}
	want = []RuleError{{MaxRules, CodeTooMany}, {MaxRules + 1, CodeTooMany}}
	if fmt.Sprint(errs) != fmt.Sprint(want) {
		t.Fatalf("errs = %v, want %v", errs, want)
	}
}

func TestRulesEmptyEqual(t *testing.T) {
	env := winEnv()
	var nilRules *Rules
	empty, _ := compileRules(env, nil, nil)
	allBad, _ := compileRules(env, []string{`C:\`}, nil)
	a, _ := compileRules(env, []string{`C:\a\a.exe`, `C:\b\`}, nil)
	b, _ := compileRules(env, []string{`c:/B/`, `C:\A\A.EXE`, `C:\a\a.exe`}, nil)
	c, _ := compileRules(env, []string{`C:\a\a.exe`}, nil)
	d, _ := compileRules(env, []string{`C:\a\a.exe`, `C:\b\b.exe`}, nil)
	p, _ := compileRules(env, []string{`C:\a\a.exe`, `C:\b\`}, []string{`C:\Program Files\PangeaVPN\PangeaVPN.exe`})
	if !nilRules.Empty() || !empty.Empty() || !allBad.Empty() || a.Empty() {
		t.Fatal("Empty is wrong")
	}
	if !nilRules.Equal(empty) || !empty.Equal(allBad) || !allBad.Equal(nilRules) {
		t.Fatal("empty rule sets differ")
	}
	if !a.Equal(b) || !b.Equal(a) {
		t.Fatal("same entries in another order/form differ")
	}
	if a.Equal(c) || a.Equal(d) || a.Equal(empty) || empty.Equal(a) || a.Equal(p) {
		t.Fatal("different rule sets compare equal")
	}
	if nilRules.MatchPath(`c:\a\a.exe`) || nilRules.MatchChain([]string{`c:\a\a.exe`}) {
		t.Fatal("nil rules matched")
	}
}

func TestCompileNeverBypass(t *testing.T) {
	env := winEnv()
	never := []string{`C:\Program Files\PangeaVPN\PangeaVPN.exe`, `C:\Program Files\PangeaVPN\resources\helper\`}
	r, errs := compileRules(env, []string{
		`C:\Program Files\PangeaVPN\PangeaVPN.exe`,
		`C:\Program Files\PangeaVPN\`,
		`C:\Program Files\PangeaVPN\resources\helper\x.exe`,
		`C:\Program Files\PangeaVPN\resources\`,
		`C:\Program Files\PangeaVPN\other.exe`,
	}, never)
	want := []RuleError{{0, CodeOwnImage}, {1, CodeOwnImage}, {2, CodeOwnImage}, {3, CodeOwnImage}}
	if fmt.Sprint(errs) != fmt.Sprint(want) {
		t.Fatalf("errs = %v, want %v", errs, want)
	}
	if len(r.rules) != 1 || r.never.Empty() || !r.never.MatchPath(`c:\program files\pangeavpn\pangeavpn.exe`) {
		t.Fatalf("rules = %v, never = %v", r.rules, r.never)
	}
}

func fakeResolver(m map[string][]string) func(string, RuleKind) []string {
	return func(raw string, kind RuleKind) []string {
		return m[raw]
	}
}

func TestResolvedFormsWindows(t *testing.T) {
	env := winEnv()
	env.resolve = fakeResolver(map[string][]string{
		`c:\steam\steamapps\common\game\`: {`\\?\D:\SteamLibrary\steamapps\common\Game`},
		`c:\links\tool.exe`:               {`\\?\C:\Users\bob\AppData\Local\Microsoft\WinGet\Packages\Tool\tool.exe`},
		`c:\escape\`:                      {`\\?\C:\Windows`},
		`c:\share\app.exe`:                {`\\?\UNC\server\share\app.exe`},
	})
	r := compileIn(t, env, `C:\Steam\steamapps\common\Game\`, `C:\Links\tool.exe`, `C:\Escape\`, `C:\Share\app.exe`)
	checkMatches(t, r, []matchCase{
		{`c:\steam\steamapps\common\game\game.exe`, true},
		{`d:\steamlibrary\steamapps\common\game\game.exe`, true},
		{`d:\steamlibrary\steamapps\common\game2\game.exe`, false},
		{`c:\users\bob\appdata\local\microsoft\winget\packages\tool\tool.exe`, true},
		{`c:\links\tool.exe`, true},
		{`c:\windows\system32\svchost.exe`, false},
		{`c:\escape\x.exe`, true},
		{`\\server\share\app.exe`, false},
	})
}

func TestResolvedFormsDarwinLinux(t *testing.T) {
	denv := darwinEnv()
	denv.resolve = fakeResolver(map[string][]string{
		"/Applications/Safari.app/": {"/System/Volumes/Preboot/Cryptexes/App/System/Applications/Safari.app", "/System/Cryptexes/App/System/Applications/Safari.app"},
	})
	r := compileIn(t, denv, "/Applications/Safari.app")
	checkMatches(t, r, []matchCase{
		{"/System/Volumes/Preboot/Cryptexes/App/System/Applications/Safari.app/Contents/MacOS/Safari", true},
		{"/System/Cryptexes/App/System/Applications/Safari.app/Contents/MacOS/Safari", true},
		{"/Applications/Safari.app/Contents/MacOS/Safari", true},
		{"/System/Volumes/Preboot/Cryptexes/App/System/Applications/Safari.app", false},
	})

	lenv := linuxEnv()
	lenv.resolve = fakeResolver(map[string][]string{
		"/usr/bin/firefox":   {"/usr/lib/firefox/firefox"},
		"/home/bob/bin/mysh": {"/usr/bin/bash"},
		"/home/bob/game":     {"/snap/game/12/bin/game"},
		"/opt/app/":          {"/data/app"},
	})
	r = compileIn(t, lenv, "/usr/bin/firefox", "/home/bob/bin/mysh", "/home/bob/game", "/opt/app/")
	checkMatches(t, r, []matchCase{
		{"/usr/lib/firefox/firefox", true},
		{"/usr/bin/firefox", true},
		{"/usr/bin/bash", false},
		{"/home/bob/bin/mysh", true},
		{"/snap/game/12/bin/game", false},
		{"/data/app/run", true},
		{"/data/apprun", false},
	})
}

func TestResolveDeadline(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	slow := func(string, RuleKind) []string {
		<-block
		return []string{"/late"}
	}
	start := time.Now()
	if got := resolveWithDeadline(slow, compiledRule{RuleFile, "/x"}, 50*time.Millisecond); got != nil {
		t.Fatalf("got %v from a resolver that missed its deadline", got)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("deadline took %v", d)
	}

	env := linuxEnv()
	env.resolve = func(raw string, kind RuleKind) []string {
		if raw == "/opt/slow/slow" {
			<-block
		}
		return nil
	}
	r, _ := compileRules(env, []string{"/opt/slow/slow", "/opt/fast/fast"}, nil)
	if !r.MatchPath("/opt/slow/slow") || !r.MatchPath("/opt/fast/fast") {
		t.Fatal("raw forms must match before resolution finishes")
	}
}
