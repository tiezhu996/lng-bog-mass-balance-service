package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

func TestParseTokenDeletedUserReturns401(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	auth := NewAuthService(repository.NewSupportRepository(db), cfg.JWTSecret)
	hash, err := bcrypt.GenerateFromPassword([]byte("LngBalance!2026"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := model.User{Email: "temp@lng.local", DisplayName: "临时用户", PasswordHash: string(hash), Role: "process_analyst", Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create temp user: %v", err)
	}
	token, err := auth.Login(context.Background(), "temp@lng.local", "LngBalance!2026")
	if err != nil {
		t.Fatalf("login temp user: %v", err)
	}
	if err := db.Delete(&model.User{}, user.ID).Error; err != nil {
		t.Fatalf("delete temp user: %v", err)
	}
	_, err = auth.ParseToken(context.Background(), token.Token)
	var appErr *api.Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 401 {
		t.Fatalf("expected typed 401 for deleted user, got %v", err)
	}
}
