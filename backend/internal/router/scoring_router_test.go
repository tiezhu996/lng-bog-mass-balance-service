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

func scoringCreateTransfer(t *testing.T, engine *gin.Engine, token string, start, end time.Time, status string) uint {
	t.Helper()
	payload := map[string]any{
		"tank_id": 1, "operation_type": "inflow",
		"start_at": start.UTC().Format(time.RFC3339), "end_at": end.UTC().Format(time.RFC3339),
		"measured_mass_kg": 100000, "measurement_uncertainty_pct": 0.2,
		"counterparty_ref": "JETTY-SCORE-02", "operation_status": status,
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create transfer failed: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return created.Data.ID
}

func TestCancelConfirmedTransferSucceedsAndPersists(t *testing.T) {
	engine, db := newScoringApp(t)
	token := scoringLogin(t, engine)
	now := time.Now().UTC().Truncate(time.Second)
	id := scoringCreateTransfer(t, engine, token, now.Add(-4*time.Hour), now.Add(-3*time.Hour), "confirmed")
	body, _ := json.Marshal(map[string]any{"target_status": "cancelled", "version": 1, "reason": "上游计量表计故障，需要作废"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/"+strconv.FormatUint(uint64(id), 10)+"/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel confirmed transfer failed: %d %s", w.Code, w.Body.String())
	}
	var item model.TransferOperation
	if err := db.First(&item, id).Error; err != nil {
		t.Fatalf("load transfer: %v", err)
	}
	if item.OperationStatus != "cancelled" {
		t.Fatalf("expected cancelled after cancel, got %s", item.OperationStatus)
	}
}

func TestCancelledListExcludesConfirmed(t *testing.T) {
	engine, db := newScoringApp(t)
	token := scoringLogin(t, engine)
	now := time.Now().UTC().Truncate(time.Second)
	scoringCreateTransfer(t, engine, token, now.Add(-9*time.Hour), now.Add(-8*time.Hour), "confirmed")
	// 直接落库一条已取消记录，模拟历史取消
	cancelled := model.TransferOperation{
		TankID: 1, OperationType: "outflow", StartAt: now.Add(-14 * time.Hour), EndAt: now.Add(-13 * time.Hour),
		MeasuredMassKG: 60000, MeasurementUncertaintyPct: 0.25, CounterpartyRef: "SENDOUT-SCORE-01",
		OperationStatus: "cancelled", Version: 2,
	}
	if err := db.Create(&cancelled).Error; err != nil {
		t.Fatalf("seed cancelled transfer: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transfers?status=cancelled", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list cancelled failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []struct {
			OperationStatus string `json:"operation_status"`
		} `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if resp.Meta.Total != 1 {
		t.Fatalf("cancelled list must only contain cancelled transfers, total=%d", resp.Meta.Total)
	}
	for _, item := range resp.Data {
		if item.OperationStatus != "cancelled" {
			t.Fatalf("cancelled list contains %s", item.OperationStatus)
		}
	}
}
func TestCreateDraftTransferSucceeds(t *testing.T) {
	engine, _ := newScoringApp(t)
	token := scoringLogin(t, engine)
	now := time.Now().UTC().Truncate(time.Second)
	payload := map[string]any{
		"tank_id": 1, "operation_type": "inflow",
		"start_at": now.Add(-2 * time.Hour).UTC().Format(time.RFC3339), "end_at": now.Add(-time.Hour).UTC().Format(time.RFC3339),
		"measured_mass_kg": 95000, "measurement_uncertainty_pct": 0.2,
		"counterparty_ref": "JETTY-SCORE-03", "operation_status": "draft",
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("draft transfer must be accepted, got %d: %s", w.Code, w.Body.String())
	}
}
