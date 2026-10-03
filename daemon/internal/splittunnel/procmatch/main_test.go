package procmatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Live tests re-run the test binary (or a renamed copy) as a helper process.
const (
	helperEnv      = "PROCMATCH_TEST_HELPER"
	helperDirEnv   = "PROCMATCH_TEST_DIR"
	helperTCPEnv   = "PROCMATCH_TEST_TCP"
	helperGameEnv  = "PROCMATCH_TEST_GAME"
	helperShareEnv = "PROCMATCH_TEST_UDPSHARE"
	helperTimeout  = 2 * time.Minute
)

type helperInfo struct {
	PID       int
	GamePID   int
	TCPLocal  string
	TCPRemote string
	UDPLocal  string
	UDPShared string
}

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		if err := runHelper(mode, os.Getenv(helperDirEnv)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runHelper(mode, dir string) error {
	switch mode {
	case "game":
		pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer pc.Close()
		conn, err := net.DialTimeout("tcp4", os.Getenv(helperTCPEnv), 10*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		info := helperInfo{PID: os.Getpid(), TCPLocal: conn.LocalAddr().String(), TCPRemote: conn.RemoteAddr().String(), UDPLocal: pc.LocalAddr().String()}
		if port := os.Getenv(helperShareEnv); port != "" {
			lc := net.ListenConfig{Control: reuseAddrControl}
			shared, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:"+port)
			if err != nil {
				return err
			}
			defer shared.Close()
			info.UDPShared = shared.LocalAddr().String()
		}
		if err := writeHelperInfo(dir, "game", info); err != nil {
			return err
		}
		waitHelperSignal(dir, "game")
		return nil
	case "launcher":
		cmd := exec.Command(os.Getenv(helperGameEnv), "-test.run=^$")
		cmd.Env = append(os.Environ(), helperEnv+"=game")
		if err := cmd.Start(); err != nil {
			return err
		}
		if err := writeHelperInfo(dir, "launcher", helperInfo{PID: os.Getpid(), GamePID: cmd.Process.Pid}); err != nil {
			return err
		}
		waitHelperSignal(dir, "launcher")
		return nil
	}
	if h := extraHelpers[mode]; h != nil {
		return h(dir)
	}
	return fmt.Errorf("unknown helper mode %q", mode)
}

// extraHelpers holds OS-specific helper modes registered by tagged test files.
var extraHelpers = map[string]func(dir string) error{}

func writeHelperInfo(dir, name string, info helperInfo) error {
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name+".json"))
}

func waitHelperSignal(dir, name string) {
	deadline := time.Now().Add(helperTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, name+".exit")); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readHelperInfo(t *testing.T, dir, name string) helperInfo {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(dir, name+".json"))
		if err == nil {
			var info helperInfo
			if err := json.Unmarshal(b, &info); err != nil {
				t.Fatalf("%s info: %v", name, err)
			}
			return info
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %v", name, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func signalHelper(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".exit"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// copyTestBinary copies the running test binary to dir/name so helpers run under another image path.
func copyTestBinary(t *testing.T, dir, name string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst := filepath.Join(dir, name)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dst
}

// helperTree starts launcher -> game from renamed copies of the test binary; the game holds a
// TCP connection to a local listener and a UDP socket until the test ends.
type helperTree struct {
	dir          string
	launcherPath string
	gamePath     string
	launcherPID  int
	launcherDone chan error
	game         helperInfo
}

func startHelperTree(t *testing.T, ext string, env ...string) *helperTree {
	t.Helper()
	dir := t.TempDir()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	h := &helperTree{dir: dir, launcherDone: make(chan error, 1)}
	h.launcherPath = copyTestBinary(t, dir, "launcher"+ext)
	h.gamePath = copyTestBinary(t, dir, "game"+ext)
	cmd := exec.Command(h.launcherPath, "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"=launcher", helperDirEnv+"="+dir, helperTCPEnv+"="+ln.Addr().String(), helperGameEnv+"="+h.gamePath)
	cmd.Env = append(cmd.Env, env...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { h.launcherDone <- cmd.Wait() }()
	t.Cleanup(func() {
		os.WriteFile(filepath.Join(dir, "launcher.exit"), nil, 0o600)
		os.WriteFile(filepath.Join(dir, "game.exit"), nil, 0o600)
		cmd.Process.Kill()
		if h.game.PID != 0 {
			if p, err := os.FindProcess(h.game.PID); err == nil {
				p.Kill()
				p.Wait()
			}
		}
		select {
		case <-h.launcherDone:
		case <-time.After(10 * time.Second):
		}
	})
	l := readHelperInfo(t, dir, "launcher")
	h.launcherPID = l.PID
	h.game = readHelperInfo(t, dir, "game")
	if h.game.PID != l.GamePID {
		t.Fatalf("game pid %d, launcher started %d", h.game.PID, l.GamePID)
	}
	return h
}

// stopLauncher makes the launcher exit while the game keeps running.
func (h *helperTree) stopLauncher(t *testing.T) {
	t.Helper()
	signalHelper(t, h.dir, "launcher")
	select {
	case err := <-h.launcherDone:
		h.launcherDone <- err
		if err != nil {
			t.Fatalf("launcher: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("launcher did not exit")
	}
}

func (h *helperTree) flows() []FlowID {
	return []FlowID{
		{Proto: protoTCP, App: netip.MustParseAddrPort(h.game.TCPLocal), Remote: netip.MustParseAddrPort(h.game.TCPRemote)},
		{Proto: protoUDP, App: netip.MustParseAddrPort(h.game.UDPLocal), Remote: netip.MustParseAddrPort("127.0.0.1:9")},
	}
}

type ownSockets struct {
	tcp, udp FlowID
}

func openOwnSockets(t *testing.T) ownSockets {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	conn, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return ownSockets{
		tcp: FlowID{Proto: protoTCP, App: addrPortOf(conn.LocalAddr()), Remote: addrPortOf(conn.RemoteAddr())},
		udp: FlowID{Proto: protoUDP, App: addrPortOf(pc.LocalAddr()), Remote: netip.MustParseAddrPort("127.0.0.1:9")},
	}
}
