package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/internal/service"
)

func TestLoginHandlerResponseNoRace(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	supportRepo := repository.NewSupportRepository(db)
	auth := service.NewAuthService(supportRepo, cfg.JWTSecret)
	h := NewSupportHandler(auth, service.NewAuditService(supportRepo))
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"email": "admin@lng.local", "password": "LngBalance!2026"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Set("request_id", "req-login-race")
	h.Login(c)
	time.Sleep(80 * time.Millisecond)
	if w.Code != http.StatusOK {
		t.Fatalf("login handler failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			User struct {
				Role string `json:"role"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.User.Role != "admin" {
		t.Fatalf("login response must carry the real role, got %q", resp.Data.User.Role)
	}
}
