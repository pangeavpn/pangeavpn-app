package api

import (
	"context"
	"errors"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// raceProfile carries reality, cloak, naive and anytls, so the race has
// transports to dial ahead and a lazy one (cloak) between them.
func raceProfile() state.Profile {
	profile := transportMemoryProfile()
	profile.AnyTLS = &state.AnyTLSProfile{RemoteHost: "at.example.com", RemotePort: 8443, Password: "p"}
	return profile
}

func realityRunning(m *fakeRealityManager) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func anytlsRunning(m *fakeAnyTLSManager) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// With every transport healthy the race still lands on the top of the cascade,
// and whatever it dialled ahead is shut again.
func TestConnect_Race_TopRankedWinsAndLosersAreStopped(t *testing.T) {
	cloak := &fakeCloakManager{}
	reality := &fakeRealityManager{}
	anytls := &fakeAnyTLSManager{}
	svc := newTestServiceFull(t, cloak, &fakeNaiveManager{}, reality,
		&fakeHysteria2Manager{}, &fakeShadowsocksManager{}, anytls, &fakeSnowflakeManager{},
		&fakeWGManager{}, &fakeKillSwitch{}, raceProfile())

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if got := svc.Status(context.Background()).ActiveTransport; got != "reality" {
		t.Fatalf("ActiveTransport = %q, want reality", got)
	}
	if !realityRunning(reality) {
		t.Fatal("the winning transport was stopped")
	}
	if anytlsRunning(anytls) {
		t.Fatal("anytls was dialled ahead and left running after reality won")
	}
	if cloak.startCalled {
		t.Fatal("cloak dials lazily, so the race must not start it ahead of its turn")
	}
}

// A failed leader hands over in cascade order: cloak (lazy) is next and wins, and
// anytls, dialled ahead meanwhile, is stopped.
func TestConnect_Race_FallsBackInCascadeOrder(t *testing.T) {
	cloak := &fakeCloakManager{}
	reality := &fakeRealityManager{startErr: errors.New("reality boom")}
	anytls := &fakeAnyTLSManager{}
	profile := raceProfile()
	profile.Naive = nil
	svc := newTestServiceFull(t, cloak, &fakeNaiveManager{}, reality,
		&fakeHysteria2Manager{}, &fakeShadowsocksManager{}, anytls, &fakeSnowflakeManager{},
		&fakeWGManager{}, &fakeKillSwitch{}, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if got := svc.Status(context.Background()).ActiveTransport; got != "cloak" {
		t.Fatalf("ActiveTransport = %q, want cloak", got)
	}
	if anytlsRunning(anytls) {
		t.Fatal("anytls was left running after cloak won")
	}
}

func TestConnect_Race_RememberedTransportStillWins(t *testing.T) {
	reality := &fakeRealityManager{}
	naive := &fakeNaiveManager{}
	svc := newTestServiceWithReality(t, &fakeCloakManager{}, naive, reality, &fakeWGManager{}, &fakeKillSwitch{}, transportMemoryProfile())
	svc.transportMemory = &fakeTransportMemory{entries: map[string]string{"wifi-home": "naive"}}
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if got := svc.Status(context.Background()).ActiveTransport; got != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive (remembered)", got)
	}
	if realityRunning(reality) {
		t.Fatal("reality was dialled ahead and left running after naive won")
	}
}

// Racing is an auto-mode behaviour: a pinned transport never dials anything else.
func TestConnect_Race_PinnedTransportDialsNothingElse(t *testing.T) {
	reality := &fakeRealityManager{}
	anytls := &fakeAnyTLSManager{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, reality,
		&fakeHysteria2Manager{}, &fakeShadowsocksManager{}, anytls, &fakeSnowflakeManager{},
		&fakeWGManager{}, &fakeKillSwitch{}, raceProfile())

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "anytls"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	reality.mu.Lock()
	realityStarted := reality.startCalled
	reality.mu.Unlock()
	if realityStarted {
		t.Fatal("a pinned transport must not race the rest of the cascade")
	}
}

func TestConnect_Race_ExhaustedReportsEveryFailure(t *testing.T) {
	svc, _ := cascadeTestService(t, map[string]bool{})

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})
	if !errors.Is(err, ErrTransportExhausted) {
		t.Fatalf("Connect error = %v, want ErrTransportExhausted", err)
	}
	var failures cascadeFailures
	if !errors.As(err, &failures) || len(failures) != 3 {
		t.Fatalf("want one failure per candidate (reality, cloak, shadowsocks), got %v", err)
	}
}

// The setting survives a daemon restart along with the rest of the session.
func TestSessionRecord_KeepsDisableTransportRace(t *testing.T) {
	svc, _ := cascadeTestService(t, map[string]bool{"reality": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{DisableTransportRace: true}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if !svc.getSessionOpts().DisableTransportRace {
		t.Fatal("session options dropped DisableTransportRace")
	}
}
