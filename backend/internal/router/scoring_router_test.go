package router

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/handler"
	"lng-boiloff-gas-balance/backend/internal/model"
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

func scoringLogin(t *testing.T, engine *gin.Engine) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": "analyst@lng.local", "password": "LngBalance!2026"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
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

func scoringTransfer(t *testing.T, engine *gin.Engine, token string, tankID uint, start, end time.Time, mass float64, status string) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"tank_id": tankID, "operation_type": "inflow",
		"start_at": start.UTC().Format(time.RFC3339), "end_at": end.UTC().Format(time.RFC3339),
		"measured_mass_kg": mass, "measurement_uncertainty_pct": 0.2,
		"counterparty_ref": "JETTY-SCORE-01", "operation_status": status,
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestCreateOverlappingTransferReturnsErrorAndNotPersisted(t *testing.T) {
	engine, db := newScoringApp(t)
	token := scoringLogin(t, engine)
	now := time.Now().UTC().Truncate(time.Second)
	w1 := scoringTransfer(t, engine, token, 1, now.Add(-3*time.Hour), now.Add(-2*time.Hour), 120000, "confirmed")
	if w1.Code != http.StatusCreated {
		t.Fatalf("create first transfer failed: %d %s", w1.Code, w1.Body.String())
	}
	w2 := scoringTransfer(t, engine, token, 1, now.Add(-150*time.Minute), now.Add(-90*time.Minute), 90000, "confirmed")
	if w2.Code != http.StatusConflict {
		t.Fatalf("overlapping transfer must be rejected with 409, got %d: %s", w2.Code, w2.Body.String())
	}
	var total int64
	if err := db.Model(&model.TransferOperation{}).Count(&total).Error; err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if total != 4 {
		t.Fatalf("overlapping transfer must not be persisted, total=%d (expected 3 seed + 1 created)", total)
	}
}

func TestTransitionInvalidKeepsStatusAndReturnsError(t *testing.T) {
	engine, db := newScoringApp(t)
	token := scoringLogin(t, engine)
	now := time.Now().UTC().Truncate(time.Second)
	w := scoringTransfer(t, engine, token, 1, now.Add(-6*time.Hour), now.Add(-5*time.Hour), 110000, "confirmed")
	if w.Code != http.StatusCreated {
		t.Fatalf("create confirmed transfer failed: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"target_status": "confirmed", "version": 1, "reason": "重复确认没有意义"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/"+strconv.FormatUint(uint64(created.Data.ID), 10)+"/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	sw := httptest.NewRecorder()
	engine.ServeHTTP(sw, req)
	if sw.Code != http.StatusConflict {
		t.Fatalf("confirmed -> confirmed must be rejected with 409, got %d: %s", sw.Code, sw.Body.String())
	}
	var item model.TransferOperation
	if err := db.First(&item, created.Data.ID).Error; err != nil {
		t.Fatalf("load transfer: %v", err)
	}
	if item.OperationStatus != "confirmed" {
		t.Fatalf("transfer status must stay confirmed, got %s", item.OperationStatus)
	}
}
