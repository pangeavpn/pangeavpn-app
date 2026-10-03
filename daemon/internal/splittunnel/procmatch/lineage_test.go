package procmatch

import (
	"fmt"
	"slices"
	"testing"
)

const (
	pathExplorer = `c:\windows\explorer.exe`
	pathLauncher = `c:\games\launcher\launcher.exe`
	pathGame     = `c:\games\game\game.exe`
	pathOther    = `c:\tools\other.exe`
)

func proc(pid, ppid int, start int64, path string) procInfo {
	return procInfo{pid: pid, ppid: ppid, start: start, path: path}
}

func viewOf(l *lineage, pid int, start int64) lineageView {
	return l.view(procKey{pid, start}, winEnv().isStop)
}

func chainOf(l *lineage, pid int, start int64) []string {
	return viewOf(l, pid, start).chain
}

func refresh(l *lineage, procs ...procInfo) {
	since := l.mark()
	l.sync(procs, since)
}

func TestLineageParentExitKeepsLink(t *testing.T) {
	l := newLineage()
	refresh(l, proc(10, 4, 1, pathExplorer), proc(20, 10, 5, pathLauncher), proc(30, 20, 7, pathGame))
	want := []string{pathGame, pathLauncher}
	if got := chainOf(l, 30, 7); !slices.Equal(got, want) {
		t.Fatalf("chain = %q, want %q", got, want)
	}

	refresh(l, proc(10, 4, 1, pathExplorer), proc(30, 20, 7, pathGame))
	if got := chainOf(l, 30, 7); !slices.Equal(got, want) {
		t.Fatalf("after launcher exit chain = %q, want %q", got, want)
	}
	if n := l.nodes[procKey{20, 5}]; n == nil || n.alive || n.refs != 1 {
		t.Fatalf("launcher node = %+v, want retained dead node with one ref", n)
	}

	refresh(l, proc(10, 4, 1, pathExplorer))
	if l.has(procKey{20, 5}) || l.has(procKey{30, 7}) {
		t.Fatal("exited launcher and game still retained after the game exited")
	}
	if len(l.nodes) != 1 || len(l.live) != 1 {
		t.Fatalf("nodes = %d live = %d, want 1/1", len(l.nodes), len(l.live))
	}
}

func TestLineageRetentionAcrossGenerations(t *testing.T) {
	l := newLineage()
	refresh(l, proc(20, 1, 5, pathLauncher), proc(30, 20, 6, `c:\games\stub.exe`), proc(40, 30, 7, pathGame))
	refresh(l, proc(40, 30, 7, pathGame))
	want := []string{pathGame, `c:\games\stub.exe`, pathLauncher}
	if got := chainOf(l, 40, 7); !slices.Equal(got, want) {
		t.Fatalf("chain = %q, want %q", got, want)
	}
	refresh(l, proc(41, 30, 8, pathOther))
	if len(l.nodes) != 1 {
		t.Fatalf("nodes = %d, want only the new process", len(l.nodes))
	}
}

func TestLineageSharedParentRefcount(t *testing.T) {
	l := newLineage()
	refresh(l, proc(20, 1, 5, pathLauncher), proc(30, 20, 6, pathGame), proc(31, 20, 6, pathOther))
	refresh(l, proc(30, 20, 6, pathGame), proc(31, 20, 6, pathOther))
	if n := l.nodes[procKey{20, 5}]; n == nil || n.refs != 2 {
		t.Fatalf("launcher node = %+v, want two refs", n)
	}
	refresh(l, proc(31, 20, 6, pathOther))
	if n := l.nodes[procKey{20, 5}]; n == nil || n.refs != 1 {
		t.Fatalf("launcher node = %+v, want one ref after one child exits", n)
	}
	refresh(l)
	if len(l.nodes) != 0 || len(l.live) != 0 {
		t.Fatalf("nodes = %d live = %d after everything exited", len(l.nodes), len(l.live))
	}
}

func TestLineageRejectsPidReuse(t *testing.T) {
	l := newLineage()
	refresh(l, proc(20, 1, 5, pathLauncher))
	refresh(l, proc(20, 1, 9, pathOther), proc(30, 20, 7, pathGame))
	if got := chainOf(l, 30, 7); !slices.Equal(got, []string{pathGame}) {
		t.Fatalf("child linked to a parent that started after it: %q", got)
	}
	if l.has(procKey{20, 5}) {
		t.Fatal("old holder of a reused pid is still recorded")
	}

	l = newLineage()
	refresh(l, proc(20, 1, 5, pathLauncher), proc(30, 20, 7, pathGame))
	refresh(l, proc(20, 1, 5, pathLauncher), proc(30, 99, 12, pathOther))
	if got := chainOf(l, 30, 12); !slices.Equal(got, []string{pathOther}) {
		t.Fatalf("new process on a reused pid inherited: %q", got)
	}
	if got := chainOf(l, 30, 7); got != nil {
		t.Fatalf("exited process still has a chain: %q", got)
	}
}

