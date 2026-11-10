//go:build android || ios

package mobile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// fakeRaceManager records whether its racer was stopped.
type fakeRaceManager struct {
	mu      sync.Mutex
	stopped bool
}

func (m *fakeRaceManager) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	return nil
}

func (m *fakeRaceManager) wasStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// upRacer starts at once on a loopback port that nothing answers, so its
// handshake check can only end by a stop or the timeout.
func upRacer(kind string, mgr *fakeRaceManager, started chan<- string) racer {
	return racer{kind: kind, start: func(context.Context) (*starter, error) {
		if started != nil {
			started <- kind
		}
		rs := newStarter(nil)
		rs.record(kind, mgr, 1)
		return rs, nil
	}}
}

func refusingRacer(kind string, started chan<- string) racer {
	return racer{kind: kind, start: func(context.Context) (*starter, error) {
		if started != nil {
			started <- kind
		}
		return nil, errors.New(kind + " refused")
	}}
}

// raceInFlight runs raceCascade on a claimed start, as Start does once it has a TUN.
func raceInFlight(t *testing.T, racers ...racer) <-chan error {
	t.Helper()
	p := keyedTunnel("A")
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
		result <- raceCascade(attempt, p.profile, newTestDevice(), racers, logs, "")
	}()
	return result
}

func TestARaceOfRefusingTransportsReportsEachFailure(t *testing.T) {
	initForTest(t, nil)
	log := recordPushes()
	result := raceInFlight(t, refusingRacer("reality", nil), refusingRacer("hysteria2", nil), refusingRacer("anytls", nil))

	err := cascadeResult(t, result, 5*time.Second)
	if err == nil {
		t.Fatal("a race of refusing transports succeeded")
	}
	for _, kind := range []string{"reality", "hysteria2", "anytls"} {
		if !strings.Contains(err.Error(), kind+" refused") {
			t.Fatalf("error %q is missing %s's failure", err, kind)
		}
	}
	pushes := log.all()
	if last := pushes[len(pushes)-1]; last.State != string(state.StateError) {
		t.Fatalf("last push = %+v, want an error", last)
	}
}

// A stop during the handshake check cancels the race, and every transport it
// dialled, the one being checked and those dialled ahead, is shut.
func TestAStopCancelsARaceAndStopsEveryRacer(t *testing.T) {
	initForTest(t, nil)
	recordPushes()
	started := make(chan string, 3)
	managers := []*fakeRaceManager{{}, {}, {}}
	result := raceInFlight(t,
		upRacer("reality", managers[0], started),
		upRacer("hysteria2", managers[1], started),
		upRacer("anytls", managers[2], started))
	// Up to the platform cap of racers hold a connection at once.
	dialled := map[string]bool{}
	for range raceMaxInFlight {
		select {
		case kind := <-started:
			dialled[kind] = true
		case <-time.After(3 * time.Second):
			t.Fatal("the race did not dial ahead")
		}
	}

	Stop()
	if err := cascadeResult(t, result, 3*time.Second); !errors.Is(err, errStartCancelled) {
		t.Fatalf("race = %v, want it cancelled", err)
	}
	for i, kind := range []string{"reality", "hysteria2", "anytls"} {
		if dialled[kind] && !managers[i].wasStopped() {
			t.Fatalf("%s was left running after the stop", kind)
		}
	}
}

// Cloak dials lazily, so the race starts it only at its turn: after the
// transport ranked above it has failed.
func TestARaceStartsALazyTransportOnlyAtItsTurn(t *testing.T) {
	initForTest(t, nil)
	recordPushes()
	started := make(chan string, 3)
	result := raceInFlight(t,
		refusingRacer("reality", started),
		refusingRacer("cloak", started),
		refusingRacer("anytls", started))
	if err := cascadeResult(t, result, 5*time.Second); err == nil {
		t.Fatal("a race of refusing transports succeeded")
	}
	close(started)
	var order []string
	for kind := range started {
		order = append(order, kind)
	}
	for i, kind := range order {
		if kind == "cloak" {
			if i == 0 || order[0] != "reality" {
				t.Fatalf("cloak started before reality failed: %v", order)
			}
			return
		}
	}
	t.Fatalf("cloak never started: %v", order)
}
