package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"lng-boiloff-gas-balance/backend/internal/middleware"
)

func newScoringCorsEngine(origins []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.Recovery(slog.New(slog.NewTextHandler(io.Discard, nil))))
	engine.Use(cors(origins))
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	return engine
}

func TestCorsAllowsConfiguredOrigin(t *testing.T) {
	engine := newScoringCorsEngine([]string{"http://allowed.example"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://allowed.example")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for allowed origin, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://allowed.example" {
		t.Fatalf("expected Access-Control-Allow-Origin header, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("expected Vary: Origin, got %q", got)
	}
}

func TestCorsPreflightHeadersPresent(t *testing.T) {
	engine := newScoringCorsEngine([]string{"http://allowed.example"})
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://allowed.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("expected Access-Control-Allow-Headers header on preflight")
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods header on preflight")
	}
}
