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
	Env       string          `mapstructure:"env"`
	HTTP      HTTPConfig      `mapstructure:"http"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Session   SessionConfig   `mapstructure:"session"`
	Legal     LegalConfig     `mapstructure:"legal"`
	SMTP      SMTPConfig      `mapstructure:"smtp"`
	Security  SecurityConfig  `mapstructure:"security"`
	Codes     CodesConfig     `mapstructure:"codes"`
	Limits    LimitsConfig    `mapstructure:"ratelimit"`
	Turnstile TurnstileConfig `mapstructure:"turnstile"`
}

// TurnstileConfig: captcha de Cloudflare en formularios públicos. Sin secreto (solo en
// desarrollo) la verificación queda desactivada; las claves de prueba de Cloudflare sirven
// para probarla de verdad en local.
type TurnstileConfig struct {
	Secret string `mapstructure:"secret"`
}

type HTTPConfig struct {
	Port            int           `mapstructure:"port"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// TrustPrivateProxies: confiar en X-Forwarded-For solo si la petición viene de una red
	// privada o local (el BFF de qatu-app). Sin esto, todos los usuarios compartirían la IP del BFF.
	TrustPrivateProxies bool `mapstructure:"trust_private_proxies"`
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

// LimitConfig es un máximo de acciones por ventana de tiempo.
type LimitConfig struct {
	Max    int           `mapstructure:"max"`
	Window time.Duration `mapstructure:"window"`
}

// LimitsConfig: valores de partida a validar en el piloto (spec 001).
type LimitsConfig struct {
	// AuthPerIP limita cada ruta pública de acceso (registro, login, recuperación) por IP.
	AuthPerIP LimitConfig `mapstructure:"auth_per_ip"`
	// LoginPerAccount: 5 intentos cada 15 minutos por cuenta (decisión de clarify).
	LoginPerAccount LimitConfig `mapstructure:"login_per_account"`
}

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
	"env":                                "development",
	"http.port":                          8080,
	"http.shutdown_timeout":              "10s",
	"http.trust_private_proxies":         true,
	"ratelimit.auth_per_ip.max":          30,
	"ratelimit.auth_per_ip.window":       "15m",
	"ratelimit.login_per_account.max":    5,
	"ratelimit.login_per_account.window": "15m",
	"database.url":                       "postgres://qatu:qatu@localhost:5433/qatu?sslmode=disable",
	"redis.addr":                         "localhost:6379",
	"redis.password":                     "",
	"redis.db":                           0,
	"session.cookie_name":                "qatu_session",
	"session.ttl":                        "720h",
	"session.renew_after":                "24h",
	"legal.terms_version":                "borrador-2026-09",
	"legal.privacy_version":              "borrador-2026-09",
	"smtp.host":                          "localhost",
	"smtp.port":                          1025,
	"smtp.username":                      "",
	"smtp.password":                      "",
	"smtp.from":                          "Qatu <no-responder@qatu.local>",
	"security.code_secret":               "",
	"codes.ttl":                          "15m",
	"codes.max_attempts":                 5,
	"codes.resend_cooldown":              "1m",
	"turnstile.secret":                   "",
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
	if cfg.IsProduction() {
		// Secretos sin valor por defecto: en producción su ausencia es un error de despliegue.
		for _, s := range []struct{ env, value string }{
			{"APP__SECURITY__CODE_SECRET", cfg.Security.CodeSecret},
			{"APP__TURNSTILE__SECRET", cfg.Turnstile.Secret},
		} {
			if s.value == "" {
				return Config{}, fmt.Errorf("config: %s es obligatorio en producción", s.env)
			}
		}
	}
	if cfg.Security.CodeSecret == "" {
		cfg.Security.CodeSecret = devCodeSecret
	}
	if cfg.HTTP.Port <= 0 {
		return Config{}, fmt.Errorf("config: APP__HTTP__PORT inválido: %d", cfg.HTTP.Port)
	}
	return cfg, nil
}

func (c Config) IsProduction() bool { return c.Env == "production" }
