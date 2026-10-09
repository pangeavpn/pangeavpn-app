//go:build android

package mobile

// Mobile's half of the transport cascade: one start func per transport, and
// the availability rules transport.Select consumes.

import (
	"context"
	"fmt"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/hysteria2"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reality"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// transportSessionTimeout bounds how long a transport is given to form its own
// session before the cascade abandons it.
const transportSessionTimeout = 10 * time.Second

// sessionWaiter and boundPortReporter are the optional transport capabilities
// declared in daemon/internal/transport, checked by type assertion.
type sessionWaiter interface {
	WaitForSession(ctx context.Context, timeout time.Duration) error
}

type boundPortReporter interface {
	BoundLocalPort() int
}

// starter implements transport.Starter and records what the winning attempt
// left running, so the caller can point WireGuard at it and stop it later.
type starter struct {
	logs *state.LogStore

	active    transport.Manager
	kind      string
	boundPort int
}

func newStarter(logs *state.LogStore) *starter {
	return &starter{logs: logs}
}

// StarterFor reports what this build can attempt. Snowflake is permanently
// unavailable: pion's WebRTC sockets expose no VpnService.protect() hook.
func (s *starter) StarterFor(profile *state.Profile, kind string) (transport.StartFn, transport.Availability) {
	switch kind {
	case "cloak":
		return s.startCloak, transport.Available
	case "reality":
		if profile.Reality == nil {
			return nil, transport.NotConfigured
		}
		return s.startReality, s.singBoxAvailability()
	case "shadowsocks":
		if profile.Shadowsocks == nil {
			return nil, transport.NotConfigured
		}
		return s.startShadowsocks, s.singBoxAvailability()
	case "hysteria2":
		if profile.Hysteria2 == nil {
			return nil, transport.NotConfigured
		}
		return s.startHysteria2, s.singBoxAvailability()
	case "naive":
		if !naiveLinked {
			return nil, transport.Unavailable
		}
		if profile.Naive == nil {
			return nil, transport.NotConfigured
		}
		return s.startNaive, transport.Available
	case "snowflake":
		return nil, transport.Unavailable
	default:
		return nil, transport.NotConfigured
	}
}

// singBoxAvailability fails these transports closed when the protect socket is
// missing: their dials would otherwise be captured by the TUN they carry.
func (s *starter) singBoxAvailability() transport.Availability {
	if protectServer == nil {
		return transport.Unavailable
	}
	return transport.Available
}

// record captures the started manager and the loopback port WireGuard must
// send to, falling back to the requested port when the transport reports none.
func (s *starter) record(kind string, mgr transport.Manager, requestedPort int) {
	port := requestedPort
	if reporter, ok := mgr.(boundPortReporter); ok {
		if bound := reporter.BoundLocalPort(); bound > 0 {
			port = bound
		}
	}
	s.active = mgr
	s.kind = kind
	s.boundPort = port
}

// awaitSession gives transports that can prove their own handshake a chance to
// fail fast, before WireGuard is pointed at them.
func awaitSession(ctx context.Context, mgr transport.Manager) error {
	waiter, ok := mgr.(sessionWaiter)
	if !ok {
		return nil
	}
	return waiter.WaitForSession(ctx, transportSessionTimeout)
}

// stopActive tears down whatever the last attempt started.
func (s *starter) stopActive(ctx context.Context) {
	if s.active == nil {
		return
	}
	_ = s.active.Stop(ctx)
	s.active = nil
	s.kind = ""
	s.boundPort = 0
}

func (s *starter) startCloak(ctx context.Context, profile *state.Profile, _ *state.WireGuardProfile) error {
	mgr := cloak.NewManager(s.logs)
	start := profile.Cloak
	start.LocalPort = 0
	if err := mgr.Start(ctx, start); err != nil {
		return fmt.Errorf("cloak start: %w", err)
	}
	if err := awaitSession(ctx, mgr); err != nil {
		_ = mgr.Stop(context.Background())
		return fmt.Errorf("cloak session: %w", err)
	}
	s.record("cloak", mgr, profile.Cloak.LocalPort)
	return nil
}

func (s *starter) startReality(ctx context.Context, profile *state.Profile, _ *state.WireGuardProfile) error {
	mgr := reality.NewManager(s.logs)
	start := *profile.Reality
	start.LocalPort = 0
	if err := mgr.Start(ctx, start); err != nil {
		return fmt.Errorf("reality start: %w", err)
	}
	if err := awaitSession(ctx, mgr); err != nil {
		_ = mgr.Stop(context.Background())
		return fmt.Errorf("reality session: %w", err)
	}
	s.record("reality", mgr, profile.Reality.LocalPort)
	return nil
}

func (s *starter) startShadowsocks(ctx context.Context, profile *state.Profile, _ *state.WireGuardProfile) error {
	mgr := shadowsocks.NewManager(s.logs)
	start := *profile.Shadowsocks
	start.LocalPort = 0
	if err := mgr.Start(ctx, start); err != nil {
		return fmt.Errorf("shadowsocks start: %w", err)
	}
	if err := awaitSession(ctx, mgr); err != nil {
		_ = mgr.Stop(context.Background())
		return fmt.Errorf("shadowsocks session: %w", err)
	}
	s.record("shadowsocks", mgr, profile.Shadowsocks.LocalPort)
	return nil
}

func (s *starter) startHysteria2(ctx context.Context, profile *state.Profile, _ *state.WireGuardProfile) error {
	mgr := hysteria2.NewManager(s.logs)
	start := *profile.Hysteria2
	start.LocalPort = 0
	if err := mgr.Start(ctx, start); err != nil {
		return fmt.Errorf("hysteria2 start: %w", err)
	}
	if err := awaitSession(ctx, mgr); err != nil {
		_ = mgr.Stop(context.Background())
		return fmt.Errorf("hysteria2 session: %w", err)
	}
	s.record("hysteria2", mgr, profile.Hysteria2.LocalPort)
	return nil
}
