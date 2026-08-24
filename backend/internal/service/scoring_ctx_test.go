package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/dto"
	"lng-boiloff-gas-balance/backend/internal/repository"
)

func TestMeasurementCreateIgnoresStaleCtx(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	svc := NewMeasurementService(repository.NewMeasurementRepository(db), repository.NewTankRepository(db))
	actor := repository.Actor{UserID: 1, Email: "analyst@lng.local", Role: "process_analyst"}
	request := dto.CreateMeasurementRequest{
		TankID: 1, MeasuredAt: timePtr(time.Now().UTC().Add(-time.Hour)),
		LiquidLevelM: 8.2, LiquidTempC: -160.4, VaporPressureKPA: 111, DensityKGM3: 451.8,
		MeasurementUncertaintyPct: 0.28, QualityFlag: "good", SourceNote: "评分计量快照",
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Create(cancelled, request, actor); err == nil {
		t.Fatal("first request with cancelled context should fail")
	}
	second := time.Now().UTC().Add(-3 * time.Hour)
	request.MeasuredAt = &second
	if _, err := svc.Create(context.Background(), request, actor); err != nil {
		t.Fatalf("a fresh request must not be poisoned by the previous cancelled context: %v", err)
	}
}

func timePtr(value time.Time) *time.Time { return &value }
