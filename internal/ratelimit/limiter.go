package ratelimit

import (
	"sync"
	"time"
)

// Limiter is an in-memory sliding-window rate limiter (parity with CF bindings
// in dev / single-process oma-server).
type Limiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
}

// NewLimiter returns a ready limiter.
func NewLimiter() *Limiter {
	return &Limiter{windows: make(map[string][]time.Time)}
}

// Allow records a hit for key when under limit within window.
// Returns false when the limit is exceeded.
func (l *Limiter) Allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-window)

	l.mu.Lock()
	defer l.mu.Unlock()

	hits := l.windows[key]
	filtered := make([]time.Time, 0, len(hits))
	for _, ts := range hits {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	if len(filtered) >= limit {
		l.windows[key] = filtered
		return false
	}
	filtered = append(filtered, now)
	l.windows[key] = filtered
	return true
}

var (
	parityMu      sync.Mutex
	parityWindows = make(map[string][]time.Time)
)

// ClearParityWindows resets in-memory windows used by IsRateLimited tests.
func ClearParityWindows() {
	parityMu.Lock()
	defer parityMu.Unlock()
	parityWindows = make(map[string][]time.Time)
}

// IsRateLimited mirrors open-managed-agents isRateLimited for unit tests.
func IsRateLimited(
	key string,
	limit int,
	windowMs int64,
	now time.Time,
) bool {
	if limit <= 0 {
		return false
	}
	cutoff := now.Add(-time.Duration(windowMs) * time.Millisecond)

	parityMu.Lock()
	defer parityMu.Unlock()

	hits := parityWindows[key]
	filtered := make([]time.Time, 0, len(hits))
	for _, ts := range hits {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	if len(filtered) >= limit {
		parityWindows[key] = filtered
		return true
	}
	filtered = append(filtered, now)
	parityWindows[key] = filtered
	return false
}
