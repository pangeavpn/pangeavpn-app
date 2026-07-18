package transport

import (
	"context"
	"time"
)

// Manager is the one thing every DPI-evasion transport (Cloak, NaiveProxy,
// and future ones) can do without any transport-specific knowledge: stop.
// cloak.Manager already satisfies this without modification (its Stop is
// already `Stop(ctx context.Context) error`); naive.Manager (Task 5) is
// built to satisfy it directly. Start and Status are deliberately NOT part
// of this interface — see the design note in this task's plan entry
// (docs/superpowers/plans/2026-07-18-naiveproxy-transport.md, Task 2) for
// why forcing common signatures for those isn't worth it here.
type Manager interface {
	Stop(ctx context.Context) error
}

// SessionWaiter is an optional capability: transports that can report when
// their handshake/session completes (as opposed to just "process started")
// implement this. Checked via type assertion, same pattern service.go
// already used ad-hoc for cloak before this package existed.
type SessionWaiter interface {
	WaitForSession(ctx context.Context, timeout time.Duration) error
}

// BoundPortReporter is an optional capability: transports that bind a
// dynamically-allocated local port (LocalPort=0 requested) implement this so
// callers can discover the kernel-assigned port.
type BoundPortReporter interface {
	BoundLocalPort() int
}
