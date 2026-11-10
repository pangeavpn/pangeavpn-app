//go:build android || ios

package mobile

// Automatic mode's fast fallback: dial several transports at once, then prove
// a WireGuard handshake over the best-ranked one that connected. The device is
// shared, so the handshake checks still run one at a time, in cascade order.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.zx2c4.com/wireguard/device"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// racer is one cascade candidate that starts on a starter of its own, so
// several can hold a connection at the same time.
type racer struct {
	kind  string
	start func(ctx context.Context) (*starter, error)
}

// racersFor turns the selected candidates into racers, keeping their order.
func racersFor(profile *state.Profile, candidates []transport.Candidate, logs *state.LogStore) []racer {
	racers := make([]racer, len(candidates))
	for i, candidate := range candidates {
		kind := candidate.Kind
		racers[i] = racer{kind: kind, start: func(ctx context.Context) (*starter, error) {
			rs := newStarter(logs)
			start, availability := rs.StarterFor(profile, kind)
			if start == nil || availability != transport.Available {
				return nil, fmt.Errorf("%s: not available on this device", kind)
			}
			if err := start(ctx, profile, nil); err != nil {
				rs.stopActive(context.Background())
				return nil, err
			}
			return rs, nil
		}}
	}
	return racers
}

// raceConfig is the desktop default with a per-platform cap on open racers.
func raceConfig() transport.RaceConfig {
	cfg := transport.DefaultRaceConfig
	cfg.MaxInFlight = raceMaxInFlight
	return cfg
}

// raceCascade is walkCascade with the dials run ahead. It owns dev and closes
// it when every racer fails or a stop cancels the attempt.
func raceCascade(attempt *startAttempt, profile *state.Profile, dev *device.Device,
	racers []racer, logs *state.LogStore, netKey string) error {
	p := attempt.prepared
	label := tunnelLabel(p)
	ctx := attempt.ctx
	logAdd(logs, state.LogInfo, fmt.Sprintf("racing %d transports", len(racers)))

	res, err := transport.Race(ctx, len(racers), raceConfig(), transport.RaceFuncs[*starter]{
		Lazy:  func(i int) bool { return transport.LazyDial[racers[i].kind] },
		Start: func(ctx context.Context, i int) (*starter, error) { return racers[i].start(ctx) },
		Verify: func(ctx context.Context, i int, rs *starter) error {
			if err := proveOver(ctx, dev, p, racers[i].kind, rs.boundPort); err != nil {
				rs.stopActive(context.Background())
				return err
			}
			return nil
		},
		Discard: func(_ int, rs *starter) { rs.stopActive(context.Background()) },
		OnVerify: func(i int) {
			pushAttemptStatus(attempt, label.with(string(state.StateConnecting), "starting "+racers[i].kind))
		},
	})

	var failures []string
	for i, failure := range res.Failures {
		if failure == nil || errors.Is(failure, context.Canceled) {
			continue
		}
		failures = append(failures, failure.Error())
		logAdd(logs, state.LogWarn, fmt.Sprintf("%s transport did not establish a tunnel: %v", racers[i].kind, failure))
	}

	if err == nil && res.Winner >= 0 {
		kind := racers[res.Winner].kind
		if res.Winner > 0 {
			logAdd(logs, state.LogInfo, "fell back to "+kind+" transport")
		}
		if goLive(attempt, dev, res.Value, label, len(p.presharedKeyRaw) == pq.PresharedKeySize, kind) {
			rememberTransport(netKey, kind)
			return nil
		}
		res.Value.stopActive(context.Background())
	}

	dev.Close()
	if ctx.Err() != nil {
		return errStartCancelled
	}
	detail := "no transport could establish a tunnel: " + strings.Join(failures, "; ")
	pushAttemptStatus(attempt, label.with(string(state.StateError), detail))
	return errors.New("mobile: " + detail)
}
