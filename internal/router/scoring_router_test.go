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

func scoringTankPayload(curve []float64) map[string]any {
	return map[string]any{
		"tank_code": "TK-SCORE", "name": "评分储罐", "nominal_capacity_m3": 180000,
		"min_level_m": 0, "max_level_m": 12, "reference_density_kgm3": 452,
		"reference_temperature_c": -160, "thermal_expansion_per_c": 0.0035,
		"capacity_curve": curve, "coefficient_version": "CV-SCORE", "tank_status": "active",
	}
}


func scoringCurve(raw json.RawMessage) []float64 {
	var arr []float64
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var obj struct {
		Coefficients []float64 `json:"coefficients"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Coefficients
	}
	return nil
}

func scoringPost(t *testing.T, engine *gin.Engine, path, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestCreateTankKeepsCurveOrder(t *testing.T) {
	engine, _ := newScoringApp(t)
	token := scoringLogin(t, engine)
	curve := []float64{0, 14000, 50}
	w := scoringPost(t, engine, "/api/v1/tanks", token, scoringTankPayload(curve))
	if w.Code != http.StatusCreated {
		t.Fatalf("create tank failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			CapacityCurveJSON json.RawMessage `json:"capacity_curve_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	got := scoringCurve(resp.Data.CapacityCurveJSON)
	if len(got) != len(curve) {
		t.Fatalf("curve length changed: got %v", got)
	}
	for i := range curve {
		if got[i] != curve[i] {
			t.Fatalf("curve order changed: got %v want %v", got, curve)
		}
	}
}

func TestUpdateTankKeepsCurveOrder(t *testing.T) {
	engine, _ := newScoringApp(t)
	token := scoringLogin(t, engine)
	w := scoringPost(t, engine, "/api/v1/tanks", token, scoringTankPayload([]float64{0, 14000, 50}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create tank failed: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	payload := scoringTankPayload([]float64{5, 12000, 80})
	payload["version"] = 1
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tanks/"+strconv.FormatUint(uint64(created.Data.ID), 10), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	uw := httptest.NewRecorder()
	engine.ServeHTTP(uw, req)
	if uw.Code != http.StatusOK {
		t.Fatalf("update tank failed: %d %s", uw.Code, uw.Body.String())
	}
	var resp struct {
		Data struct {
			CapacityCurveJSON json.RawMessage `json:"capacity_curve_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(uw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	want := []float64{5, 12000, 80}
	got := scoringCurve(resp.Data.CapacityCurveJSON)
	if len(got) != len(want) {
		t.Fatalf("curve length changed: got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("curve order changed after update: got %v want %v", got, want)
		}
	}
}
