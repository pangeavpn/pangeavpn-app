//go:build darwin && !ios

package nicpin

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// acquire binds by IP_BOUND_IF plus a scoped host route per address (the tunnel's /1s beat
// the unscoped default); unbound sockets never consult a scoped route, so no app follows it.
func acquire(addrs []netip.Addr) (*Pin, error) {
	name, index, gateway, err := physicalInterface()
	if err != nil {
		return nil, err
	}
	var held []scopedHost
	for _, addr := range addrs {
		key := scopedHost{iface: name, addr: addr}
		if err := scoped.acquire(key, gateway); err != nil {
			scoped.release(held)
			return nil, err
		}
		held = append(held, key)
	}
	return &Pin{Interface: name, index: index, release: func() { scoped.release(held) }}, nil
}

type scopedHost struct {
	iface string
	addr  netip.Addr
}

// scopedRoutes counts holders per route, so a CDN restart's late release can't pull
// the route out from under the session that replaced it.
type scopedRoutes struct {
	mu    sync.Mutex
	held  map[scopedHost]int
	route func(args ...string) error
}

var scoped = &scopedRoutes{held: map[scopedHost]int{}, route: runRoute}

func (s *scopedRoutes) acquire(key scopedHost, gateway string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Always (re)written: the gateway may have moved since an earlier holder added it.
	add := scopedRouteArgs("add", key, gateway)
	if err := s.route(add...); err != nil {
		if !isRouteExists(err) {
			return fmt.Errorf("add scoped route to %s on %s: %w", key.addr, key.iface, err)
		}
		if err := s.route(scopedRouteArgs("change", key, gateway)...); err != nil {
			return fmt.Errorf("update scoped route to %s on %s: %w", key.addr, key.iface, err)
		}
	}
	s.held[key]++
	return nil
}

func (s *scopedRoutes) release(keys []scopedHost) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		if s.held[key]--; s.held[key] > 0 {
			continue
		}
		delete(s.held, key)
		_ = s.route(scopedRouteArgs("delete", key, "")...)
	}
}

// scopedRouteArgs builds route(8) arguments; an empty gateway means the link is on-link.
func scopedRouteArgs(op string, key scopedHost, gateway string) []string {
	args := []string{"-n", op, "-ifscope", key.iface, "-host", key.addr.String()}
	if op == "delete" {
		return args
	}
	if gateway == "" {
		return append(args, "-interface", key.iface)
	}
	return append(args, gateway)
}

const routeTimeout = 5 * time.Second

var errRouteExists = errors.New("route exists")

func runRoute(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), routeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/sbin/route", args...).CombinedOutput()
	text := strings.ToLower(strings.TrimSpace(string(out)))
	if strings.Contains(text, "file exists") || strings.Contains(text, "already in table") {
		return errRouteExists
	}
	// route(8) exits 0 when the kernel refuses a change; only its warning says so.
	if err == nil && strings.Contains(text, "routing socket") {
		err = errors.New("refused")
	}
	if err != nil {
		return fmt.Errorf("route %s: %w (%s)", strings.Join(args, " "), err, text)
	}
	return nil
}

func isRouteExists(err error) bool { return errors.Is(err, errRouteExists) }
