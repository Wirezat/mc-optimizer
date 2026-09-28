package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
)

// limiter refuses a key once it has collected max events inside window, until
// the oldest event ages out.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	events map[string][]time.Time
	now    func() time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, events: map[string][]time.Time{}, now: time.Now}
}

var (
	loginIPLimiter      = newLimiter(5, 15*time.Minute)
	loginAccountLimiter = newLimiter(20, 15*time.Minute)
	registerIPLimiter   = newLimiter(5, 15*time.Minute)
	refreshIPLimiter    = newLimiter(20, 15*time.Minute)
)

// retryAfter reports how long key must wait, or zero if it may proceed.
func (l *limiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := l.prune(key, now)
	if len(recent) < l.max {
		return 0
	}
	return recent[0].Add(l.window).Sub(now)
}

func (l *limiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.events[key] = append(l.prune(key, now), now)
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.events, key)
	l.mu.Unlock()
}

// prune drops events older than the window. Caller holds the lock.
func (l *limiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	kept := l.events[key][:0]
	for _, t := range l.events[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.events, key)
		return nil
	}
	l.events[key] = kept
	return kept
}

func (l *limiter) reap() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for key := range l.events {
		l.prune(key, now)
	}
}

// ReapRateLimiters drops expired entries from every auth limiter.
func ReapRateLimiters() {
	for _, l := range []*limiter{loginIPLimiter, loginAccountLimiter, registerIPLimiter, refreshIPLimiter} {
		l.reap()
	}
}

// allowAll writes a 429 with Retry-After and returns false when any of the
// given limiter/key pairs is exhausted. what names the endpoint for the log.
func allowAll(w http.ResponseWriter, r *http.Request, what string, checks ...limitCheck) bool {
	var wait time.Duration
	for _, c := range checks {
		wait = max(wait, c.l.retryAfter(c.key))
	}
	if wait <= 0 {
		return true
	}
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeAPIError(w, http.StatusTooManyRequests, CodeTooManyRequests, "too many attempts, try again later")
	GoLog.Warnf("%s: rate limited %s for %ds", what, clientIP(r), secs)
	return false
}

type limitCheck struct {
	l   *limiter
	key string
}
