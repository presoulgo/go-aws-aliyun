package auth

import (
	"strings"
	"sync"
	"time"
)

// Limiter locks a username after too many consecutive failed logins.
type Limiter struct {
	mu          sync.Mutex
	maxFailures int
	lockFor     time.Duration
	entries     map[string]*limitEntry
	now         func() time.Time
}

type limitEntry struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

// NewLimiter locks an account for lockFor after maxFailures failures.
func NewLimiter(maxFailures int, lockFor time.Duration) *Limiter {
	return &Limiter{maxFailures: maxFailures, lockFor: lockFor, entries: map[string]*limitEntry{}, now: time.Now}
}

func key(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

// LockedUntil returns the unlock time if the username is currently locked.
func (l *Limiter) LockedUntil(username string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key(username)]
	if !ok {
		return time.Time{}, false
	}
	if l.now().Before(e.lockedUntil) {
		return e.lockedUntil, true
	}
	return time.Time{}, false
}

// Fail records a failure and returns the failures left before a lock (0 when
// the account just got locked) and the lock expiry.
func (l *Limiter) Fail(username string) (remaining int, lockedUntil time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gc()
	now := l.now()
	k := key(username)
	e, ok := l.entries[k]
	if !ok {
		e = &limitEntry{}
		l.entries[k] = e
	}
	if !e.lockedUntil.IsZero() && !now.Before(e.lockedUntil) {
		// The previous lock expired: start counting afresh.
		e.failures = 0
		e.lockedUntil = time.Time{}
	}
	e.failures++
	e.lastSeen = now
	if e.failures >= l.maxFailures {
		e.lockedUntil = now.Add(l.lockFor)
		return 0, e.lockedUntil
	}
	return l.maxFailures - e.failures, time.Time{}
}

// MaxFailures is the number of consecutive failures that locks a username.
func (l *Limiter) MaxFailures() int { return l.maxFailures }

// Reset clears the failures after a successful login.
func (l *Limiter) Reset(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key(username))
}

// gc drops stale entries; callers hold the lock.
func (l *Limiter) gc() {
	if len(l.entries) < 1024 {
		return
	}
	cutoff := l.now().Add(-24 * time.Hour)
	for k, e := range l.entries {
		if e.lastSeen.Before(cutoff) && l.now().After(e.lockedUntil) {
			delete(l.entries, k)
		}
	}
}