func TestLineageUnobservedParentNotLinked(t *testing.T) {
	l := newLineage()
	refresh(l, proc(20, 1, 5, pathLauncher))
	refresh(l, proc(10, 1, 1, pathOther))
	refresh(l, proc(10, 1, 1, pathOther), proc(30, 20, 7, pathGame))
	if got := chainOf(l, 30, 7); !slices.Equal(got, []string{pathGame}) {
		t.Fatalf("child linked to a parent that exited before it was seen: %q", got)
	}
}

func TestLineageAddAndSync(t *testing.T) {
	l := newLineage()
	l.add(proc(20, 1, 5, pathLauncher), nil)
	parent := procKey{20, 5}
	l.add(proc(30, 20, 7, pathGame), &parent)
	if got := chainOf(l, 30, 7); !slices.Equal(got, []string{pathGame, pathLauncher}) {
		t.Fatalf("chain = %q", got)
	}
	late := procKey{21, 9}
	l.add(proc(21, 1, 9, pathOther), nil)
	l.add(proc(31, 21, 8, pathGame), &late)
	if got := chainOf(l, 31, 8); !slices.Equal(got, []string{pathGame}) {
		t.Fatalf("add linked a parent that started later: %q", got)
	}

	since := l.mark()
	l.add(proc(40, 30, 11, pathOther), nil)
	l.sync([]procInfo{proc(30, 20, 7, pathGame), proc(40, 30, 10, `c:\stale.exe`)}, since)
	if !l.has(procKey{40, 11}) || l.has(procKey{40, 10}) {
		t.Fatal("a stale snapshot entry replaced a newer per-pid observation")
	}
	if !l.has(procKey{20, 5}) || l.nodes[procKey{20, 5}].alive {
		t.Fatal("launcher should be retained but dead after the snapshot missed it")
	}
	if l.has(procKey{21, 9}) {
		t.Fatal("unreferenced exited process retained")
	}

	l.add(proc(30, 1, 15, pathOther), nil)
	if l.has(procKey{30, 7}) || l.has(procKey{20, 5}) {
		t.Fatal("per-pid observation of a reused pid kept the old chain")
	}
}

func TestLineageSyncFillsPath(t *testing.T) {
	l := newLineage()
	refresh(l, proc(20, 1, 5, ""), proc(30, 20, 7, pathGame))
	if got := chainOf(l, 30, 7); !slices.Equal(got, []string{pathGame}) {
		t.Fatalf("chain through an unreadable image = %q, want it cut there", got)
	}
	if v := viewOf(l, 30, 7); len(v.keys) != 2 {
		t.Fatalf("protection keys = %v, want both processes", v.keys)
	}
	refresh(l, proc(20, 1, 5, pathLauncher), proc(30, 20, 7, pathGame))
	if got := chainOf(l, 30, 7); !slices.Equal(got, []string{pathGame, pathLauncher}) {
		t.Fatalf("chain = %q after the path became readable", got)
	}
}

func TestLineageViewStops(t *testing.T) {
	l := newLineage()
	refresh(l,
		proc(4, 0, 0, `c:\windows\system32\ntoskrnl.exe`),
		proc(400, 4, 1, `c:\windows\system32\smss.exe`),
		proc(500, 400, 2, `c:\windows\system32\winlogon.exe`),
		proc(600, 500, 3, `c:\windows\system32\userinit.exe`),
		proc(700, 600, 4, `C:\WINDOWS\explorer.exe`),
		proc(800, 700, 5, pathLauncher),
		proc(900, 800, 6, pathGame),
	)
	v := viewOf(l, 900, 6)
	if !slices.Equal(v.chain, []string{pathGame, pathLauncher}) {
		t.Fatalf("chain = %q, want it cut at explorer", v.chain)
	}
	if len(v.keys) != 6 || len(v.paths) != 6 {
		t.Fatalf("protection walk = %v, want every recorded ancestor below pid 4", v.keys)
	}
	if got := chainOf(l, 700, 4); got != nil {
		t.Fatalf("explorer chain = %q, want empty", got)
	}

	l = newLineage()
	refresh(l, proc(1, 0, 0, "/sbin/init"), proc(50, 1, 2, "/usr/bin/app"))
	if v := l.view(procKey{50, 2}, linuxEnv().isStop); !slices.Equal(v.chain, []string{"/usr/bin/app"}) || len(v.keys) != 1 {
		t.Fatalf("view through pid 1 = %+v", v)
	}
	l = newLineage()
	refresh(l, proc(2, 1, 0, "/usr/lib/systemd/systemd"), proc(60, 2, 1, "/usr/bin/gnome-shell"), proc(70, 60, 2, "/opt/app/app"))
	if v := l.view(procKey{70, 2}, linuxEnv().isStop); !slices.Equal(v.chain, []string{"/opt/app/app"}) {
		t.Fatalf("linux chain = %q, want it cut at gnome-shell", v.chain)
	}
	l = newLineage()
	refresh(l, proc(100, 1, 0, "/sbin/launchd"), proc(110, 100, 1, "/System/Library/CoreServices/Finder.app/Contents/MacOS/Finder"), proc(120, 110, 2, "/Applications/Foo.app/Contents/MacOS/Foo"))
	if v := l.view(procKey{120, 2}, darwinEnv().isStop); !slices.Equal(v.chain, []string{"/Applications/Foo.app/Contents/MacOS/Foo"}) {
		t.Fatalf("darwin chain = %q, want it cut at Finder", v.chain)
	}
}

