package transport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeCandidate scripts one candidate: how long its dial takes and whether the
// dial and the WireGuard check pass.
type fakeCandidate struct {
	lazy      bool
	dialDelay time.Duration
	dialErr   error
	verifyErr error
	// blockDial makes the dial hang until its context is cancelled.
	blockDial bool
}

type raceLog struct {
	mu        sync.Mutex
	started   []int
	verified  []int
	discarded []int
	verifying int
	overlap   bool
}

func (l *raceLog) add(list *[]int, i int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	*list = append(*list, i)
}

func testRaceConfig() RaceConfig {
	return RaceConfig{Stagger: 20 * time.Millisecond, Grace: 150 * time.Millisecond, MaxInFlight: 3}
}

func runFakeRace(t *testing.T, ctx context.Context, cfg RaceConfig, cands []fakeCandidate) (RaceResult[int], error, *raceLog) {
	t.Helper()
	log := &raceLog{}
	funcs := RaceFuncs[int]{
		Lazy: func(i int) bool { return cands[i].lazy },
		Start: func(ctx context.Context, i int) (int, error) {
			log.add(&log.started, i)
			c := cands[i]
			if c.blockDial {
				<-ctx.Done()
				return 0, ctx.Err()
			}
			select {
			case <-time.After(c.dialDelay):
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			if c.dialErr != nil {
				return 0, c.dialErr
			}
			return 100 + i, nil
		},
		Verify: func(ctx context.Context, i int, v int) error {
			log.mu.Lock()
			log.verifying++
			if log.verifying > 1 {
				log.overlap = true
			}
			log.verified = append(log.verified, i)
			log.mu.Unlock()
			defer func() {
				log.mu.Lock()
				log.verifying--
				log.mu.Unlock()
			}()
			if v != 100+i {
				return errors.New("verify got the wrong value")
			}
			time.Sleep(5 * time.Millisecond)
			return cands[i].verifyErr
		},
		Discard: func(i int, _ int) { log.add(&log.discarded, i) },
	}
	res, err := Race(ctx, len(cands), cfg, funcs)
	return res, err, log
}

func TestRacePrefersTopRankedWhenItConnects(t *testing.T) {
	res, err, log := runFakeRace(t, context.Background(), testRaceConfig(), []fakeCandidate{
		{dialDelay: 40 * time.Millisecond},
		{dialDelay: 1 * time.Millisecond},
		{dialDelay: 1 * time.Millisecond},
	})
	if err != nil || res.Winner != 0 || res.Value != 100 {
		t.Fatalf("want candidate 0, got winner=%d value=%d err=%v", res.Winner, res.Value, err)
	}
	if len(log.verified) != 1 {
		t.Fatalf("only the winner should be verified, got %v", log.verified)
	}
	if len(log.discarded) == 0 {
		t.Fatalf("faster losers that connected should be discarded")
	}
}

func TestRaceSkipsBlackholedLeaderAfterGrace(t *testing.T) {
	start := time.Now()
	res, err, _ := runFakeRace(t, context.Background(), testRaceConfig(), []fakeCandidate{
		{blockDial: true},
		{dialDelay: 1 * time.Millisecond},
	})
	if err != nil || res.Winner != 1 {
		t.Fatalf("want candidate 1, got winner=%d err=%v", res.Winner, err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("candidate 1 went ahead before the leader's grace ran out (%s)", elapsed)
	}
}

func TestRaceFallsToNextReadyWhenVerifyFails(t *testing.T) {
	res, err, log := runFakeRace(t, context.Background(), testRaceConfig(), []fakeCandidate{
		{dialDelay: 1 * time.Millisecond, verifyErr: errors.New("no handshake")},
		{dialDelay: 1 * time.Millisecond},
	})
	if err != nil || res.Winner != 1 {
		t.Fatalf("want candidate 1, got winner=%d err=%v", res.Winner, err)
	}
	if res.Failures[0] == nil {
		t.Fatalf("candidate 0's failure should be recorded")
	}
	if len(log.verified) != 2 || log.verified[0] != 0 || log.verified[1] != 1 {
		t.Fatalf("want verify order [0 1], got %v", log.verified)
	}
}

func TestRaceNeverDialsLazyAhead(t *testing.T) {
	res, err, log := runFakeRace(t, context.Background(), testRaceConfig(), []fakeCandidate{
		{dialDelay: 1 * time.Millisecond, verifyErr: errors.New("no handshake")},
		{lazy: true},
		{dialDelay: 1 * time.Millisecond},
	})
	if err != nil || res.Winner != 1 {
		t.Fatalf("want lazy candidate 1 at its turn, got winner=%d err=%v", res.Winner, err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	// The lazy candidate starts only once it is verified, i.e. after candidate 0 failed.
	for idx, i := range log.started {
		if i == 1 {
			for _, earlier := range log.started[:idx] {
				if earlier == 0 {
					return
				}
			}
			t.Fatalf("lazy candidate started before its turn: %v", log.started)
		}
	}
	t.Fatalf("lazy candidate never started: %v", log.started)
}

func TestRaceExhaustedRecordsEveryFailure(t *testing.T) {
	res, err, log := runFakeRace(t, context.Background(), testRaceConfig(), []fakeCandidate{
		{dialErr: errors.New("reset")},
		{lazy: true, verifyErr: errors.New("no handshake")},
		{dialDelay: 1 * time.Millisecond, verifyErr: errors.New("no handshake")},
		{dialErr: errors.New("reset")},
	})
	if err != nil || res.Winner != -1 {
		t.Fatalf("want exhausted, got winner=%d err=%v", res.Winner, err)
	}
	for i, f := range res.Failures {
		if f == nil {
			t.Fatalf("candidate %d has no recorded failure", i)
		}
	}
	if log.overlap {
		t.Fatalf("verifications overlapped")
	}
}

func TestRaceRespectsMaxInFlight(t *testing.T) {
	cfg := testRaceConfig()
	cfg.MaxInFlight = 2
	cands := []fakeCandidate{{blockDial: true}, {blockDial: true}, {blockDial: true}, {blockDial: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	_, err, log := runFakeRace(t, ctx, cfg, cands)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	if len(log.started) != 2 {
		t.Fatalf("want 2 dials in flight, got %v", log.started)
	}
}

func TestRaceFatalStopsRace(t *testing.T) {
	offline := errors.New("network is unreachable")
	log := &raceLog{}
	funcs := RaceFuncs[int]{
		Lazy: func(int) bool { return false },
		Start: func(ctx context.Context, i int) (int, error) {
			log.add(&log.started, i)
			if i == 0 {
				return 0, offline
			}
			<-ctx.Done()
			return 0, ctx.Err()
		},
		Verify:  func(context.Context, int, int) error { return nil },
		Discard: func(int, int) {},
		Fatal:   func(err error) bool { return errors.Is(err, offline) },
	}
	_, err := Race(context.Background(), 3, testRaceConfig(), funcs)
	if !errors.Is(err, ErrRaceFatal) || !errors.Is(err, offline) {
		t.Fatalf("want fatal offline error, got %v", err)
	}
}

func TestRaceKeepsVerifyThatLandsDuringCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	verifyStarted := make(chan struct{})
	funcs := RaceFuncs[int]{
		Lazy:  func(int) bool { return false },
		Start: func(context.Context, int) (int, error) { return 7, nil },
		Verify: func(context.Context, int, int) error {
			close(verifyStarted)
			time.Sleep(30 * time.Millisecond)
			return nil
		},
		Discard: func(int, int) {},
	}
	go func() {
		<-verifyStarted
		cancel()
	}()
	res, err := Race(ctx, 1, testRaceConfig(), funcs)
	if err != nil || res.Winner != 0 || res.Value != 7 {
		t.Fatalf("want the landed verify kept, got winner=%d value=%d err=%v", res.Winner, res.Value, err)
	}
}
