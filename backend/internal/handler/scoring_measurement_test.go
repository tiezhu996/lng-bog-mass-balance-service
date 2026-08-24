package handler

import (
	"bytes"
	"context"
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

func TestMeasurementHandlerPassesRequestCtx(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	svc := service.NewMeasurementService(repository.NewMeasurementRepository(db), repository.NewTankRepository(db))
	h := NewMeasurementHandler(svc)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body, _ := json.Marshal(map[string]any{
		"tank_id": 1, "measured_at": time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339),
		"liquid_level_m": 8.2, "liquid_temp_c": -160.4, "vapor_pressure_kpa": 111,
		"density_kgm3": 451.8, "measurement_uncertainty_pct": 0.28,
		"quality_flag": "good", "source_note": "评分计量快照",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/measurements", bytes.NewReader(body))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Set("user_id", uint(1))
	c.Set("email", "analyst@lng.local")
	c.Set("role", "process_analyst")
	h.Create(c)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("handler must propagate the cancelled request context (expected 500), got %d: %s", w.Code, w.Body.String())
	}
}
