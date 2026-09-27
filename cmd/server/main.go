// Command server arranca la API HTTP de Qatu.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"

	apihttp "github.com/santiago-noe/qatu-api/internal/adapter/inbound/http"
	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/handler"
	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/middleware"
	"github.com/santiago-noe/qatu-api/internal/adapter/logging"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/argon2"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/breachedlist"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/postgres"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/redisclient"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/smtp"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/system"
	"github.com/santiago-noe/qatu-api/internal/config"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

const (
	healthTimeout = 2 * time.Second
	redisPrefix   = "qatu:"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(cfg.IsProduction())

	// Adaptadores de salida.
	db, err := postgres.New(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer db.Close()

	cache := redisclient.New(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	defer cache.Close()

	breached := breachedlist.New()
	if err := breached.Load(); err != nil {
		return err
	}
	accounts := postgres.NewAccountRepository(db)
	clock := system.Clock{}

	// Casos de uso.
	health := service.NewHealthService(healthTimeout, db, cache)
	sessions := service.NewSessionService(redisclient.NewSessionStore(cache.RDB, redisPrefix), clock,
		service.SessionConfig{TTL: cfg.Session.TTL, RenewAfter: cfg.Session.RenewAfter})
	codes := service.NewOneTimeCodes(redisclient.NewCodeStore(cache.RDB, redisPrefix), cfg.Security.CodeSecret,
		service.CodesConfig{TTL: cfg.Codes.TTL, MaxAttempts: cfg.Codes.MaxAttempts, ResendCooldown: cfg.Codes.ResendCooldown})
	mailer, err := smtp.New(smtp.Config{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password, From: cfg.SMTP.From})
	if err != nil {
		return err
	}
	verification := service.NewEmailVerificationService(accounts, codes, mailer, clock)
	auth, err := service.NewAuthService(service.AuthDeps{
		Accounts: accounts, Audit: accounts, Hasher: argon2.New(argon2.DefaultParams), Breached: breached,
		Sessions: sessions, Verification: verification, Clock: clock, IDs: system.UUIDGenerator{},
		Legal: service.LegalVersions{Terms: cfg.Legal.TermsVersion, Privacy: cfg.Legal.PrivacyVersion},
	})
	if err != nil {
		return err
	}

	// Adaptadores de entrada.
	app := apihttp.NewRouter(log,
		apihttp.Handlers{
			Health: handler.NewHealthHandler(health),
			Auth:   handler.NewAuthHandler(auth),
			Email:  handler.NewEmailVerificationHandler(verification),
		},
		apihttp.Middlewares{
			Session: middleware.SessionAuth(sessions, cfg.Session.CookieName),
		},
	)

	errCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%d", cfg.HTTP.Port)
		log.Info().Str("addr", addr).Str("env", cfg.Env).Msg("qatu-api escuchando")
		errCh <- app.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true})
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info().Msg("apagando qatu-api")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	return app.ShutdownWithContext(shutdownCtx)
}
