package platform

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

type hostAnswer struct {
	ips []net.IP
	at  time.Time
}

// hostAnswers is each endpoint host's last good lookup, shared by the lock and the route
// pins; hostFailures holds a recent failure so one switch never waits on it twice.
var hostAnswers, hostFailures, hostRefreshing sync.Map

const (
	hostAnswerFresh = 5 * time.Minute
	hostFailureHold = 30 * time.Second
	hostLookupCap   = 4 * time.Second
)

var lookupIPUncached = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// CachedLookupIP answers from the last good lookup at once, refreshing it in the background
// once it is old: mid-switch the old tunnel's DNS is often gone, and these hosts rarely move.
func CachedLookupIP(ctx context.Context, host string) ([]net.IP, error) {
	if v, ok := hostAnswers.Load(host); ok {
		a := v.(hostAnswer)
		if time.Since(a.at) >= hostAnswerFresh {
			refreshHost(host)
		}
		return a.ips, nil
	}
	if v, ok := hostFailures.Load(host); ok && time.Since(v.(time.Time)) < hostFailureHold {
		return nil, fmt.Errorf("lookup %s failed moments ago", host)
	}
	return lookupAndStore(ctx, host)
}

func lookupAndStore(ctx context.Context, host string) ([]net.IP, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, hostLookupCap)
	defer cancel()
	ips, err := lookupIPUncached(lookupCtx, host)
	if err == nil && len(ips) > 0 {
		hostAnswers.Store(host, hostAnswer{ips: ips, at: time.Now()})
		hostFailures.Delete(host)
		return ips, nil
	}
	if ctx.Err() == nil {
		hostFailures.Store(host, time.Now())
	}
	if err == nil {
		err = fmt.Errorf("lookup %s returned no addresses", host)
	}
	return nil, err
}

func refreshHost(host string) {
	if _, busy := hostRefreshing.LoadOrStore(host, struct{}{}); busy {
		return
	}
	go func() {
		defer hostRefreshing.Delete(host)
		_, _ = lookupAndStore(context.Background(), host)
	}()
}

// PrefetchHosts resolves hosts in the background, so a switch to a server never waits on DNS.
func PrefetchHosts(hosts []string) {
	for _, host := range hosts {
		if host == "" || net.ParseIP(host) != nil {
			continue
		}
		if v, ok := hostAnswers.Load(host); ok && time.Since(v.(hostAnswer).at) < hostAnswerFresh {
			continue
		}
		refreshHost(host)
	}
}
