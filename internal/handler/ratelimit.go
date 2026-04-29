package handler

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu       sync.Mutex
	entries  map[string]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		entries: make(map[string]time.Time),
	}
}

func (rl *rateLimiter) allow(key string, window time.Duration) bool {
	now := time.Now()
	cutoff := now.Add(-window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if last, ok := rl.entries[key]; ok && last.After(cutoff) {
		return false
	}

	rl.entries[key] = now

	// 惰性清理过期条目，防止内存泄漏
	for k, v := range rl.entries {
		if v.Before(cutoff) {
			delete(rl.entries, k)
		}
	}

	return true
}
