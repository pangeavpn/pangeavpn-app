//go:build android || ios

package mobile

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
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

// cascadeInFlight claims a start for a freshly keyed config and walks the
// cascade on a test device, as Start does once it has a TUN.
func cascadeInFlight(t *testing.T, candidates ...transport.Candidate) <-chan error {
	t.Helper()
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	peer, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	p := keyedTunnel("A")
	p.serverName = "Alpha"
	p.wgPrivateKeyRaw, p.serverPubKeyRaw = priv.Bytes(), peer.PublicKey().Bytes()
	stagePrepared(p)
	attempt, err := claimStart()
	if err != nil {
		t.Fatalf("claimStart: %v", err)
	}
	mu.Lock()
	logs := wgLogs
	mu.Unlock()
	result := make(chan error, 1)
	go func() {
		defer finishStart(attempt)
		result <- walkCascade(attempt, p.profile, newTestDevice(), newStarter(logs), candidates, logs, "")
	}()
	return result
}

// The cascade dials the per-start profile, which carries NAT64 synthesis, never
// the prepared one, which must stay as the hub described it.
func TestTheCascadeStartsTransportsOnTheProfileItWasGiven(t *testing.T) {
	initForTest(t, nil)
	recordPushes()
	p := keyedTunnel("A")
	stagePrepared(p)
	attempt, err := claimStart()
	if err != nil {
		t.Fatalf("claimStart: %v", err)
	}
	defer finishStart(attempt)
	dialed := &state.Profile{ID: "synthesized"}
	var started []*state.Profile
	refuse := func(_ context.Context, profile *state.Profile, _ *state.WireGuardProfile) error {
		started = append(started, profile)
		return errors.New("refused")
	}
	candidates := []transport.Candidate{{Kind: "reality", Start: refuse}, {Kind: "cloak", Start: refuse}}
	mu.Lock()
	logs := wgLogs
	mu.Unlock()

	if err := walkCascade(attempt, dialed, newTestDevice(), newStarter(logs), candidates, logs, ""); err == nil {
		t.Fatal("a cascade of refusing transports succeeded")
	}
	if len(started) != 2 || started[0] != dialed || started[1] != dialed {
		t.Fatalf("transports started on %v, want both on the given profile %p", started, dialed)
	}
}

func cascadeResult(t *testing.T, result <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(within):
		t.Fatalf("the cascade was still running %v after the stop", within)
		return nil
	}
}

func TestAStopCancelsAStartStillInTheCascade(t *testing.T) {
	for name, stop := range map[string]func(){"Stop": Stop, "StopForSwitch": StopForSwitch, "Wipe": func() { _ = Wipe() }} {
		t.Run(name, func(t *testing.T) {
			initForTest(t, nil)
			log := recordPushes()
			entered := make(chan struct{})
			hang := transport.Candidate{Kind: "cloak", Start: func(ctx context.Context, _ *state.Profile, _ *state.WireGuardProfile) error {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}}
			next := transport.Candidate{Kind: "reality", Start: func(context.Context, *state.Profile, *state.WireGuardProfile) error {
				t.Error("a cancelled start went on to the next transport")
				return errors.New("unreachable")
			}}
			result := cascadeInFlight(t, hang, next)
			<-entered

			stop()
			if err := cascadeResult(t, result, time.Second); !errors.Is(err, errStartCancelled) {
				t.Fatalf("cascade = %v, want it cancelled", err)
			}
			mu.Lock()
			active, pending := activeTunnel, starting
			mu.Unlock()
			if active != nil || pending != nil {
				t.Fatalf("active=%v starting=%v, want neither after the stop", active, pending)
			}
			pushes := log.all()
			for _, push := range pushes {
				if push.State == string(state.StateError) || push.State == string(state.StateConnected) {
					t.Fatalf("a cancelled start pushed %+v", push)
				}
			}
			last := pushes[len(pushes)-1]
			switch name {
			case "StopForSwitch":
				if last.State != string(state.StateConnecting) || last.Detail != "switching server" {
					t.Fatalf("last push = %+v, want connecting to switch", last)
				}
			default:
				if last.State != string(state.StateDisconnected) {
					t.Fatalf("last push = %+v, want disconnected", last)
				}
			}
		})
	}
}

func TestAStopCancelsAStartWaitingForItsHandshake(t *testing.T) {
	initForTest(t, nil)
	recordPushes()
	started := make(chan struct{})
	up := transport.Candidate{Kind: "cloak", Start: func(context.Context, *state.Profile, *state.WireGuardProfile) error {
		close(started)
		return nil
	}}
	result := cascadeInFlight(t, up)
	<-started

	Stop()
	if err := cascadeResult(t, result, 3*time.Second); !errors.Is(err, errStartCancelled) {
		t.Fatalf("cascade = %v, want it cancelled well before the handshake timeout", err)
	}
}

func TestACancelledStartNeverGoesLive(t *testing.T) {
	initForTest(t, nil)
	log := recordPushes()
	stagePrepared(keyedTunnel("A"))
	attempt, err := claimStart()
	if err != nil {
		t.Fatalf("claimStart: %v", err)
	}
	defer finishStart(attempt)
	detachTunnel()

	dev := newTestDevice()
	defer dev.Close()
	if goLive(attempt, dev, newStarter(nil), statusJSON{ServerID: "A"}, false, "cloak") {
		t.Fatal("a cancelled start went live")
	}
	mu.Lock()
	active := activeTunnel
	mu.Unlock()
	if active != nil || len(log.all()) != 0 {
		t.Fatalf("active=%v pushes=%+v, want nothing published", active, log.all())
	}
}

// Transports were started under the attempt's context, so it must outlive
// Start for as long as the tunnel runs, and end with it.
func TestALiveTunnelKeepsItsStartContextUntilStopped(t *testing.T) {
	initForTest(t, nil)
	recordPushes()
	stagePrepared(keyedTunnel("A"))
	attempt, err := claimStart()
	if err != nil {
		t.Fatalf("claimStart: %v", err)
	}
	if !goLive(attempt, newTestDevice(), newStarter(nil), statusJSON{ServerID: "A"}, false, "cloak") {
		t.Fatal("a live attempt did not go live")
	}
	finishStart(attempt)
	if attempt.ctx.Err() != nil {
		t.Fatal("finishing a successful start cancelled the context its transports run under")
	}

	Stop()
	if attempt.ctx.Err() == nil {
		t.Fatal("stopping the tunnel left its start context running")
	}
}

func TestStartRefusesWhileAnotherIsStarting(t *testing.T) {
	initForTest(t, nil)
	stagePrepared(keyedTunnel("A"))
	attempt, err := claimStart()
	if err != nil {
		t.Fatalf("claimStart: %v", err)
	}
	defer finishStart(attempt)

	fd := pipeFD(t)
	if err := Start(fd); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("Start = %v, want it refused while another start runs", err)
	}
	checkHostFDAfterRefusedStart(t, fd)
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
