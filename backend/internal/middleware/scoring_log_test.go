package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

type captureHandler struct {
	mu      sync.Mutex
	records []map[string]any
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	entry := map[string]any{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		entry[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, entry)
	h.mu.Unlock()
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) find(msg string) (map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, record := range h.records {
		if record["msg"] == msg {
			return record, true
		}
	}
	return nil, false
}

func TestAccessLogIncludesRequestID(t *testing.T) {
	capture := &captureHandler{}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequestID())
	engine.Use(AccessLog(slog.New(capture)))
	engine.Use(Recovery(slog.New(capture)))
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "req-123-abc")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	record, ok := capture.find("http_request")
	if !ok {
		t.Fatal("access log record missing")
	}
	if got, _ := record["request_id"].(string); got != "req-123-abc" {
		t.Fatalf("access log must carry the client request id, got %q", got)
	}
}

func TestRecoveryLogIncludesRequestID(t *testing.T) {
	capture := &captureHandler{}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequestID())
	engine.Use(AccessLog(slog.New(capture)))
	engine.Use(Recovery(slog.New(capture)))
	engine.GET("/boom", func(c *gin.Context) { panic("boom") })
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("X-Request-ID", "req-panic-007")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	record, ok := capture.find("panic_recovered")
	if !ok {
		t.Fatal("panic recovery log record missing")
	}
	if got, _ := record["request_id"].(string); got != "req-panic-007" {
		t.Fatalf("panic log must correlate the client request id, got %q", got)
	}
	access, ok := capture.find("http_request")
	if !ok {
		t.Fatal("access log record missing")
	}
	if got, _ := access["status"].(int64); got != 500 {
		t.Fatalf("access log must record the real 500 status, got %v", got)
	}
}
func TestAccessLogIncludesRole(t *testing.T) {
	capture := &captureHandler{}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequestID())
	engine.Use(AccessLog(slog.New(capture)))
	engine.GET("/", func(c *gin.Context) {
		c.Set("role", "admin")
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "req-role-001")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	record, ok := capture.find("http_request")
	if !ok {
		t.Fatal("access log record missing")
	}
	if got, _ := record["role"].(string); got != "admin" {
		t.Fatalf("access log must include the authenticated role, got %q", got)
	}
}
