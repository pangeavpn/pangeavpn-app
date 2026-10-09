package splittunnel

import (
	"sync"
	"time"
)

const logEvery = 60 * time.Second

// rateLog never carries per-flow data: callers pass state transitions, counts and OS error text only.
type rateLog struct {
	logf func(format string, args ...any)
	mu   sync.Mutex
	last map[string]time.Time
}

func newRateLog(logf func(format string, args ...any)) *rateLog {
	return &rateLog{logf: logf, last: make(map[string]time.Time)}
}

func (l *rateLog) printf(format string, args ...any) {
	if l.logf != nil {
		l.logf(format, args...)
	}
}

func (l *rateLog) limited(class, format string, args ...any) {
	if l.logf == nil {
		return
	}
	now := time.Now()
	l.mu.Lock()
	if t, ok := l.last[class]; ok && now.Sub(t) < logEvery {
		l.mu.Unlock()
		return
	}
	l.last[class] = now
	l.mu.Unlock()
	l.logf(format, args...)
}
