package auth

import (
	"sync"
	"time"
)

type limitEntry struct {
	failures     int
	firstFailure time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

// Limiter implements a small in-memory login throttle. It is deliberately
// simple: the console is a single-node service and the goal is to stop
// trivial password spraying, not to be a global rate limiter.
type Limiter struct {
	mu      sync.Mutex
	entries map[string]*limitEntry
	max     int
}

func NewLimiter() *Limiter {
	return &Limiter{entries: make(map[string]*limitEntry), max: 20000}
}

// Allow reserves the attempt before expensive work. Otherwise a burst of
// concurrent requests can all pass the check before the first failure lands.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.cleanupLocked(now)
	e, ok := l.entries[key]
	if !ok {
		l.entries[key] = &limitEntry{lastSeen: now}
		return true
	}
	e.lastSeen = now
	return !e.blockedUntil.After(now)
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.cleanupLocked(now)

	e, ok := l.entries[key]
	if !ok {
		e = &limitEntry{}
		l.entries[key] = e
	}
	if e.firstFailure.IsZero() {
		e.firstFailure = now
	}
	if now.Sub(e.firstFailure) > 15*time.Minute {
		e.failures = 0
		e.firstFailure = now
	}
	e.failures++
	e.lastSeen = now

	if e.failures >= 5 {
		shift := e.failures - 5
		if shift > 6 {
			shift = 6
		}
		block := 5 * time.Minute * time.Duration(1<<shift)
		if block > time.Hour {
			block = time.Hour
		}
		e.blockedUntil = now.Add(block)
	}
}

func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Limiter) cleanupLocked(now time.Time) {
	if len(l.entries) < l.max {
		for k, e := range l.entries {
			if now.Sub(e.lastSeen) > 24*time.Hour && e.blockedUntil.Before(now) {
				delete(l.entries, k)
			}
		}
		return
	}
	for k := range l.entries {
		delete(l.entries, k)
		break
	}
}
