package llm

import (
	"sync"
	"time"
)

type quotaEntry struct {
	used    int
	resetAt time.Time
}

type TokenQuota struct {
	mu      sync.Mutex
	limit   int
	entries map[string]*quotaEntry
}

func NewTokenQuota(limit int) *TokenQuota {
	return &TokenQuota{
		limit:   limit,
		entries: make(map[string]*quotaEntry),
	}
}

func (tq *TokenQuota) CheckAndReserve(key string, estTokens int) bool {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	now := time.Now()
	e, ok := tq.entries[key]
	if !ok || now.After(e.resetAt) {
		e = &quotaEntry{used: 0, resetAt: now.Add(time.Minute)}
		tq.entries[key] = e
	}
	if e.used+estTokens > tq.limit {
		return false
	}
	e.used += estTokens
	return true
}

func (tq *TokenQuota) AddUsage(key string, tokens int) {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	if e, ok := tq.entries[key]; ok {
		e.used += tokens
	}
}

func (tq *TokenQuota) IsExceeded(key string) bool {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	if e, ok := tq.entries[key]; ok {
		return e.used >= tq.limit
	}
	return false
}
