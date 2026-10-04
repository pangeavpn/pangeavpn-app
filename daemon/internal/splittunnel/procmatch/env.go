package procmatch

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// envSettings are the host facts the rule policy depends on; zero values fall back to defaults.
type envSettings struct {
	systemRoots []string
	programDirs []string
	usersRoots  []string
	initExe     string
	own         []string
	resolve     func(raw string, kind RuleKind) []string
}

// ruleEnv holds one OS flavour's validation policy, independent of the host OS.
type ruleEnv struct {
	goos        string
	systemRoots []string
	usersRoots  []string
	broad       []string
	protected   []compiledRule
	own         []compiledRule
	resolve     func(raw string, kind RuleKind) []string
}

var winProtectedNames = map[string]bool{
	"explorer.exe": true, "smss.exe": true, "csrss.exe": true, "wininit.exe": true,
	"winlogon.exe": true, "services.exe": true, "lsass.exe": true, "svchost.exe": true,
	"userinit.exe": true, "sihost.exe": true, "runtimebroker.exe": true, "dllhost.exe": true,
	"taskhostw.exe": true, "dwm.exe": true, "ctfmon.exe": true, "fontdrvhost.exe": true,
	"conhost.exe": true, "applicationframehost.exe": true, "startmenuexperiencehost.exe": true,
	"shellexperiencehost.exe": true, "searchhost.exe": true,
}

var linuxProtectedNames = map[string]bool{
	"systemd": true, "dbus-daemon": true, "gnome-shell": true, "gnome-session-binary": true,
	"plasmashell": true, "ksmserver": true, "kwin_wayland": true, "kwin_x11": true,
	"Xorg": true, "Xwayland": true,
}

var linuxProtectedPrefixes = []string{"dbus-broker", "gdm", "sddm", "lightdm", "xdg-desktop-portal"}

var linuxInterpreters = map[string]bool{
	"sh": true, "bash": true, "dash": true, "zsh": true, "fish": true, "ksh": true, "env": true,
	"node": true, "java": true, "php": true, "flatpak": true, "bwrap": true, "snap": true,
	"snap-confine": true, "xdg-open": true, "gtk-launch": true, "gio": true, "sudo": true, "pkexec": true,
}

var linuxInterpreterPrefixes = []string{"python", "perl", "ruby", "wine", "systemd"}

func newRuleEnv(goos string, s envSettings) *ruleEnv {
	e := &ruleEnv{goos: goos, resolve: s.resolve}
	switch goos {
	case "windows":
		e.initWindows(s)
	case "darwin":
		e.broad = []string{"/", "/Applications/", "/System/", "/Library/", "/usr/", "/Users/"}
		e.protected = []compiledRule{
			{RuleFile, "/sbin/launchd"},
			{RuleFile, "/usr/libexec/xpcproxy"},
			{RuleBundle, "/System/Library/CoreServices/Finder.app/"},
			{RuleBundle, "/System/Library/CoreServices/Dock.app/"},
			{RuleBundle, "/System/Library/CoreServices/loginwindow.app/"},
		}
	default:
		e.broad = []string{"/", "/usr/", "/usr/local/", "/usr/bin/", "/usr/sbin/", "/usr/share/",
			"/bin/", "/sbin/", "/opt/", "/home/", "/tmp/", "/var/", "/snap/", "/root/"}
		for _, p := range []string{"/sbin/init", "/usr/sbin/init", "/lib/systemd/systemd",
			"/usr/lib/systemd/systemd", "/usr/lib/xorg/Xorg", s.initExe} {
			if strings.HasPrefix(p, "/") {
				e.protected = append(e.protected, compiledRule{RuleFile, path.Clean(p)})
			}
		}
	}
	for _, p := range s.own {
		if r, code := parseRule(goos, p); code == "" && r.kind == RuleFile {
			e.own = append(e.own, r)
		}
	}
	return e
}

