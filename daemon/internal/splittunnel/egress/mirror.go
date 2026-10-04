package egress

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// mirrorStaleGrace outlasts a configd reconfiguration, which takes over a demoted interface's key.
	mirrorStaleGrace = 2 * time.Second
	mirrorAddBackoff = 30 * time.Second
)

// routeMirror keeps a scoped copy of the primary default route for IP_BOUND_IF sockets: with the
// tunnel's 0.0.0.0/1 installed, XNU's last-resort unscoped-default lookup resolves to that /1 instead.
type routeMirror struct {
	fetch func() ([]ribRoute, error)
	run   func(args ...string) error
	iface func(index int) (name string, up bool)
	now   func() time.Time
	log   *rateLog

	mu         sync.Mutex
	closed     bool
	unwanted   bool
	staleSince map[scopedDefault]time.Time
	failed     scopedDefault
	retryAt    time.Time
}

func newRouteMirror(fetch func() ([]ribRoute, error), run func(args ...string) error, iface func(int) (string, bool), log *rateLog) *routeMirror {
	return &routeMirror{fetch: fetch, run: run, iface: iface, now: time.Now, log: log, staleSince: make(map[scopedDefault]time.Time)}
}

// setWanted says whether off-tunnel sockets need the mirror now; the next sync acts on it.
func (m *routeMirror) setWanted(wanted bool) {
	m.mu.Lock()
	m.unwanted = !wanted
	m.mu.Unlock()
}

func (m *routeMirror) sync(routes []ribRoute) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	plan := planDarwinMirror(routes, m.iface)
	if m.unwanted {
		plan.Remove, plan.Add = darwinMirrors(routes, m.iface), nil
	}
	m.removeStale(plan)
	if plan.Add == nil {
		return
	}
	now := m.now()
	if *plan.Add == m.failed && now.Before(m.retryAt) {
		return
	}
	if err := m.run(mirrorArgs("add", *plan.Add)...); err != nil {
		m.failed, m.retryAt = *plan.Add, now.Add(mirrorAddBackoff)
		m.log.log("mirror-add", "split egress: adding the scoped default route on %s failed: %v", plan.Add.Name, err)
		return
	}
	m.failed = scopedDefault{}
	m.log.log("mirror-on", "split egress: scoped default route added on %s for off-tunnel sockets", plan.Add.Name)
}

// removeStale deletes a mirror on the primary at once: configd never scopes the primary's own default.
// Elsewhere it waits for a later sync, as configd may have just replaced it at that key.
func (m *routeMirror) removeStale(plan mirrorPlan) {
	now := m.now()
	seen := make(map[scopedDefault]bool, len(plan.Remove))
	var due []scopedDefault
	for _, r := range plan.Remove {
		seen[r] = true
		since, ok := m.staleSince[r]
		switch {
		case r.Index == plan.Primary, ok && now.Sub(since) >= mirrorStaleGrace:
			due = append(due, r)
			delete(m.staleSince, r)
		case !ok:
			m.staleSince[r] = now
		}
	}
	for r := range m.staleSince {
		if !seen[r] {
			delete(m.staleSince, r)
		}
	}
	m.remove(due)
}

// close removes every mirror, so a stopped daemon leaves no scoped default behind.
func (m *routeMirror) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	routes, err := m.fetch()
	if err != nil {
		m.log.log("mirror-del", "split egress: reading routes to remove the scoped default failed: %v", err)
		return
	}
	m.remove(darwinMirrors(routes, m.iface))
}

func (m *routeMirror) remove(mirrors []scopedDefault) {
	for _, r := range mirrors {
		if err := m.run(mirrorArgs("delete", r)...); err != nil {
			m.log.log("mirror-del", "split egress: removing the scoped default route on %s failed: %v", r.Name, err)
		}
	}
}

// mirrorArgs always passes -ifscope: without it a delete would take the system's own default route.
func mirrorArgs(op string, r scopedDefault) []string {
	args := []string{"-n", op, "-ifscope", r.Name}
	if op == "add" {
		args = append(args, "-proto2")
	}
	return append(args, "default", r.NextHop.String())
}

// routeResult reads a route(8) run: it exits 0 when the kernel refuses the change, and only its
// "writing to routing socket" warning says so.
func routeResult(out []byte, err error) error {
	text := strings.TrimSpace(string(out))
	if err == nil && strings.Contains(text, "routing socket") {
		err = errors.New("refused")
	}
	if err != nil {
		return fmt.Errorf("%w (%s)", err, text)
	}
	return nil
}
