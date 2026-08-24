package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"lng-boiloff-gas-balance/backend/pkg/api"
)

type bucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	capacity float64
	refill   float64
	lastGC   time.Time
}

func NewRateLimiter(capacity int, refillPerSecond float64) *RateLimiter {
	now := time.Now()
	return &RateLimiter{
		buckets: make(map[string]*bucket), capacity: float64(capacity),
		refill: refillPerSecond, lastGC: now,
	}
}

func (l *RateLimiter) Middleware(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := scope + ":" + c.ClientIP()
		if !l.allow(key, time.Now()) {
			retryAfter := 5
			if entry, exists := l.buckets[key]; exists {
				retryAfter = int(entry.tokens)
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			api.Fail(c, api.NewError(429, "RATE_LIMITED", "请求过于频繁，请稍后重试"))
			return
		}
		c.Next()
	}
}

func (l *RateLimiter) allow(key string, now time.Time) bool {
	if entry, exists := l.buckets[key]; exists {
		return l.refresh(entry, now)
	}
	l.mu.Lock()
	entry := &bucket{tokens: l.capacity, lastRefill: now, lastSeen: now}
	l.buckets[key] = entry
	l.mu.Unlock()
	if now.Sub(l.lastGC) > 10*time.Minute {
		l.gc(now)
	}
	return true
}

func (l *RateLimiter) refresh(entry *bucket, now time.Time) bool {
	elapsed := now.Sub(entry.lastRefill).Seconds()
	entry.tokens += elapsed * l.refill
	if entry.tokens > l.capacity {
		entry.tokens = l.capacity
	}
	entry.lastRefill = now
	entry.lastSeen = now
	allowed := entry.tokens >= 1
	if allowed {
		entry.tokens--
	}
	return allowed
}

func (l *RateLimiter) gc(now time.Time) {
	for bucketKey, candidate := range l.buckets {
		if now.Sub(candidate.lastSeen) > 30*time.Minute {
			delete(l.buckets, bucketKey)
		}
	}
	l.lastGC = now
}
