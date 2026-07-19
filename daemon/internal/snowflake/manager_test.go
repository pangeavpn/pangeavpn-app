package snowflake

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	sf "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/client/lib"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// fakeTransport is a snowflakeTransport test double: Dial either returns a
// preset error, or hands back one end of a net.Pipe(), letting tests act as
// the "far end" (proxy/server) of the stream without any real broker,
// WebRTC, or network access.
type fakeTransport struct {
	conn net.Conn
	err  error
}

func (f *fakeTransport) Dial() (net.Conn, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.conn, nil
}

// withFakeTransport swaps newSnowflakeClient for the test duration and
// restores it afterward.
func withFakeTransport(t *testing.T, ft *fakeTransport) {
	t.Helper()
	orig := newSnowflakeClient
	newSnowflakeClient = func(sf.ClientConfig) (snowflakeTransport, error) {
		return ft, nil
	}
	t.Cleanup(func() { newSnowflakeClient = orig })
}

func testProfile() state.SnowflakeProfile {
	return state.SnowflakeProfile{
		LocalPort:         0,
		BrokerURL:         "http://broker.invalid",
		BridgeFingerprint: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
}

func TestStop_WhenNotRunning_NoError(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() on a never-started manager returned error: %v", err)
	}
}

func TestStart_RequiresBrokerURL(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	profile := testProfile()
	profile.BrokerURL = ""
	if err := m.Start(context.Background(), profile); err == nil {
		t.Fatal("expected error when BrokerURL is empty")
	}
	if m.Status().Running {
		t.Fatal("manager should not be running after a rejected Start")
	}
}

func TestStart_RejectsNegativeLocalPort(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	profile := testProfile()
	profile.LocalPort = -1
	if err := m.Start(context.Background(), profile); err == nil {
		t.Fatal("expected error for negative LocalPort")
	}
}

func TestBoundLocalPort_ZeroWhenNotRunning(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	if got := m.BoundLocalPort(); got != 0 {
		t.Fatalf("BoundLocalPort() = %d, want 0 when not running", got)
	}
}

func TestWaitForSession_ErrorsWhenNotRunning(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	if err := m.WaitForSession(context.Background(), time.Second); err == nil {
		t.Fatal("expected error waiting for session on a manager that was never started")
	}
}

// TestStart_DialFailure_StopsRunning proves a failed rendezvous (broker
// unreachable, WebRTC catch failed, etc.) leaves the manager back in a
// not-running state rather than stuck "running" with a dead stream.
func TestStart_DialFailure_StopsRunning(t *testing.T) {
	withFakeTransport(t, &fakeTransport{err: errors.New("no snowflake proxies available")})

	m := NewManager(state.NewLogStore(10))
	if err := m.Start(context.Background(), testProfile()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !m.Status().Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.Status().Running {
		t.Fatal("expected manager to stop running after Dial failure")
	}

	if err := m.WaitForSession(context.Background(), 50*time.Millisecond); err == nil {
		t.Fatal("expected WaitForSession to error once the manager has stopped")
	}
}

// TestStart_BridgesUDPOverStream is the non-e2e proof that Manager's
// UDP<->stream bridge (framing.go's WriteFrame/ReadFrame carried over
// whatever net.Conn snowflakeTransport.Dial returns) actually moves bytes
// end to end, using a net.Pipe() stand-in for the real Snowflake stream
// instead of a broker/proxy/server stack. See e2e_test.go (transport_e2e
// build tag) for the version of this proof against the real library.
func TestStart_BridgesUDPOverStream(t *testing.T) {
	clientEnd, farEnd := net.Pipe()
	withFakeTransport(t, &fakeTransport{conn: clientEnd})

	logs := state.NewLogStore(100)
	m := NewManager(logs)
	if err := m.Start(context.Background(), testProfile()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })

	if err := m.WaitForSession(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("WaitForSession: %v", err)
	}

	port := m.BoundLocalPort()
	if port <= 0 {
		t.Fatalf("BoundLocalPort() = %d, want > 0 after session established", port)
	}

	// Simulate WireGuard: a UDP socket that sends to the manager's loopback
	// port and expects replies back at its own ephemeral source port.
	wgSocket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer wgSocket.Close()

	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	outbound := []byte("wireguard handshake initiation")
	if _, err := wgSocket.WriteToUDP(outbound, target); err != nil {
		t.Fatalf("WriteToUDP: %v", err)
	}

	// Read the framed datagram on the far end of the fake stream, as a
	// snowflake server would.
	farEnd.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := ReadFrame(farEnd)
	if err != nil {
		t.Fatalf("ReadFrame on far end: %v", err)
	}
	if !bytes.Equal(got, outbound) {
		t.Fatalf("far end received %q, want %q", got, outbound)
	}

	// Write a framed reply back; the manager should deliver it to wgSocket.
	reply := []byte("wireguard handshake response")
	if err := WriteFrame(farEnd, reply); err != nil {
		t.Fatalf("WriteFrame on far end: %v", err)
	}

	wgSocket.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := wgSocket.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	if !bytes.Equal(buf[:n], reply) {
		t.Fatalf("wgSocket received %q, want %q", buf[:n], reply)
	}
}

// TestPumpFunctions_DirectRoundTrip exercises pumpUDPToStream/
// pumpStreamToUDP directly (bypassing Manager) for a tighter unit test of
// the framing bridge itself.
func TestPumpFunctions_DirectRoundTrip(t *testing.T) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer udpConn.Close()

	sender, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP sender: %v", err)
	}
	defer sender.Close()

	streamA, streamB := net.Pipe()

	var peer atomic.Pointer[net.UDPAddr]
	errCh := make(chan error, 2)
	go func() { errCh <- pumpUDPToStream(udpConn, streamA, &peer) }()
	go func() { errCh <- pumpStreamToUDP(streamA, udpConn, &peer) }()

	target := udpConn.LocalAddr().(*net.UDPAddr)
	payload := []byte("hello snowflake")
	if _, err := sender.WriteToUDP(payload, target); err != nil {
		t.Fatalf("WriteToUDP: %v", err)
	}

	streamB.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := ReadFrame(streamB)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}

	reply := []byte("hello wireguard")
	if err := WriteFrame(streamB, reply); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	sender.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := sender.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	if !bytes.Equal(buf[:n], reply) {
		t.Fatalf("sender received %q, want %q", buf[:n], reply)
	}

	udpConn.Close()
	streamA.Close()
	streamB.Close()
	<-errCh
	<-errCh
}
