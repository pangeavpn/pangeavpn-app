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
var hostAnswers, hostFailures sync.Map

const (
	hostAnswerFresh = 5 * time.Minute
	hostFailureHold = 30 * time.Second
	hostLookupCap   = 4 * time.Second
)

var lookupIPUncached = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// CachedLookupIP answers from a recent lookup at once, and falls back to the last good
// answer when a fresh one fails, since mid-switch the old tunnel's DNS is often gone.
func CachedLookupIP(ctx context.Context, host string) ([]net.IP, error) {
	var last []net.IP
	if v, ok := hostAnswers.Load(host); ok {
		a := v.(hostAnswer)
		if time.Since(a.at) < hostAnswerFresh {
			return a.ips, nil
		}
		last = a.ips
	}
	if v, ok := hostFailures.Load(host); ok && time.Since(v.(time.Time)) < hostFailureHold {
		if last != nil {
			return last, nil
		}
		return nil, fmt.Errorf("lookup %s failed moments ago", host)
	}
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
	if last != nil {
		return last, nil
	}
	if err == nil {
		err = fmt.Errorf("lookup %s returned no addresses", host)
	}
	return nil, err
}

// PrefetchHosts resolves hosts in the background, so a switch to a server never waits on DNS.
func PrefetchHosts(hosts []string) {
	for _, host := range hosts {
		if host == "" || net.ParseIP(host) != nil {
			continue
		}
		go CachedLookupIP(context.Background(), host)
	}
}
