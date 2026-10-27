package middleware

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

type KeyedLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	r        rate.Limit
	burst    int
}

func NewKeyedLimiter(r rate.Limit, burst int) *KeyedLimiter {
	kl := &KeyedLimiter{
		limiters: make(map[string]*limiterEntry),
		r:        r,
		burst:    burst,
	}
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			kl.cleanup()
		}
	}()
	return kl
}

func (kl *KeyedLimiter) Allow(key string) bool {
	kl.mu.Lock()
	e, ok := kl.limiters[key]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(kl.r, kl.burst)}
		kl.limiters[key] = e
	}
	e.lastSeen = time.Now()
	kl.mu.Unlock()
	return e.lim.Allow()
}

func (kl *KeyedLimiter) cleanup() {
	kl.mu.Lock()
	defer kl.mu.Unlock()
	cutoff := time.Now().Add(-time.Hour)
	for k, e := range kl.limiters {
		if e.lastSeen.Before(cutoff) {
			delete(kl.limiters, k)
		}
	}
}

func RateLimitMiddleware(kl *KeyedLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.RemoteAddr
		}
		if !kl.Allow(key) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
