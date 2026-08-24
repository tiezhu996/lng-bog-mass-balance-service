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
		buckets:  make(map[string]*bucket),
		capacity: float64(capacity),
		refill:   refillPerSecond,
		lastGC:   now,
	}
}

func (l *RateLimiter) Middleware(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := scope + ":" + c.ClientIP()
		allowed, retryAfter := l.take(key, time.Now())
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			api.Fail(c, api.NewError(429, "RATE_LIMITED", "请求过于频繁，请稍后重试"))
			return
		}
		c.Next()
	}
}

// allow reports whether key may proceed. It is retained for tests; production
// code uses take so the Retry-After hint is computed atomically with the
// decision rather than re-read from the map without the lock.
func (l *RateLimiter) allow(key string, now time.Time) bool {
	allowed, _ := l.take(key, now)
	return allowed
}

// take applies the token-bucket algorithm under a single lock so concurrent
// callers cannot interleave reads and writes of the same bucket fields. When
// the request is denied it returns a conservative Retry-After in seconds.
func (l *RateLimiter) take(key string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.buckets[key]
	if !exists {
		entry = &bucket{tokens: l.capacity, lastRefill: now, lastSeen: now}
		l.buckets[key] = entry
	}

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
		if now.Sub(l.lastGC) > 10*time.Minute {
			l.gc(now)
		}
		return true, 0
	}

	retryAfter := 1
	if l.refill > 0 {
		retryAfter = int((1-entry.tokens)/l.refill) + 1
	}
	return false, retryAfter
}

func (l *RateLimiter) gc(now time.Time) {
	for bucketKey, candidate := range l.buckets {
		if now.Sub(candidate.lastSeen) > 30*time.Minute {
			delete(l.buckets, bucketKey)
		}
	}
	l.lastGC = now
}
