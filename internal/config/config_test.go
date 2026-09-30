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

// productionSecrets son las variables sin valor por defecto que exige producción, en orden.
var productionSecrets = []string{
	"APP__SECURITY__CODE_SECRET", "APP__SECURITY__LOCATION_SECRET", "APP__TURNSTILE__SECRET", "APP__GOOGLE__CLIENT_ID",
	"APP__GOOGLE__CLIENT_SECRET", "APP__STORAGE__ACCESS_KEY", "APP__STORAGE__SECRET_KEY",
}

// clearSecrets aísla la prueba del .env que exporta el Makefile (una variable vacía cuenta como no definida).
func clearSecrets(t *testing.T) {
	for _, env := range productionSecrets {
		t.Setenv(env, "")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("APP__ENV", "production")
	for _, env := range productionSecrets {
		t.Setenv(env, "valor-de-prueba")
	}
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

func TestProductionRequiresSecrets(t *testing.T) {
	clearSecrets(t)
	t.Setenv("APP__ENV", "production")
	// Se agregan de a uno: mientras falte alguno, no arranca.
	for _, env := range productionSecrets {
		if _, err := Load(); err == nil {
			t.Fatalf("en producción sin %s debe fallar", env)
		}
		t.Setenv(env, "valor-de-prueba")
	}
	if _, err := Load(); err != nil {
		t.Fatalf("con todos los secretos arranca: %v", err)
	}
}

func TestGoogleNeedsSecretWithClientID(t *testing.T) {
	clearSecrets(t)
	t.Setenv("APP__GOOGLE__CLIENT_ID", "id.apps.googleusercontent.com")
	if _, err := Load(); err == nil {
		t.Fatal("un client ID sin secreto es una configuración incompleta")
	}
	t.Setenv("APP__GOOGLE__CLIENT_SECRET", "secreto")
	cfg, err := Load()
	if err != nil || !cfg.Google.Enabled() || cfg.Google.StateTTL != 10*time.Minute {
		t.Fatalf("Google configurado: %+v %v", cfg.Google, err)
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
