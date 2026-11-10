package transport

import (
	"context"
	"errors"
	"sync"
	"time"
)

// LazyDial lists the transports whose start only binds a local listener and
// dials the node once WireGuard sends its first packet. Their start proves
// nothing about the network, so the race never dials them ahead: they run at
// their turn, start and WireGuard check together.
var LazyDial = map[string]bool{"cloak": true, "shadowsocks": true}

// RaceConfig tunes Race. The zero value is not usable; see DefaultRaceConfig.
type RaceConfig struct {
	// Stagger is how long one dial gets before the next candidate starts dialling
	// alongside it. A failed dial lets the next one start straight away.
	Stagger time.Duration
	// Grace is how long a candidate still dialling keeps priority. Once it has
	// dialled this long, a lower-ranked candidate that is ready, or lazy, may
	// go ahead of it. This keeps the cascade order when the network is fine.
	Grace time.Duration
	// MaxInFlight caps candidates holding a dial or an open connection at once.
	// The highest-ranked unresolved candidate may always start, so the cap
	// can never stall the race.
	MaxInFlight int
}

// DefaultRaceConfig: a reachable node finishes a TLS or QUIC dial well inside
// Grace even on a slow, lossy link, so a candidate still dialling after that
// is most likely blackholed.
var DefaultRaceConfig = RaceConfig{
	Stagger:     250 * time.Millisecond,
	Grace:       3 * time.Second,
	MaxInFlight: 3,
}

// RaceFuncs are the per-candidate steps Race drives. i indexes the candidate list.
type RaceFuncs[T any] struct {
	// Lazy reports a candidate that is never dialled ahead (see LazyDial).
	Lazy func(i int) bool
	// Start dials candidate i. It runs concurrently with other Starts, and must
	// clean up after itself when it returns an error.
	Start func(ctx context.Context, i int) (T, error)
	// Verify brings WireGuard up over a started candidate and proves it carries
	// traffic. Calls never overlap, since there is one WireGuard device. It must
	// clean up after itself, the started transport included, when it fails.
	Verify func(ctx context.Context, i int, v T) error
	// Discard stops a started candidate that will not be used.
	Discard func(i int, v T)
	// Fatal reports a failure that should end the whole race, such as the host
	// going offline. Optional.
	Fatal func(err error) bool
	// OnVerify is told which candidate is about to be verified. Optional.
	OnVerify func(i int)
}

// RaceResult is the outcome of a Race.
type RaceResult[T any] struct {
	// Winner indexes the verified candidate, or is -1.
	Winner int
	Value  T
	// Failures holds each candidate's error by index, nil where none was recorded.
	Failures []error
}

// ErrRaceFatal wraps the error that made Fatal end a race.
var ErrRaceFatal = errors.New("transport race aborted")

type raceStatus int

const (
	raceIdle raceStatus = iota
	raceDialling
	raceReady
	raceVerifying
	raceFailed
	raceWon
)

type racer[T any] struct {
	status    raceStatus
	startedAt time.Time
	value     T
}

type dialResult[T any] struct {
	i     int
	value T
	err   error
}

type verifyResult[T any] struct {
	i     int
	value T
	err   error
}

// raceTick bounds how late Race notices a stagger or grace deadline.
const raceTick = 25 * time.Millisecond

