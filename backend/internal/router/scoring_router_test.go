package router

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/handler"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/internal/service"
)

func newScoringApp(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	cfg := config.Config{
		Port: "0", DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
		CORSOrigins: []string{},
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open scoring database: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tankRepo := repository.NewTankRepository(db)
	measurementRepo := repository.NewMeasurementRepository(db)
	transferRepo := repository.NewTransferRepository(db)
	balanceRepo := repository.NewBalanceRepository(db)
	supportRepo := repository.NewSupportRepository(db)
	authService := service.NewAuthService(supportRepo, cfg.JWTSecret)
	engine := New(log, authService, Handlers{
		Support:     handler.NewSupportHandler(authService, service.NewAuditService(supportRepo)),
		Tank:        handler.NewTankHandler(service.NewTankService(tankRepo)),
		Measurement: handler.NewMeasurementHandler(service.NewMeasurementService(measurementRepo, tankRepo)),
		Transfer:    handler.NewTransferHandler(service.NewTransferService(transferRepo, tankRepo)),
		Balance:     handler.NewBalanceHandler(service.NewBalanceService(balanceRepo, tankRepo, measurementRepo, transferRepo)),
	}, cfg.CORSOrigins)
	return engine, db
}

func scoringLogin(t *testing.T, engine *gin.Engine, email, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login %s failed: %d %s", email, w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return resp.Data.Token
}

func scoringDo(t *testing.T, engine *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestGetMissingTankReturns404(t *testing.T) {
	engine, _ := newScoringApp(t)
	token := scoringLogin(t, engine, "analyst@lng.local", "LngBalance!2026")
	w := scoringDo(t, engine, http.MethodGet, "/api/v1/tanks/999999", token)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing tank, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMissingMeasurementReturns404(t *testing.T) {
	engine, _ := newScoringApp(t)
	token := scoringLogin(t, engine, "analyst@lng.local", "LngBalance!2026")
	w := scoringDo(t, engine, http.MethodGet, "/api/v1/measurements/999999", token)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing measurement, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginWrongEmailReturns401(t *testing.T) {
	engine, _ := newScoringApp(t)
	body, _ := json.Marshal(map[string]string{"email": "nobody@lng.local", "password": "LngBalance!2026"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong email, got %d: %s", w.Code, w.Body.String())
	}
}
