package procmatch

import (
	"fmt"
	"testing"
)

func procStatLine(pid int, comm string, ppid int, start int64) string {
	return fmt.Sprintf("%d (%s) S %d %d %d 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 %d 1234567 89 18446744073709551615\n", pid, comm, ppid, pid, pid, start)
}

func TestParseProcStat(t *testing.T) {
	cases := []struct {
		line  string
		ppid  int
		start int64
		ok    bool
	}{
		{procStatLine(4321, "game", 1200, 987654), 1200, 987654, true},
		{procStatLine(4321, "my (odd) game) x", 77, 5), 77, 5, true},
		{"4321 (game) S 1200 4321 4321 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 987654", 1200, 987654, true},
		{"4321 (game) S 1200 4321 4321 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0", 0, 0, false},
		{"4321 (game) S x200 4321 4321 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 987654 1", 0, 0, false},
		{"4321 (game) S 1200 4321 4321 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 98a654 1", 0, 0, false},
		{"4321 game S 1200", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tc := range cases {
		ppid, start, ok := parseProcStat([]byte(tc.line))
		if ppid != tc.ppid || start != tc.start || ok != tc.ok {
			t.Errorf("parseProcStat(%q) = %d %d %v, want %d %d %v", tc.line, ppid, start, ok, tc.ppid, tc.start, tc.ok)
		}
	}
}

func TestParseStatusFSUID(t *testing.T) {
	status := "Name:\tgame\nUmask:\t0022\nState:\tS (sleeping)\nTgid:\t4321\nPid:\t4321\nPPid:\t1200\nUid:\t1000\t1001\t1002\t1003\nGid:\t1000\t1000\t1000\t1000\n"
	if uid, ok := parseStatusFSUID([]byte(status)); !ok || uid != 1003 {
		t.Fatalf("fsuid = %d %v, want the fourth Uid value", uid, ok)
	}
	for _, bad := range []string{"Name:\tgame\n", "Uid:\t1000\t1000\n", "Uid:\t1\t2\t3\tx\n", "Uid:\t1\t2\t3\t99999999999\n"} {
		if uid, ok := parseStatusFSUID([]byte(bad)); ok {
			t.Errorf("parseStatusFSUID(%q) = %d", bad, uid)
		}
	}
	if uid, ok := parseStatusFSUID([]byte("Uid:\t0\t0\t0\t0")); !ok || uid != 0 {
		t.Fatalf("last line without newline = %d %v", uid, ok)
	}
}

func TestParseSocketLink(t *testing.T) {
	cases := []struct {
		link string
		ino  uint32
		ok   bool
	}{
		{"socket:[123456]", 123456, true},
		{"socket:[4294967295]", 4294967295, true},
		{"socket:[4294967296]", 0, false},
		{"socket:[]", 0, false},
		{"socket:[12a]", 0, false},
		{"socket:[12", 0, false},
		{"pipe:[123]", 0, false},
		{"/dev/null", 0, false},
		{"anon_inode:[eventfd]", 0, false},
	}
	for _, tc := range cases {
		ino, ok := parseSocketLink([]byte(tc.link))
		if ino != tc.ino || ok != tc.ok {
			t.Errorf("parseSocketLink(%q) = %d %v", tc.link, ino, ok)
		}
	}
}

func TestExeLinkCleaning(t *testing.T) {
	if got := cleanExeLink("/usr/lib/firefox/firefox (deleted)"); got != "/usr/lib/firefox/firefox" {
		t.Fatalf("deleted suffix kept: %q", got)
	}
	if got := cleanExeLink("/opt/app (deleted)/bin/app"); got != "/opt/app (deleted)/bin/app" {
		t.Fatalf("inner text changed: %q", got)
	}
	r := compileIn(t, linuxEnv(), "/opt/app/bin/app")
	if !r.MatchPath(cleanExeLink("/opt/app/bin/app (deleted)")) {
		t.Fatal("upgraded binary no longer matches its rule")
	}
}

func TestFlatpakTranslation(t *testing.T) {
	info := "[Application]\nname=com.spotify.Client\napp-path=/wrong/section\n\n[Instance]\ninstance-id=123\napp-path=/var/lib/flatpak/app/com.spotify.Client/x86_64/stable/abc123/files\nbranch=stable\n"
	appPath := flatpakAppPath([]byte(info))
	if appPath != "/var/lib/flatpak/app/com.spotify.Client/x86_64/stable/abc123/files" {
		t.Fatalf("app-path = %q", appPath)
	}
	for _, bad := range []string{"[Instance]\napp-path=relative/files\n", "[Application]\napp-path=/x\n", "", "[Instance]\nbranch=stable\n"} {
		if got := flatpakAppPath([]byte(bad)); got != "" {
			t.Errorf("flatpakAppPath(%q) = %q", bad, got)
		}
	}
	if got := flatpakAppPath([]byte("[Instance]\napp-path=/var/lib/flatpak/app/x/../y/files/\n")); got != "/var/lib/flatpak/app/y/files" {
		t.Fatalf("app-path not cleaned: %q", got)
	}
	cases := []struct{ exe, appPath, want string }{
		{"/app/bin/spotify", appPath, appPath + "/bin/spotify"},
		{"/app/extra/share/spotify/spotify", appPath, appPath + "/extra/share/spotify/spotify"},
		{"/app/bin/../../etc/x", appPath, "/var/lib/flatpak/app/com.spotify.Client/x86_64/stable/abc123/etc/x"},
		{"/usr/bin/bwrap", appPath, "/usr/bin/bwrap"},
		{"/application/bin/x", appPath, "/application/bin/x"},
		{"/app/bin/spotify", "", "/app/bin/spotify"},
	}
	for _, tc := range cases {
		if got := flatpakMatchPath(tc.exe, tc.appPath); got != tc.want {
			t.Errorf("flatpakMatchPath(%q) = %q, want %q", tc.exe, got, tc.want)
		}
	}
	r := compileIn(t, linuxEnv(), "/var/lib/flatpak/app/com.spotify.Client/")
	if !r.MatchPath(flatpakMatchPath("/app/bin/spotify", appPath)) {
		t.Fatal("translated flatpak image does not match the installation rule")
	}
	if r.MatchPath("/app/bin/spotify") {
		t.Fatal("sandbox-relative path matched")
	}
}

func TestMountFSType(t *testing.T) {
	info := "22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw\n" +
		"30 22 0:40 / /run rw shared:2 - tmpfs tmpfs rw\n" +
		"41 22 0:52 / /home/eve/my\\040mount rw - fuse.sshfs eve@host: rw\n" +
		"42 22 0:53 /flatpak-info /.flatpak-info ro - tmpfs tmpfs ro\n" +
		"43 22 0:54 / /.flatpak-info ro - fuse.evil evil ro\n" +
		"malformed line\n"
	cases := []struct {
		target, want string
	}{
		{"/usr/bin/x", "ext4"},
		{"/run/user/1000/x", "tmpfs"},
		{"/runner/x", "ext4"},
		{"/home/eve/my mount/f", "fuse.sshfs"},
		{"/.flatpak-info", "fuse.evil"},
	}
	for _, tc := range cases {
		if got, ok := mountFSType([]byte(info), tc.target); !ok || got != tc.want {
			t.Errorf("mountFSType(%q) = %q, %v; want %q", tc.target, got, ok, tc.want)
		}
	}
	if _, ok := mountFSType([]byte("garbage\n"), "/x"); ok {
		t.Error("a listing with no mounts reported one")
	}
	for fstype, want := range map[string]bool{
		"fuse": true, "fuseblk": true, "fuse.sshfs": true, "nfs4": true, "cifs": true, "9p": true,
		"ext4": false, "tmpfs": false, "btrfs": false, "overlay": false, "squashfs": false,
	} {
		if got := blockingFSType(fstype); got != want {
			t.Errorf("blockingFSType(%q) = %v, want %v", fstype, got, want)
		}
	}
}
