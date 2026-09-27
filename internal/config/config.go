// Package config carga la configuración desde variables de entorno con prefijo APP__.
// Los niveles se separan con doble guion bajo: APP__HTTP__PORT -> http.port.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Env      string         `mapstructure:"env"`
	HTTP     HTTPConfig     `mapstructure:"http"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Session  SessionConfig  `mapstructure:"session"`
	Legal    LegalConfig    `mapstructure:"legal"`
	SMTP     SMTPConfig     `mapstructure:"smtp"`
	Security SecurityConfig `mapstructure:"security"`
	Codes    CodesConfig    `mapstructure:"codes"`
}

type HTTPConfig struct {
	Port            int           `mapstructure:"port"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

// SessionConfig: 30 días renovables (decisión de clarify de la feature 001).
type SessionConfig struct {
	CookieName string        `mapstructure:"cookie_name"`
	TTL        time.Duration `mapstructure:"ttl"`
	RenewAfter time.Duration `mapstructure:"renew_after"`
}

// LegalConfig: versión de los términos y la política de privacidad que se aceptan al registrarse.
type LegalConfig struct {
	TermsVersion   string `mapstructure:"terms_version"`
	PrivacyVersion string `mapstructure:"privacy_version"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

type SecurityConfig struct {
	// CodeSecret firma los códigos de un solo uso guardados en Redis.
	CodeSecret string `mapstructure:"code_secret"`
}

// CodesConfig: códigos de 6 dígitos (verificación de correo, recuperación, dos pasos).
type CodesConfig struct {
	TTL            time.Duration `mapstructure:"ttl"`
	MaxAttempts    int           `mapstructure:"max_attempts"`
	ResendCooldown time.Duration `mapstructure:"resend_cooldown"`
}

// Solo para desarrollo: en producción Load exige APP__SECURITY__CODE_SECRET.
const devCodeSecret = "dev-only-code-secret-change-me"

type DatabaseConfig struct {
	URL string `mapstructure:"url"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// Valores por defecto para desarrollo local (docker compose).
var defaults = map[string]any{
	"env":                   "development",
	"http.port":             8080,
	"http.shutdown_timeout": "10s",
	"database.url":          "postgres://qatu:qatu@localhost:5433/qatu?sslmode=disable",
	"redis.addr":            "localhost:6379",
	"redis.password":        "",
	"redis.db":              0,
	"session.cookie_name":   "qatu_session",
	"session.ttl":           "720h",
	"session.renew_after":   "24h",
	"legal.terms_version":   "borrador-2026-09",
	"legal.privacy_version": "borrador-2026-09",
	"smtp.host":             "localhost",
	"smtp.port":             1025,
	"smtp.username":         "",
	"smtp.password":         "",
	"smtp.from":             "Qatu <no-responder@qatu.local>",
	"security.code_secret":  "",
	"codes.ttl":             "15m",
	"codes.max_attempts":    5,
	"codes.resend_cooldown": "1m",
}

func Load() (Config, error) {
	v := viper.New()
	// Prefijo "APP_" más el separador "_" de viper da "APP__".
	v.SetEnvPrefix("APP_")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()
	for key, value := range defaults {
		v.SetDefault(key, value)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if cfg.Security.CodeSecret == "" {
		if cfg.IsProduction() {
			return Config{}, fmt.Errorf("config: APP__SECURITY__CODE_SECRET es obligatorio en producción")
		}
		cfg.Security.CodeSecret = devCodeSecret
	}
	if cfg.HTTP.Port <= 0 {
		return Config{}, fmt.Errorf("config: APP__HTTP__PORT inválido: %d", cfg.HTTP.Port)
	}
	return cfg, nil
}

func (c Config) IsProduction() bool { return c.Env == "production" }