func (e *ruleEnv) initWindows(s envSettings) {
	add := func(dst []string, raw ...string) []string {
		for _, p := range raw {
			if !isDriveAbs(strings.ReplaceAll(p, "/", `\`)) {
				continue
			}
			c := cleanWindows(strings.ReplaceAll(p, "/", `\`))
			if !containsString(dst, c) {
				dst = append(dst, c)
			}
		}
		return dst
	}
	e.systemRoots = add(nil, append([]string{`c:\windows`}, s.systemRoots...)...)
	programs := add(nil, append([]string{`c:\program files`, `c:\program files (x86)`, `c:\program files (arm)`, `c:\programdata`}, s.programDirs...)...)
	e.usersRoots = add(nil, append([]string{`c:\users`}, s.usersRoots...)...)
	for _, root := range e.systemRoots {
		e.broad = append(e.broad, withSep(root, `\`))
		for name := range winProtectedNames {
			e.protected = append(e.protected,
				compiledRule{RuleFile, root + `\system32\` + name},
				compiledRule{RuleFile, root + `\syswow64\` + name})
		}
		e.protected = append(e.protected, compiledRule{RuleFile, root + `\explorer.exe`})
	}
	for _, p := range programs {
		e.broad = append(e.broad, withSep(p, `\`))
		if strings.HasSuffix(p, `\program files`) {
			e.broad = append(e.broad, p+`\windowsapps\`)
		}
	}
	for _, u := range e.usersRoots {
		e.broad = append(e.broad, withSep(u, `\`))
	}
}

func (e *ruleEnv) check(entry string, never []compiledRule) (compiledRule, string) {
	r, code := parseRule(e.goos, entry)
	if code == "" {
		code = e.policy(r, never)
	}
	return r, code
}

// policy applies the per-OS refusals to a parsed rule; never holds extra protected images.
func (e *ruleEnv) policy(r compiledRule, never []compiledRule) string {
	if r.kind != RuleFile && e.tooBroad(r.raw) {
		return CodeTooBroad
	}
	if e.systemProcess(r) {
		return CodeSystemProcess
	}
	for _, o := range e.own {
		if overlaps(r, o) {
			return CodeOwnImage
		}
	}
	for _, o := range never {
		if overlaps(r, o) {
			return CodeOwnImage
		}
	}
	return ""
}

func (e *ruleEnv) tooBroad(dir string) bool {
	cmp := dir
	if e.goos == "darwin" {
		cmp = strings.ToLower(dir)
	}
	for _, b := range e.broad {
		if e.goos == "darwin" {
			b = strings.ToLower(b)
		}
		if strings.HasPrefix(b, cmp) {
			return true
		}
	}
	switch e.goos {
	case "windows":
		if len(dir) <= len(`c:\`) {
			return true
		}
		for _, b := range e.broad {
			if strings.TrimSuffix(dir, `\`) == strings.TrimSuffix(b, `\`)+`\common files` {
				return true
			}
		}
		for _, u := range e.usersRoots {
			rest, ok := strings.CutPrefix(dir, u+`\`)
			if !ok {
				continue
			}
			parts := strings.Split(strings.TrimSuffix(rest, `\`), `\`)
			tail := strings.Join(parts[1:], `\`)
			switch tail {
			case "", "appdata", `appdata\local`, `appdata\roaming`, `appdata\local\programs`,
				"desktop", "downloads", "documents", `appdata\local\temp`:
				return true
			}
			// OneDrive folder backup moves Desktop and Documents under "OneDrive" or "OneDrive - <org>".
			if len(parts) >= 2 && strings.HasPrefix(parts[1], "onedrive") && (len(parts) == 2 || len(parts) == 3 && sharedUserDir(parts[2])) {
				return true
			}
		}
	case "darwin":
		if rest, ok := strings.CutPrefix(cmp, "/users/"); ok {
			parts := strings.Split(strings.TrimSuffix(rest, "/"), "/")
			if len(parts) == 1 || len(parts) == 2 && sharedUserDir(parts[1]) {
				return true
			}
		}
	default:
		parts := strings.Split(strings.Trim(dir, "/"), "/")
		switch {
		case len(parts) == 1 && strings.HasPrefix(parts[0], "lib"):
			return true
		case len(parts) == 2 && parts[0] == "usr" && strings.HasPrefix(parts[1], "lib"):
			return true
		case len(parts) == 2 && parts[0] == "home":
			return true
		case len(parts) == 3 && parts[0] == "home" && sharedUserDir(strings.ToLower(parts[2])):
			return true
		}
	}
	return false
}

// sharedUserDir names per-user folders that hold unrelated files; a folder rule there would
// exclude whatever anyone downloads or saves into it.
func sharedUserDir(name string) bool {
	switch name {
	case "desktop", "downloads", "documents":
		return true
	}
	return false
}

func (e *ruleEnv) systemProcess(r compiledRule) bool {
	cmp := r
	if e.goos == "darwin" {
		cmp.raw = strings.ToLower(r.raw)
	}
	for _, p := range e.protected {
		if e.goos == "darwin" {
			p.raw = strings.ToLower(p.raw)
		}
		if overlaps(cmp, p) {
			return true
		}
	}
	switch e.goos {
	case "windows":
		for _, root := range e.systemRoots {
			if r.kind == RuleFile && strings.HasPrefix(r.raw, root+`\`) && winProtectedNames[winBase(r.raw)] {
				return true
			}
			if r.kind == RuleDir && strings.HasPrefix(r.raw, root+`\systemapps\`) {
				return true
			}
		}
	case "darwin":
	default:
		if r.kind == RuleFile {
			base := path.Base(r.raw)
			return linuxProtectedBase(base) || linuxInterpreter(base)
		}
		if name, ok := strings.CutPrefix(r.raw, "/snap/"); ok && strings.Count(name, "/") == 1 {
			name, _, _ = strings.Cut(strings.TrimSuffix(name, "/"), ".")
			return linuxInterpreter(name)
		}
	}
	return false
}

// isStop reports whether the ancestor walk must stop at this image (session roots, system hosts).
func (e *ruleEnv) isStop(p string) bool {
	if p == "" {
		return false
	}
	switch e.goos {
	case "windows":
		p = normalizeWinImage(p)
		for _, root := range e.systemRoots {
			if strings.HasPrefix(p, root+`\`) && winProtectedNames[winBase(p)] {
				return true
			}
		}
		return false
	case "darwin":
		lp := strings.ToLower(p)
		for _, r := range e.protected {
			r.raw = strings.ToLower(r.raw)
			if r.covers(lp) {
				return true
			}
		}
		return false
	default:
		for _, r := range e.protected {
			if r.covers(p) {
				return true
			}
		}
		return linuxProtectedBase(path.Base(p))
	}
}

func linuxProtectedBase(base string) bool {
	return linuxProtectedNames[base] || hasAnyPrefix(base, linuxProtectedPrefixes)
}

func linuxInterpreter(base string) bool {
	return linuxInterpreters[base] || hasAnyPrefix(base, linuxInterpreterPrefixes)
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var (
	hostOnce sync.Once
	host     *ruleEnv
)

func hostEnv() *ruleEnv {
	hostOnce.Do(func() {
		s := hostSettings()
		if exe, err := os.Executable(); err == nil {
			s.own = append(s.own, exe)
			if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
				s.own = append(s.own, real)
			}
		}
		host = newRuleEnv(runtime.GOOS, s)
	})
	return host
}
