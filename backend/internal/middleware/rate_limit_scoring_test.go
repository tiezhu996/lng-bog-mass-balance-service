package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiterConcurrentAdmissionNoRace(t *testing.T) {
	limiter := NewRateLimiter(4, 0)
	if !limiter.allow("shared", time.Now()) {
		t.Fatal("first request should pass")
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var allowed atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 200; j++ {
				if limiter.allow("shared", time.Now()) {
					allowed.Add(1)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if allowed.Load() > 4 {
		t.Fatalf("rate limit bypassed: allowed %d exceeds capacity 4", allowed.Load())
	}
}

func TestRateLimiterGCRunsUnderLock(t *testing.T) {
	limiter := NewRateLimiter(4, 0)
	limiter.lastGC = time.Now().Add(-11 * time.Minute)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			for j := 0; j < 50; j++ {
				_ = limiter.allow(fmt.Sprintf("k-%d-%d", n, j), time.Now())
			}
		}(i)
	}
	close(start)
	wg.Wait()
}

func TestRateLimiterFirstRequestConsumesToken(t *testing.T) {
	limiter := NewRateLimiter(1, 0)
	if !limiter.allow("k", time.Now()) {
		t.Fatal("first request should pass")
	}
	if limiter.allow("k", time.Now()) {
		t.Fatal("second request within the same window must be blocked (capacity 1)")
	}
}

func TestRateLimiterMiddlewareRetryAfterReadNoRace(t *testing.T) {
	limiter := NewRateLimiter(1, 0)
	_ = limiter.allow("scope:203.0.113.7", time.Now())
	start := make(chan struct{})
	var wg sync.WaitGroup
	// 一组请求不断创建新桶（写 map），另一组走 Middleware 读取限流结果
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			for j := 0; j < 40; j++ {
				_ = limiter.allow(fmt.Sprintf("scope:10.0.0.%d-%d", n, j), time.Now())
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 40; j++ {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/balances", nil)
				req.RemoteAddr = "203.0.113.7:53000"
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = req
				limiter.Middleware("scope")(c)
			}
		}()
	}
	close(start)
	wg.Wait()
}
