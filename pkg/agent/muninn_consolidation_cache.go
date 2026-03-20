package agent

import (
	"strings"
	"sync"
	"time"
)

const (
	muninnRecallCacheTTL               = 2 * time.Minute
	muninnRecallCacheMaxHistoryEntries = 8
)

type muninnRecallCache struct {
	mu      sync.Mutex
	entries map[string]muninnRecallCacheEntry
	now     func() time.Time
}

type muninnRecallCacheEntry struct {
	plan        MuninnProxyPlan
	result      MuninnProxyResult
	queryTokens map[string]struct{}
	createdAt   time.Time
	lastUsedAt  time.Time
	lastReason  string
}

func newMuninnRecallCache() *muninnRecallCache {
	return &muninnRecallCache{
		entries: make(map[string]muninnRecallCacheEntry),
		now:     time.Now,
	}
}

func (c *muninnRecallCache) lookup(plan MuninnProxyPlan) (MuninnProxyResult, string, bool) {
	if c == nil {
		return MuninnProxyResult{}, "cache disabled", false
	}
	plan = plan.Normalized()
	if plan.SessionKey == "" || strings.TrimSpace(plan.Query) == "" {
		return MuninnProxyResult{}, "cache key unavailable", false
	}
	now := c.now()
	queryTokens := muninnRecallQueryTokens(plan.Query)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[plan.SessionKey]
	if !ok {
		return MuninnProxyResult{}, "no cached recall", false
	}
	if now.Sub(entry.createdAt) > muninnRecallCacheTTL {
		delete(c.entries, plan.SessionKey)
		return MuninnProxyResult{}, "cached recall expired", false
	}
	if entry.plan.Vault != plan.Vault {
		delete(c.entries, plan.SessionKey)
		return MuninnProxyResult{}, "vault changed", false
	}
	if entry.plan.Channel != plan.Channel {
		delete(c.entries, plan.SessionKey)
		return MuninnProxyResult{}, "channel changed", false
	}
	if !muninnRecallQueriesRelated(queryTokens, entry.queryTokens) {
		delete(c.entries, plan.SessionKey)
		return MuninnProxyResult{}, "material context changed", false
	}

	entry.lastUsedAt = now
	entry.lastReason = "closely related turn"
	c.entries[plan.SessionKey] = entry

	result := cloneMuninnProxyResult(entry.result)
	result.Status = MuninnProxyStatusCacheHit
	result.Reason = "reused cached recall for closely related turn"
	result.Duration = 0
	return result, entry.lastReason, true
}

func (c *muninnRecallCache) store(plan MuninnProxyPlan, result MuninnProxyResult) {
	if c == nil {
		return
	}
	plan = plan.Normalized()
	if plan.SessionKey == "" || strings.TrimSpace(plan.Query) == "" {
		return
	}
	if result.Status != MuninnProxyStatusHit {
		c.invalidate(plan.SessionKey)
		return
	}
	now := c.now()
	entry := muninnRecallCacheEntry{
		plan:        plan,
		result:      cloneMuninnProxyResult(result),
		queryTokens: muninnRecallQueryTokens(plan.Query),
		createdAt:   now,
		lastUsedAt:  now,
		lastReason:  "fresh recall result",
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= muninnRecallCacheMaxHistoryEntries {
		c.evictOldestLocked()
	}
	c.entries[plan.SessionKey] = entry
}

func (c *muninnRecallCache) invalidate(sessionKey string) {
	if c == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, strings.TrimSpace(sessionKey))
}

func (c *muninnRecallCache) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, entry := range c.entries {
		if oldestKey == "" || entry.lastUsedAt.Before(oldest) {
			oldestKey = key
			oldest = entry.lastUsedAt
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func muninnRecallQueryTokens(query string) map[string]struct{} {
	tokens := make(map[string]struct{})
	ignore := map[string]struct{}{
		"user": {}, "request": {}, "conversation": {}, "channel": {}, "chat": {},
		"current": {}, "sender": {}, "session": {}, "key": {}, "claweb": {},
		"room": {}, "cron": {},
	}
	for _, token := range strings.Fields(strings.ToLower(query)) {
		token = strings.Trim(token, ".,!?;:'\"()[]{}")
		if len(token) < 4 {
			continue
		}
		if _, drop := ignore[token]; drop {
			continue
		}
		tokens[token] = struct{}{}
	}
	return tokens
}

func muninnRecallQueriesRelated(current, previous map[string]struct{}) bool {
	if len(current) == 0 || len(previous) == 0 {
		return false
	}
	if len(current) != len(previous) {
		return false
	}
	if len(current) == len(previous) {
		allEqual := true
		for token := range current {
			if _, ok := previous[token]; !ok {
				allEqual = false
				break
			}
		}
		if allEqual {
			return true
		}
	}
	return false
}

func cloneMuninnProxyResult(result MuninnProxyResult) MuninnProxyResult {
	cloned := result
	if len(result.Items) > 0 {
		cloned.Items = append([]MuninnProxyMemoryItem(nil), result.Items...)
	}
	return cloned
}
