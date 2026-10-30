//go:build android || ios

package mobile

// A Start still walking the cascade, registered so Stop, StopForSwitch and
// Wipe can cancel it before it goes live.

import (
	"context"
	"errors"
	"time"
)

var errStartCancelled = errors.New("mobile: the start was cancelled by a stop")

// startUnwindWait bounds how long a stop waits for a cancelled start to tear
// down what it brought up.
const startUnwindWait = 5 * time.Second

type startAttempt struct {
	ctx    context.Context
	cancel context.CancelFunc
	// done closes once the attempt has released everything it held.
	done     chan struct{}
	prepared *preparedTunnel
	// live: the tunnel went up, and its runtime now owns ctx; guarded by mu.
	live bool
}

// starting is the attempt in its cascade, nil once it is live or ended;
// guarded by mu.
var starting *startAttempt

// claimStart registers a start for the prepared config, refusing while a
// tunnel is running or another start is still in its cascade.
func claimStart() (*startAttempt, error) {
	mu.Lock()
	defer mu.Unlock()
	if prepared == nil {
		return nil, errors.New("mobile: no prepared tunnel; call Prepare first")
	}
	if activeTunnel != nil || starting != nil {
		return nil, errors.New("mobile: tunnel already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	attempt := &startAttempt{ctx: ctx, cancel: cancel, done: make(chan struct{}), prepared: prepared}
	prepared.starts++
	starting = attempt
	return attempt, nil
}

// finishStart releases the attempt, scrubbing its keys if Prepare or a sign-out
// dropped the config while the cascade still needed them.
func finishStart(attempt *startAttempt) {
	mu.Lock()
	if starting == attempt {
		starting = nil
	}
	p := attempt.prepared
	p.starts--
	if p.starts == 0 && prepared != p {
		p.scrub()
	}
	live := attempt.live
	mu.Unlock()
	// A live tunnel's transports were started under ctx; closing it cancels ctx.
	if !live {
		attempt.cancel()
	}
	close(attempt.done)
}

// awaitUnwind lets a cancelled start finish tearing down, so nothing it
// brought up outlives the stop that cancelled it.
func awaitUnwind(attempt *startAttempt) {
	if attempt == nil {
		return
	}
	select {
	case <-attempt.done:
	case <-time.After(startUnwindWait):
	}
}
