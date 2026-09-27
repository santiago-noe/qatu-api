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
}

type HTTPConfig struct {
	Port            int           `mapstructure:"port"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
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
	"env":                   "development",
	"http.port":             8080,
	"http.shutdown_timeout": "10s",
	"database.url":          "postgres://qatu:qatu@localhost:5433/qatu?sslmode=disable",
	"redis.addr":            "localhost:6379",
	"redis.password":        "",
	"redis.db":              0,
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
	if cfg.HTTP.Port <= 0 {
		return Config{}, fmt.Errorf("config: APP__HTTP__PORT inválido: %d", cfg.HTTP.Port)
	}
	return cfg, nil
}

func (c Config) IsProduction() bool { return c.Env == "production" }
