package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultCorsOriginsNonNil(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", "file::memory:?cache=shared")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.CORSOrigins == nil {
		t.Fatal("CORSOrigins must not be a nil slice when the env value is empty")
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Fatalf("expected the two default origins, got %v", cfg.CORSOrigins)
	}
}
