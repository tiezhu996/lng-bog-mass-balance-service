package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"lng-boiloff-gas-balance/backend/internal/config"
	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/internal/repository"
)

func TestLoginTokenCarriesCorrectRole(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	auth := NewAuthService(repository.NewSupportRepository(db), cfg.JWTSecret)
	result, err := auth.Login(context.Background(), "admin@lng.local", "LngBalance!2026")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	parsed, err := jwt.ParseWithClaims(result.Token, &Claims{}, func(token *jwt.Token) (any, error) {
		return []byte(cfg.JWTSecret), nil
	}, jwt.WithIssuer("lng-boiloff-gas-balance"))
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		t.Fatalf("unexpected claims type")
	}
	if claims.Role != "admin" {
		t.Fatalf("token must embed the login role, got %q", claims.Role)
	}
	_ = time.Now()
}

func TestParseTokenReflectsCurrentRole(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	auth := NewAuthService(repository.NewSupportRepository(db), cfg.JWTSecret)
	result, err := auth.Login(context.Background(), "analyst@lng.local", "LngBalance!2026")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	var user model.User
	if err := db.Model(&model.User{}).Where("email = ?", "analyst@lng.local").First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("role", "reviewer").Error; err != nil {
		t.Fatalf("update role: %v", err)
	}
	claims, err := auth.ParseToken(context.Background(), result.Token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if claims.Role != "reviewer" {
		t.Fatalf("parse token must refresh role from database, got %q", claims.Role)
	}
}
func TestParseTokenRejectsDisabledUser(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	auth := NewAuthService(repository.NewSupportRepository(db), cfg.JWTSecret)
	result, err := auth.Login(context.Background(), "analyst@lng.local", "LngBalance!2026")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	var user model.User
	if err := db.Model(&model.User{}).Where("email = ?", "analyst@lng.local").First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("active", false).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	_, err = auth.ParseToken(context.Background(), result.Token)
	if err == nil {
		t.Fatal("parse token must reject a disabled user")
	}
}
func TestLoginTokenCarriesCorrectEmail(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	auth := NewAuthService(repository.NewSupportRepository(db), cfg.JWTSecret)
	result, err := auth.Login(context.Background(), "admin@lng.local", "LngBalance!2026")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	parsed, err := jwt.ParseWithClaims(result.Token, &Claims{}, func(token *jwt.Token) (any, error) {
		return []byte(cfg.JWTSecret), nil
	}, jwt.WithIssuer("lng-boiloff-gas-balance"))
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		t.Fatalf("unexpected claims type")
	}
	if claims.Email != "admin@lng.local" {
		t.Fatalf("token must embed the login email, got %q", claims.Email)
	}
}
func TestMeRejectsDisabledUser(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite", DBDSN: "file:" + t.Name() + "?mode=memory&cache=shared",
		DBAutoMigrate: true, SeedData: true, JWTSecret: strings.Repeat("s", 32),
	}
	db, err := config.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	auth := NewAuthService(repository.NewSupportRepository(db), cfg.JWTSecret)
	var user model.User
	if err := db.Model(&model.User{}).Where("email = ?", "analyst@lng.local").First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("active", false).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if _, err := auth.Me(context.Background(), user.ID); err == nil {
		t.Fatal("Me must reject a disabled user")
	}
}
