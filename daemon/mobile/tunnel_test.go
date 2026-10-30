//go:build android || ios

package mobile

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// pushLog records every status push, in order.
type pushLog struct {
	mu     sync.Mutex
	pushes []statusJSON
}

func (l *pushLog) OnStatus(raw string) {
	var status statusJSON
	_ = json.Unmarshal([]byte(raw), &status)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pushes = append(l.pushes, status)
}

func (l *pushLog) all() []statusJSON {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]statusJSON(nil), l.pushes...)
}

func recordPushes() *pushLog {
	log := &pushLog{}
	mu.Lock()
	sink = log
	mu.Unlock()
	return log
}

// stagePrepared installs p as if Prepare had just run.
func stagePrepared(p *preparedTunnel) {
	mu.Lock()
	prepared = p
	mu.Unlock()
}

func TestBuildUAPIIsAcceptedByWireGuard(t *testing.T) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	peer, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	dev := newTestDevice()
	defer dev.Close()

	psk := bytes.Repeat([]byte{7}, 32)
	if err := dev.IpcSet(buildUAPI(priv.Bytes(), peer.PublicKey().Bytes(), psk, 51820)); err != nil {
		t.Fatalf("IpcSet: %v", err)
	}
	uapi, err := dev.IpcGet()
	if err != nil || !strings.Contains(uapi, "preshared_key="+strings.Repeat("07", 32)) {
		t.Fatalf("device did not take the pre-shared key (%v):\n%s", err, uapi)
	}
}

func TestStopForSwitchNeverReportsDisconnected(t *testing.T) {
	initForTest(t, nil)
	log := recordPushes()
	next := &preparedTunnel{serverID: "B", serverName: "Bravo", profile: &state.Profile{},
		entryServerID: "C", entryServerName: "Charlie"}
	stagePrepared(next)
	mu.Lock()
	activeTunnel = &tunnelRuntime{dev: newTestDevice(), serverID: "A", serverName: "Alpha"}
	mu.Unlock()

	StopForSwitch()

	pushes := log.all()
	for _, push := range pushes {
		if push.State == string(state.StateDisconnected) || push.State == string(state.StateDisconnecting) {
			t.Fatalf("StopForSwitch pushed %+v", push)
		}
	}
	if len(pushes) != 1 {
		t.Fatalf("got %d pushes, want one: %+v", len(pushes), pushes)
	}
	want := statusJSON{State: string(state.StateConnecting), Detail: "switching server",
		ServerID: "B", ServerName: "Bravo", EntryServerID: "C", EntryServerName: "Charlie"}
	if pushes[0] != want {
		t.Fatalf("got %+v, want %+v", pushes[0], want)
	}
	mu.Lock()
	gotActive, gotPrepared := activeTunnel, prepared
	mu.Unlock()
	if gotActive != nil || gotPrepared != next {
		t.Fatalf("after StopForSwitch: active=%v prepared kept=%v", gotActive, gotPrepared == next)
	}
}

func TestStopForSwitchFallsBackToTheRunningServer(t *testing.T) {
	initForTest(t, nil)
	log := recordPushes()
	mu.Lock()
	activeTunnel = &tunnelRuntime{dev: newTestDevice(), serverID: "A", serverName: "Alpha", entryServerID: "C"}
	mu.Unlock()

	StopForSwitch()

	pushes := log.all()
	if len(pushes) != 1 || pushes[0].ServerID != "A" || pushes[0].EntryServerID != "C" ||
		pushes[0].State != string(state.StateConnecting) {
		t.Fatalf("got %+v, want one connecting push for A via C", pushes)
	}
}

func TestStopForSwitchWithNothingRunningIsSilent(t *testing.T) {
	initForTest(t, nil)
	log := recordPushes()
	StopForSwitch()
	if pushes := log.all(); len(pushes) != 0 {
		t.Fatalf("got %+v, want no pushes", pushes)
	}
}

func TestStopStillReportsDisconnected(t *testing.T) {
	initForTest(t, nil)
	log := recordPushes()
	mu.Lock()
	activeTunnel = &tunnelRuntime{dev: newTestDevice(), serverID: "A", serverName: "Alpha"}
	mu.Unlock()

	Stop()

	pushes := log.all()
	if len(pushes) != 2 || pushes[0].State != string(state.StateDisconnecting) || pushes[0].ServerID != "A" ||
		pushes[1] != (statusJSON{State: string(state.StateDisconnected)}) {
		t.Fatalf("got %+v, want disconnecting for A then a bare disconnected", pushes)
	}
}

func TestStartAfterStopForSwitchUsesThePreparedServer(t *testing.T) {
	initForTest(t, nil)
	log := recordPushes()
	next := &preparedTunnel{serverID: "B", serverName: "Bravo", profile: &state.Profile{},
		entryServerID: "C", entryServerName: "Charlie"}
	stagePrepared(next)
	mu.Lock()
	activeTunnel = &tunnelRuntime{dev: newTestDevice(), serverID: "A", serverName: "Alpha"}
	mu.Unlock()
	StopForSwitch()

	// This profile has no REALITY config, so Start stops at selection, before any tun device.
	SetPreferredTransport("reality")
	fd := pipeFD(t)
	if err := Start(fd); err == nil || !strings.Contains(err.Error(), "no reality configuration") {
		t.Fatalf("Start after StopForSwitch = %v, want it to reach transport selection", err)
	}
	checkHostFDAfterRefusedStart(t, fd)

	pushes := log.all()
	last := pushes[len(pushes)-1]
	if last.State != string(state.StateError) || last.ServerID != "B" || last.EntryServerID != "C" {
		t.Fatalf("last push = %+v, want an error for B via C", last)
	}
}

func TestStateCarriesLivenessFields(t *testing.T) {
	initForTest(t, nil)
	raw := State()
	for _, key := range []string{`"lastHandshake":0`, `"postQuantum":false`} {
		if !strings.Contains(raw, key) {
			t.Fatalf("State() = %s, want %s", raw, key)
		}
	}

	dev := newTestDevice()
	defer dev.Close()
	mu.Lock()
	activeTunnel = &tunnelRuntime{dev: dev, serverID: "A", postQuantum: true}
	mu.Unlock()
	var status statusJSON
	if err := json.Unmarshal([]byte(State()), &status); err != nil {
		t.Fatalf("decode State(): %v", err)
	}
	if !status.PostQuantum || status.LastHandshake != 0 {
		t.Fatalf("State() = %+v, want postQuantum and no handshake yet", status)
	}
}

func TestMultihopEntriesExport(t *testing.T) {
	initForTest(t, nil)
	if got := MultihopEntries("eu-west-1"); got != "[]" {
		t.Fatalf("with no servers got %s, want []", got)
	}
	load := 10
	mu.Lock()
	servers = []serverInfo{{ID: "eu-west-1", Multihop: true}, {ID: "us-east-1", Multihop: true}, {ID: "sg-1", Multihop: true, Load: &load}}
	mu.Unlock()
	if got := MultihopEntries("eu-west-1"); got != `["sg-1","us-east-1"]` {
		t.Fatalf("got %s, want the lightest entry first", got)
	}
}
