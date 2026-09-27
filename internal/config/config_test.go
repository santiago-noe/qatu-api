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
	if cfg.Session.TTL != 30*24*time.Hour || cfg.Session.RenewAfter != 24*time.Hour || cfg.Session.CookieName != "qatu_session" {
		t.Fatalf("sesión inesperada: %+v", cfg.Session)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("APP__ENV", "production")
	t.Setenv("APP__SECURITY__CODE_SECRET", "secreto-de-prueba")
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

func TestProductionRequiresCodeSecret(t *testing.T) {
	t.Setenv("APP__ENV", "production")
	if _, err := Load(); err == nil {
		t.Fatal("en producción sin APP__SECURITY__CODE_SECRET debe fallar")
	}
}

func TestCodesDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Codes.TTL != 15*time.Minute || cfg.Codes.MaxAttempts != 5 || cfg.Codes.ResendCooldown != time.Minute {
		t.Fatalf("códigos inesperados: %+v", cfg.Codes)
	}
	if cfg.Security.CodeSecret == "" || cfg.SMTP.Port != 1025 {
		t.Fatalf("en desarrollo hay valores por defecto: %+v %+v", cfg.Security, cfg.SMTP)
	}
}

func TestRateLimitDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.LoginPerAccount.Max != 5 || cfg.Limits.LoginPerAccount.Window != 15*time.Minute ||
		cfg.Limits.AuthPerIP.Max <= 0 || !cfg.HTTP.TrustPrivateProxies {
		t.Fatalf("límites inesperados: %+v %+v", cfg.Limits, cfg.HTTP)
	}
}
