package procmatch

import (
	"net/netip"
	"strings"
	"testing"
)

const tcpStateEstablished = 5

var (
	tunIP   = netip.MustParseAddr("10.64.0.2")
	app     = netip.AddrPortFrom(tunIP, 50000)
	remote  = netip.MustParseAddrPort("203.0.113.9:443")
	remote2 = netip.MustParseAddrPort("203.0.113.9:8443")
	anyIP   = netip.IPv4Unspecified()
	physIP  = netip.MustParseAddr("192.168.1.20")
)

func TestTCPOwnerAmbiguity(t *testing.T) {
	synSent := uint32(3)
	cases := []struct {
		name   string
		rows   []tcpRow
		remote netip.AddrPort
		pid    int
		ok     bool
	}{
		{"single", []tcpRow{{synSent, app, remote, 100}}, remote, 100, true},
		{"none", nil, remote, 0, false},
		{"other remote", []tcpRow{{synSent, app, remote2, 100}}, remote, 0, false},
		{"other local port", []tcpRow{{synSent, netip.AddrPortFrom(tunIP, 50001), remote, 100}}, remote, 0, false},
		{"other local addr", []tcpRow{{synSent, netip.AddrPortFrom(physIP, 50000), remote, 100}}, remote, 0, false},
		{"listen and time-wait ignored", []tcpRow{
			{tcpStateListen, app, netip.AddrPortFrom(anyIP, 0), 300},
			{tcpStateTimeWait, app, remote, 0},
			{synSent, app, remote, 100},
		}, remote, 100, true},
		{"differing owners", []tcpRow{{synSent, app, remote, 100}, {tcpStateEstablished, app, remote, 200}}, remote, 0, false},
		{"duplicate same owner", []tcpRow{{synSent, app, remote, 100}, {tcpStateEstablished, app, remote, 100}}, remote, 100, true},
		{"any remote, one owner", []tcpRow{{synSent, app, remote, 100}, {tcpStateEstablished, app, remote2, 100}}, netip.AddrPort{}, 100, true},
		{"any remote, two owners", []tcpRow{{synSent, app, remote, 100}, {tcpStateEstablished, app, remote2, 200}}, netip.AddrPort{}, 0, false},
	}
	for _, tc := range cases {
		pid, ok := tcpOwner(tc.rows, app, tc.remote)
		if pid != tc.pid || ok != tc.ok {
			t.Errorf("%s: got %d %v, want %d %v", tc.name, pid, ok, tc.pid, tc.ok)
		}
	}
}

func TestUDPOwnerAmbiguity(t *testing.T) {
	wild := netip.AddrPortFrom(anyIP, app.Port())
	cases := []struct {
		name string
		rows []udpRow
		pid  int
		ok   bool
	}{
		{"exact", []udpRow{{app, 100}}, 100, true},
		{"wildcard", []udpRow{{wild, 100}}, 100, true},
		{"none", nil, 0, false},
		{"other port", []udpRow{{netip.AddrPortFrom(tunIP, 50001), 100}}, 0, false},
		{"other address", []udpRow{{netip.AddrPortFrom(physIP, app.Port()), 100}}, 0, false},
		{"other address ignored", []udpRow{{netip.AddrPortFrom(physIP, app.Port()), 200}, {wild, 100}}, 100, true},
		{"exact and wildcard, one owner", []udpRow{{app, 100}, {wild, 100}}, 100, true},
		{"exact and wildcard, two owners", []udpRow{{wild, 200}, {app, 100}}, 0, false},
		{"two wildcards, two owners", []udpRow{{wild, 100}, {wild, 200}}, 0, false},
		{"two exact, two owners", []udpRow{{app, 100}, {app, 200}}, 0, false},
	}
	for _, tc := range cases {
		pid, ok := udpOwner(tc.rows, app)
		if pid != tc.pid || ok != tc.ok {
			t.Errorf("%s: got %d %v, want %d %v", tc.name, pid, ok, tc.pid, tc.ok)
		}
	}
}

func TestFlowOwnerProtocols(t *testing.T) {
	tcp := []tcpRow{{3, app, remote, 100}}
	udp := []udpRow{{app, 200}}
	if pid, ok := flowOwner(tcp, udp, FlowID{Proto: protoTCP, App: app, Remote: remote}); !ok || pid != 100 {
		t.Fatalf("tcp owner = %d %v", pid, ok)
	}
	if pid, ok := flowOwner(tcp, udp, FlowID{Proto: protoUDP, App: app, Remote: remote}); !ok || pid != 200 {
		t.Fatalf("udp owner = %d %v", pid, ok)
	}
	if _, ok := flowOwner(tcp, udp, FlowID{Proto: 1, App: app, Remote: remote}); ok {
		t.Fatal("icmp flow got an owner")
	}
}

func TestNTToDos(t *testing.T) {
	devices := map[string]string{
		`\device\harddiskvolume1`:  "c:",
		`\device\harddiskvolume10`: "d:",
		`\device\vboxminirdr\;e:`:  "e:",
	}
	cases := []struct {
		nt, want string
		ok       bool
	}{
		{`\Device\HarddiskVolume1\Program Files\Game\game.exe`, `c:\program files\game\game.exe`, true},
		{`\Device\HarddiskVolume10\Games\x.exe`, `d:\games\x.exe`, true},
		{`\Device\HarddiskVolume2\x.exe`, "", false},
		{`\Device\HarddiskVolume1`, "", false},
		{`Registry`, "", false},
		{``, "", false},
	}
	for _, tc := range cases {
		got, ok := ntToDos(devices, tc.nt)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ntToDos(%q) = %q %v, want %q %v", tc.nt, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLogLimiter(t *testing.T) {
	var lines []string
	logf := func(format string, args ...any) { lines = append(lines, format) }
	var l logLimiter
	for i := 0; i < 100; i++ {
		l.logf(logf, "a", "class a")
		l.logf(logf, "b", "class b")
	}
	l.logf(nil, "c", "dropped")
	if strings.Join(lines, ",") != "class a,class b" {
		t.Fatalf("lines = %q, want one per class", lines)
	}
}
