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
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/constants"
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

func scoringDo(t *testing.T, engine *gin.Engine, method, path string, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func seedBalanceRun(t *testing.T, db *gorm.DB, status constants.BalanceStatus) uint {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	run := model.BalanceRun{
		TankID: 1, PeriodStart: now.Add(-48 * time.Hour), PeriodEnd: now.Add(-24 * time.Hour),
		BalanceStatus: status, InputSnapshotJSON: datatypes.JSON(`{}`), EvidenceJSON: datatypes.JSON(`{}`),
		CoefficientVersion: "CV-T", Version: 1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("seed balance run: %v", err)
	}
	return run.ID
}

func TestReviewRejectActuallyRejects(t *testing.T) {
	engine, db := newScoringApp(t)
	runID := seedBalanceRun(t, db, constants.BalancePendingReview)
	token := scoringLogin(t, engine, "reviewer@lng.local", "LngBalance!2026")
	w := scoringDo(t, engine, http.MethodPost, "/api/v1/balances/"+itoa(runID)+"/review", token, map[string]any{
		"target_status": "rejected", "version": 1, "review_note": "计量不确定度过大，驳回",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("review reject failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			BalanceStatus string `json:"balance_status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode review response: %v", err)
	}
	if resp.Data.BalanceStatus != string(constants.BalanceRejected) {
		t.Fatalf("expected rejected, got %s", resp.Data.BalanceStatus)
	}
}

func TestRejectedListExcludesAccepted(t *testing.T) {
	engine, db := newScoringApp(t)
	seedBalanceRun(t, db, constants.BalanceAccepted)
	seedBalanceRun(t, db, constants.BalanceRejected)
	token := scoringLogin(t, engine, "analyst@lng.local", "LngBalance!2026")
	w := scoringDo(t, engine, http.MethodGet, "/api/v1/balances?status=rejected", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list rejected failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []struct {
			BalanceStatus string `json:"balance_status"`
		} `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if resp.Meta.Total != 1 {
		t.Fatalf("expected exactly 1 rejected run, got total=%d", resp.Meta.Total)
	}
	for _, item := range resp.Data {
		if item.BalanceStatus != string(constants.BalanceRejected) {
			t.Fatalf("rejected list contains %s", item.BalanceStatus)
		}
	}
}

func itoa(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
