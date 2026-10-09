package transport

import (
	"context"
	"time"
)

// Manager is the one thing every DPI-evasion transport can do without any
// transport-specific knowledge: stop. Start/Status are deliberately excluded.
type Manager interface {
	Stop(ctx context.Context) error
}

// SessionWaiter is an optional capability: transports that can report when
// their handshake/session completes implement this, checked via type assertion.
type SessionWaiter interface {
	WaitForSession(ctx context.Context, timeout time.Duration) error
}

// BoundPortReporter is an optional capability: transports that bind a
// dynamically-allocated local port (LocalPort=0) implement this.
type BoundPortReporter interface {
	BoundLocalPort() int
}

// Implementers should add `var _ transport.SessionWaiter = (*Manager)(nil)`
// locally so signature drift fails the build, not the assertion.