// Race dials candidates in parallel with staggered starts and verifies the
// started ones one at a time, in rank order once Grace allows, until one passes.
// Every dial still running when Race returns is cancelled, waited for, and discarded.
func Race[T any](ctx context.Context, n int, cfg RaceConfig, f RaceFuncs[T]) (RaceResult[T], error) {
	result := RaceResult[T]{Winner: -1, Failures: make([]error, n)}
	if n == 0 {
		return result, nil
	}
	if cfg.MaxInFlight < 1 {
		cfg.MaxInFlight = 1
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	racers := make([]racer[T], n)
	dials := make(chan dialResult[T], n)
	verifies := make(chan verifyResult[T], 1)
	var wg sync.WaitGroup
	verifying := false
	var lastLaunch time.Time

	defer func() {
		// Losers: cancel their dials, wait them out, and stop whatever they opened.
		cancel()
		wg.Wait()
		close(dials)
		for d := range dials {
			if d.err == nil && d.i != result.Winner {
				f.Discard(d.i, d.value)
			}
		}
		for i := range racers {
			if racers[i].status == raceReady && i != result.Winner {
				f.Discard(i, racers[i].value)
			}
		}
	}()

	inFlight := func() int {
		count := 0
		for i := range racers {
			if s := racers[i].status; s == raceDialling || s == raceReady || s == raceVerifying {
				count++
			}
		}
		return count
	}
	// firstOpen is the best-ranked candidate not yet failed: the one the cascade
	// would be waiting on, and so the one always allowed to start.
	firstOpen := func() int {
		for i := range racers {
			if racers[i].status != raceFailed {
				return i
			}
		}
		return n
	}
	launch := func(i int, now time.Time) {
		racers[i].status = raceDialling
		racers[i].startedAt = now
		lastLaunch = now
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := f.Start(raceCtx, i)
			dials <- dialResult[T]{i: i, value: v, err: err}
		}()
	}
	// eligible: everything ranked above i has failed, or has been dialling past Grace.
	eligible := func(i int, now time.Time) bool {
		for j := 0; j < i; j++ {
			switch racers[j].status {
			case raceFailed:
			case raceDialling:
				if now.Sub(racers[j].startedAt) < cfg.Grace {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	verify := func(i int, v T) {
		racers[i].status = raceVerifying
		verifying = true
		if f.OnVerify != nil {
			f.OnVerify(i)
		}
		lazy := f.Lazy(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			if lazy {
				// A lazy start is local-only, so it is part of its own check.
				v, err = f.Start(raceCtx, i)
			}
			if err == nil {
				err = f.Verify(raceCtx, i, v)
			}
			verifies <- verifyResult[T]{i: i, value: v, err: err}
		}()
	}

	ticker := time.NewTicker(raceTick)
	defer ticker.Stop()

	for {
		now := time.Now()

		// Dial ahead, in rank order, skipping lazy candidates.
		for i := range racers {
			if racers[i].status != raceIdle || f.Lazy(i) {
				continue
			}
			if i == firstOpen() {
				launch(i, now)
				continue
			}
			if inFlight() >= cfg.MaxInFlight || (!lastLaunch.IsZero() && now.Sub(lastLaunch) < cfg.Stagger && anyDialling(racers)) {
				break
			}
			launch(i, now)
		}

		// Verify the best-ranked candidate that may go now.
		if !verifying {
			for i := range racers {
				r := &racers[i]
				ready := r.status == raceReady || (r.status == raceIdle && f.Lazy(i))
				if ready && eligible(i, now) {
					verify(i, r.value)
					break
				}
			}
		}

		if !verifying && !anyDialling(racers) && firstOpen() == n {
			return result, nil
		}

		select {
		case <-ctx.Done():
			if verifying {
				// Verify sees the same cancellation; let it finish so the WireGuard
				// device is never left mid-bring-up, and keep a pass that landed first.
				v := <-verifies
				if v.err == nil {
					racers[v.i].status = raceWon
					result.Winner = v.i
					result.Value = v.value
					return result, nil
				}
				racers[v.i].status = raceFailed
				result.Failures[v.i] = v.err
			}
			return result, ctx.Err()
		case d := <-dials:
			if d.err != nil {
				racers[d.i].status = raceFailed
				result.Failures[d.i] = d.err
				// The slot it held is free: the next candidate need not wait out the stagger.
				lastLaunch = time.Time{}
				if f.Fatal != nil && f.Fatal(d.err) {
					return result, errors.Join(ErrRaceFatal, d.err)
				}
				continue
			}
			racers[d.i].status = raceReady
			racers[d.i].value = d.value
		case v := <-verifies:
			verifying = false
			if v.err == nil {
				racers[v.i].status = raceWon
				result.Winner = v.i
				result.Value = v.value
				return result, nil
			}
			racers[v.i].status = raceFailed
			result.Failures[v.i] = v.err
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if f.Fatal != nil && f.Fatal(v.err) {
				return result, errors.Join(ErrRaceFatal, v.err)
			}
		case <-ticker.C:
		}
	}
}

func anyDialling[T any](racers []racer[T]) bool {
	for i := range racers {
		if racers[i].status == raceDialling {
			return true
		}
	}
	return false
}