func TestLineageDepthLimit(t *testing.T) {
	l := newLineage()
	var procs []procInfo
	for i := 0; i < 40; i++ {
		procs = append(procs, proc(100+i, 99+i, int64(i), fmt.Sprintf(`c:\deep\p%02d.exe`, i)))
	}
	refresh(l, procs...)
	v := viewOf(l, 139, 39)
	if len(v.keys) != maxChainDepth || len(v.chain) != maxChainDepth {
		t.Fatalf("walk length = %d/%d, want %d", len(v.keys), len(v.chain), maxChainDepth)
	}
}

func TestVerdictFor(t *testing.T) {
	env := winEnv()
	rules := compileIn(t, env, `C:\Games\Launcher\launcher.exe`)
	never := compileImages(env, []string{`C:\Program Files\PangeaVPN\PangeaVPN.exe`})
	l := newLineage()
	refresh(l,
		proc(700, 600, 4, pathExplorer),
		proc(710, 700, 5, `c:\program files\pangeavpn\pangeavpn.exe`),
		proc(720, 710, 6, pathGame),
		proc(800, 700, 5, pathLauncher),
		proc(810, 800, 6, pathGame),
		proc(820, 810, 7, `c:\games\game\crash.exe`),
		proc(900, 999, 5, pathLauncher),
		proc(910, 900, 6, pathOther),
		proc(730, 700, 6, pathOther),
		proc(950, 1, 2, `c:\windows\system32\svchost.exe`),
		proc(960, 950, 3, pathGame),
	)
	self := procKey{900, 5}
	nevers := []*Rules{never, nil}
	cases := []struct {
		name  string
		key   procKey
		want  Verdict
		chain []string
	}{
		{"owner matches", procKey{800, 5}, VerdictBypass, []string{pathLauncher}},
		{"child inherits", procKey{810, 6}, VerdictBypass, []string{pathGame, pathLauncher}},
		{"grandchild inherits", procKey{820, 7}, VerdictBypass, []string{`c:\games\game\crash.exe`, pathGame, pathLauncher}},
		{"unrelated", procKey{730, 6}, VerdictTunnel, []string{pathOther}},
		{"under never-bypass image", procKey{720, 6}, VerdictTunnel, nil},
		{"self", procKey{900, 5}, VerdictTunnel, nil},
		{"self child", procKey{910, 6}, VerdictTunnel, nil},
		{"child of system host", procKey{960, 3}, VerdictTunnel, []string{pathGame}},
		{"unknown", procKey{1234, 1}, VerdictTunnel, nil},
	}
	for _, tc := range cases {
		v, chain := verdictFor(rules, nevers, self, l.view(tc.key, env.isStop))
		if v != tc.want || !slices.Equal(chain, tc.chain) {
			t.Errorf("%s: verdict %v chain %q, want %v %q", tc.name, v, chain, tc.want, tc.chain)
		}
	}

	injected := newRules("windows", []compiledRule{{RuleFile, `c:\windows\system32\svchost.exe`}, {RuleFile, pathExplorer}})
	for _, k := range []procKey{{960, 3}, {730, 6}, {700, 4}} {
		if v, _ := verdictFor(injected, nevers, self, l.view(k, env.isStop)); v != VerdictTunnel {
			t.Errorf("pid %d inherited from a session root or system host", k.pid)
		}
	}

	gameRule := compileIn(t, env, `C:\Games\Game\`)
	if v, chain := verdictFor(gameRule, nevers, self, l.view(procKey{720, 6}, env.isStop)); v != VerdictTunnel || chain != nil {
		t.Errorf("child of a never-bypass image: verdict %v chain %q", v, chain)
	}
	if v, _ := verdictFor(gameRule, nevers, self, l.view(procKey{810, 6}, env.isStop)); v != VerdictBypass {
		t.Error("direct match lost")
	}
	if v, _ := verdictFor(nil, nevers, self, l.view(procKey{810, 6}, env.isStop)); v != VerdictTunnel {
		t.Error("empty rules bypassed")
	}
}
