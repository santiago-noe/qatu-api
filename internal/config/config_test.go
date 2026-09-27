package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Port != 8080 || cfg.HTTP.ShutdownTimeout != 10*time.Second {
		t.Fatalf("defaults inesperados: %+v", cfg.HTTP)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("APP__ENV", "production")
	t.Setenv("APP__HTTP__PORT", "9090")
	t.Setenv("APP__REDIS__ADDR", "redis:6379")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Port != 9090 || cfg.Redis.Addr != "redis:6379" || !cfg.IsProduction() {
		t.Fatalf("no leyó las variables de entorno: %+v", cfg)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("APP__HTTP__PORT", "0")
	if _, err := Load(); err == nil {
		t.Fatal("se esperaba error con puerto 0")
	}
}
