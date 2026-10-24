//go:build anytls_live && with_utls

package anytls

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// liveProfile reads a deployed node's AnyTLS block from the environment.
func liveProfile(t *testing.T) state.AnyTLSProfile {
	t.Helper()
	port, _ := strconv.Atoi(os.Getenv("AL_PORT"))
	profile := state.AnyTLSProfile{
		RemoteHost: os.Getenv("AL_NODE"),
		RemotePort: port,
		Password:   os.Getenv("AL_PASSWORD"),
		ServerName: os.Getenv("AL_SNI"),
		PinSHA256:  os.Getenv("AL_PIN"),
	}
	if profile.RemoteHost == "" || port == 0 || profile.Password == "" || profile.PinSHA256 == "" {
		t.Skip("set AL_NODE, AL_PORT, AL_PASSWORD, AL_PIN, AL_SNI")
	}
	return profile
}

// sessionAliveAfterDatagram sends one WireGuard-sized datagram and reports
// whether the node kept the session open; it closes the session on a bad password.
func sessionAliveAfterDatagram(t *testing.T, profile state.AnyTLSProfile) bool {
	t.Helper()
	mgr := NewManager(state.NewLogStore(200))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := mgr.Start(ctx, profile); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	sock, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: mgr.BoundLocalPort()})
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer sock.Close()
	if _, err := sock.Write(make([]byte, 148)); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(5 * time.Second)
	return mgr.Status().Running
}

func TestLiveNodeKeepsAuthenticatedSession(t *testing.T) {
	if !sessionAliveAfterDatagram(t, liveProfile(t)) {
		t.Fatal("the node closed a session using the provisioned password")
	}
}

func TestLiveNodeDropsWrongPassword(t *testing.T) {
	profile := liveProfile(t)
	profile.Password += "-wrong"
	if sessionAliveAfterDatagram(t, profile) {
		t.Fatal("the node kept a session with a wrong password open")
	}
}

func TestLiveNodeRejectsWrongPin(t *testing.T) {
	profile := liveProfile(t)
	profile.PinSHA256 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	mgr := NewManager(state.NewLogStore(200))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := mgr.Start(ctx, profile); err == nil {
		mgr.Stop(context.Background())
		t.Fatal("Start with a wrong pin succeeded")
	}
}
