package anytls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(state.NewLogStore(100))
}

func TestManager_StopWhenNotRunning(t *testing.T) {
	mgr := testManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop before Start = %v, want nil", err)
	}
	if got := mgr.BoundLocalPort(); got != 0 {
		t.Fatalf("BoundLocalPort() = %d, want 0 when not running", got)
	}
	if status := mgr.Status(); status.Running {
		t.Fatal("Status().Running = true before any Start")
	}
	if err := mgr.WaitForSession(ctx, 0); err == nil {
		t.Fatal("WaitForSession should error when not running")
	}
}

func TestManager_StartRejectsInvalidProfile(t *testing.T) {
	mgr := testManager(t)
	profile := validProfile()
	profile.Password = ""

	err := mgr.Start(context.Background(), profile)
	if err == nil {
		t.Fatal("Start with no password = nil, want a validation error")
	}
	if !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("Start error = %v, want the password validation error", err)
	}
	if mgr.Status().Running {
		t.Fatal("a rejected Start must leave the manager stopped")
	}
}

// Unlike Shadowsocks, Start dials the node: a closed port is a Start failure,
// which is what lets the cascade move on without waiting for a handshake timeout.
func TestManager_StartFailsWhenNothingListens(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	mgr := testManager(t)
	profile := validProfile()
	profile.RemoteHost, profile.RemotePort = "127.0.0.1", port
	profile.ServerName = "127.0.0.1"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = mgr.Start(ctx, profile)
	if err == nil {
		mgr.Stop(context.Background())
		t.Fatal("Start against a closed port = nil, want a dial error")
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("Start error = %v, want it labelled as a handshake failure", err)
	}
	if mgr.Status().Running {
		t.Fatal("a failed Start must leave the manager stopped")
	}
}

// Disconnect (and a user op preempting a rebuild) cancel the caller's context;
// the dial must follow it instead of running out its own 10s budget.
func TestManager_StartCallerCancelAbortsHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close() // never answers the ClientHello
		}
	}()

	profile := validProfile()
	profile.RemoteHost, profile.RemotePort = "127.0.0.1", listener.Addr().(*net.TCPAddr).Port
	profile.ServerName = "127.0.0.1"

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	mgr := testManager(t)
	started := time.Now()
	err = mgr.Start(ctx, profile)
	took := time.Since(started)
	if err == nil {
		mgr.Stop(context.Background())
		t.Fatal("Start succeeded against a server that never answers")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want it to unwrap to context.Canceled", err)
	}
	if took > 3*time.Second {
		t.Fatalf("Start ignored the cancelled context and took %s", took)
	}
	if mgr.Status().Running {
		t.Fatal("an aborted Start must leave the manager stopped")
	}
}

func TestAnnotateHandshakeErrorExplainsEOF(t *testing.T) {
	annotated := annotateHandshakeError(io.EOF)
	if !errors.Is(annotated, io.EOF) {
		t.Fatal("annotated error must still unwrap to io.EOF")
	}
	if !strings.Contains(annotated.Error(), "password") {
		t.Fatalf("annotated EOF %q should name the credentials to check", annotated.Error())
	}
	wrapped := annotateHandshakeError(fmt.Errorf("dial tcp: %w", io.EOF))
	if !strings.Contains(wrapped.Error(), "AnyTLS session") {
		t.Fatalf("wrapped EOF %q should be annotated", wrapped.Error())
	}
	if got := annotateHandshakeError(errors.New("connection refused")); got.Error() != "connection refused" {
		t.Fatalf("non-EOF error changed: %q", got.Error())
	}
	if annotateHandshakeError(nil) != nil {
		t.Fatal("nil must pass through as nil")
	}
}

func TestDestinationUDPAddr(t *testing.T) {
	addr := destinationUDPAddr("127.0.0.1", 51820)
	udp, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("destinationUDPAddr(literal) = %T, want *net.UDPAddr", addr)
	}
	if udp.Port != 51820 || !udp.IP.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("destinationUDPAddr() = %v, want 127.0.0.1:51820", udp)
	}

	// A hostname stays unresolved: the node resolves it, and the client must
	// not emit a DNS query a Lockdown lock would block anyway.
	hostAddr := destinationUDPAddr("wg.internal", 51820)
	socksaddr, ok := hostAddr.(M.Socksaddr)
	if !ok {
		t.Fatalf("destinationUDPAddr(hostname) = %T, want M.Socksaddr", hostAddr)
	}
	if socksaddr.Fqdn != "wg.internal" || socksaddr.Port != 51820 {
		t.Fatalf("destinationUDPAddr() = %+v, want wg.internal:51820", socksaddr)
	}
}
