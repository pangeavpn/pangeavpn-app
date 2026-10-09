package reality

import (
	"net"
	"sync/atomic"
	"testing"
)

func wgInitiationPacket() []byte {
	pkt := make([]byte, wireGuardInitiationLen)
	pkt[0] = 1
	return pkt
}

func udpAddr(port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
}

func TestAcceptFromPeer_PinsFirstSourceAndRejectsOthers(t *testing.T) {
	var peer atomic.Pointer[net.UDPAddr]
	data := []byte{4, 0, 0, 0, 9, 9}

	if !acceptFromPeer(&peer, udpAddr(1000), data) {
		t.Fatal("first source must be accepted")
	}
	if acceptFromPeer(&peer, udpAddr(2000), data) {
		t.Fatal("a second source sending data packets must be rejected")
	}
	if !acceptFromPeer(&peer, udpAddr(1000), data) {
		t.Fatal("the pinned source must stay accepted")
	}
}

// A rebuilt WireGuard device opens from a fresh socket with a handshake
// initiation; the bridge must follow it instead of blackholing the session.
func TestAcceptFromPeer_FollowsNewSocketOnHandshakeInitiation(t *testing.T) {
	var peer atomic.Pointer[net.UDPAddr]

	if !acceptFromPeer(&peer, udpAddr(1000), wgInitiationPacket()) {
		t.Fatal("first source must be accepted")
	}
	if !acceptFromPeer(&peer, udpAddr(2000), wgInitiationPacket()) {
		t.Fatal("a handshake initiation from a new socket must re-pin the peer")
	}
	if acceptFromPeer(&peer, udpAddr(1000), []byte{4, 0, 0, 0, 9, 9}) {
		t.Fatal("the old socket's data packets must be rejected after the re-pin")
	}
	if !acceptFromPeer(&peer, udpAddr(2000), []byte{4, 0, 0, 0, 9, 9}) {
		t.Fatal("the re-pinned source must be accepted")
	}
}

func TestAcceptFromPeer_MalformedInitiationDoesNotRePin(t *testing.T) {
	var peer atomic.Pointer[net.UDPAddr]

	if !acceptFromPeer(&peer, udpAddr(1000), wgInitiationPacket()) {
		t.Fatal("first source must be accepted")
	}
	wrongLen := []byte{1, 0, 0, 0}
	if acceptFromPeer(&peer, udpAddr(2000), wrongLen) {
		t.Fatal("a short type-1 packet must not re-pin")
	}
	badReserved := wgInitiationPacket()
	badReserved[1] = 7
	if acceptFromPeer(&peer, udpAddr(2000), badReserved) {
		t.Fatal("a type-1 packet with non-zero reserved bytes must not re-pin")
	}
}
