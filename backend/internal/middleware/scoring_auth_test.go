package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/internal/service"
)

func TestAuthMiddlewarePropagatesRequestCtx(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	auth := service.NewAuthService(repository.NewSupportRepository(db), cfg.JWTSecret)
	token, err := auth.Login(context.Background(), "analyst@lng.local", "LngBalance!2026")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tanks", nil)
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token.Token)
	c.Request = req
	Auth(auth)(c)
	// 请求上下文已取消时，认证必须随请求取消而失败（中止），而不是用后台 context 继续
	if !c.IsAborted() {
		t.Fatal("auth middleware must abort when the request context is cancelled")
	}
}
