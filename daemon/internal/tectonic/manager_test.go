package tectonic

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	tect "github.com/pangeavpn/tectonic"
	"github.com/pangeavpn/tectonic/lane"
	"github.com/pangeavpn/tectonic/server"
	"github.com/pangeavpn/tectonic/wire"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// testNode is a real Tectonic server whose "WireGuard" echoes every datagram.
type testNode struct {
	addr   string
	secret []byte
	pin    string
}

func startNode(t *testing.T) *testNode {
	t.Helper()
	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := echo.ReadFromUDP(buf)
			if err != nil {
				return
			}
			echo.WriteToUDP(buf[:n], from)
		}
	}()

	cert := selfSigned(t)
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	n := &testNode{secret: bytes.Repeat([]byte{7}, 32), pin: lane.PinOf(leaf).String()}
	srv, err := server.New(server.Config{
		NodeSecret:         n.secret,
		CoverDomain:        "n1.cover.test",
		OwnCertificate:     func() (*tls.Certificate, error) { return &cert, nil },
		DefaultCertificate: func() (*tls.Certificate, error) { return &cert, nil },
		Cover: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, "<html>hello</html>")
		}),
		WireGuard: echo.LocalAddr().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	n.addr = ln.Addr().String()
	return n
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// profile issues a credential the way the hub does. SNI mode none checks the
// pin, so the test needs no CA.
func (n *testNode) profile(t *testing.T, expiry time.Time) state.TectonicProfile {
	t.Helper()
	id, err := wire.NewKeyID(expiry)
	if err != nil {
		t.Fatal(err)
	}
	dk := wire.DeviceKey(n.secret, id)
	return state.TectonicProfile{
		Node: n.addr, CoverDomain: "n1.cover.test", KeyID: id[:], DeviceKey: dk[:],
		Pins: []string{n.pin}, SNIMode: "none",
	}
}

func newTestManager() *Manager {
	return NewManager(state.NewLogStore(200), func() string { return "test-net" })
}

func TestRelaysDatagramsThroughTheNode(t *testing.T) {
	node := startNode(t)
	m := newTestManager()
	ctx := context.Background()
	if err := m.Start(ctx, node.profile(t, time.Now().Add(24*time.Hour))); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)
	if err := m.WaitForSession(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForSession: %v", err)
	}
	if !m.Status().Running || m.BoundLocalPort() == 0 {
		t.Fatalf("status %+v port %d, want running on a bound port", m.Status(), m.BoundLocalPort())
	}

	wg, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: m.BoundLocalPort()})
	if err != nil {
		t.Fatal(err)
	}
	defer wg.Close()
	for i := 0; i < 20; i++ {
		msg := []byte(strings.Repeat("x", 100+i*50))
		if _, err := wg.Write(msg); err != nil {
			t.Fatal(err)
		}
		wg.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 4096)
		n, err := wg.Read(buf)
		if err != nil {
			t.Fatalf("datagram %d: %v", i, err)
		}
		if !bytes.Equal(buf[:n], msg) {
			t.Fatalf("datagram %d came back changed", i)
		}
	}

	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if m.Status().Running || m.BoundLocalPort() != 0 {
		t.Fatal("still running after Stop")
	}
}

func TestSameProfileKeepsTheSessionAndANewOneReplacesIt(t *testing.T) {
	node := startNode(t)
	m := newTestManager()
	ctx := context.Background()
	p := node.profile(t, time.Now().Add(24*time.Hour))
	if err := m.Start(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)
	port := m.BoundLocalPort()
	if err := m.Start(ctx, *state.CloneTectonicProfile(&p)); err != nil {
		t.Fatal(err)
	}
	if m.BoundLocalPort() != port {
		t.Fatal("an identical profile restarted the session")
	}
	if err := m.Start(ctx, node.profile(t, time.Now().Add(48*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitForSession(ctx, 10*time.Second); err != nil {
		t.Fatalf("new session: %v", err)
	}
}

func TestRejectsBadProfilesWithoutStopping(t *testing.T) {
	node := startNode(t)
	m := newTestManager()
	ctx := context.Background()
	good := node.profile(t, time.Now().Add(24*time.Hour))
	if err := m.Start(ctx, good); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)

	hostname := good
	hostname.Node = "node.example:443"
	expired := node.profile(t, time.Now().Add(-48*time.Hour))
	badMode := good
	badMode.SNIMode = "sideways"
	shortKey := good
	shortKey.DeviceKey = good.DeviceKey[:8]

	for name, p := range map[string]state.TectonicProfile{
		"hostname": hostname, "expired": expired, "sni mode": badMode, "short key": shortKey,
	} {
		err := m.Start(ctx, p)
		if err == nil {
			t.Errorf("%s: Start accepted it", name)
		}
		if name == "expired" && !errors.Is(err, tect.ErrCredentialRejected) {
			t.Errorf("expired: %v, want ErrCredentialRejected", err)
		}
	}
	if err := m.WaitForSession(ctx, 5*time.Second); err != nil {
		t.Fatalf("a rejected profile took down the live session: %v", err)
	}
}

func TestWrongCredentialNeverLogsIn(t *testing.T) {
	node := startNode(t)
	m := newTestManager()
	ctx := context.Background()
	p := node.profile(t, time.Now().Add(24*time.Hour))
	p.DeviceKey = bytes.Repeat([]byte{1}, 32)
	if err := m.Start(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)
	if err := m.WaitForSession(ctx, 3*time.Second); err == nil {
		t.Fatal("a wrong device key logged in")
	}
}

func TestProtectRunsOnEveryLaneSocket(t *testing.T) {
	node := startNode(t)
	calls := make(chan int, 64)
	Protect = func(fd int) bool { calls <- fd; return true }
	defer func() { Protect = nil }()
	m := newTestManager()
	ctx := context.Background()
	if err := m.Start(ctx, node.profile(t, time.Now().Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)
	if err := m.WaitForSession(ctx, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 {
		t.Fatal("no lane socket was offered to Protect")
	}
}

func TestUnprotectedLaneNeverDials(t *testing.T) {
	node := startNode(t)
	Protect = func(int) bool { return false }
	defer func() { Protect = nil }()
	m := newTestManager()
	ctx := context.Background()
	if err := m.Start(ctx, node.profile(t, time.Now().Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)
	if err := m.WaitForSession(ctx, 2*time.Second); err == nil {
		t.Fatal("a lane logged in on a socket Protect refused")
	}
}
