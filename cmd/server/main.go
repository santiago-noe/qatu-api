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
	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/worker"
	"github.com/santiago-noe/qatu-api/internal/adapter/logging"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/argon2"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/breachedlist"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/googleoauth"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/imaging"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/jsonschema"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/postgres"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/queue"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/redisclient"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/s3storage"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/smtp"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/system"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/turnstile"
	"github.com/santiago-noe/qatu-api/internal/config"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
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

	storage, err := s3storage.New(s3storage.Config{Endpoint: cfg.Storage.Endpoint, AccessKey: cfg.Storage.AccessKey,
		SecretKey: cfg.Storage.SecretKey, UseSSL: cfg.Storage.UseSSL, Region: cfg.Storage.Region,
		PublicBucket: cfg.Storage.PublicBucket, PrivateBucket: cfg.Storage.PrivateBucket, PublicBaseURL: cfg.Storage.PublicBaseURL})
	if err != nil {
		return err
	}
	if cfg.Storage.CreateBuckets {
		if err := storage.EnsureBuckets(ctx); err != nil {
			// Sin almacenamiento la API sigue: solo fallan las fotos (y /health lo muestra).
			log.Warn().Err(err).Msg("no se pudieron crear los buckets de fotos")
		}
	}

	var human port.HumanVerifier = turnstile.New(cfg.Turnstile.Secret)
	if cfg.Turnstile.Secret == "" { // solo en desarrollo: config.Load lo exige en producción
		log.Warn().Msg("APP__TURNSTILE__SECRET vacío: captcha desactivado")
		human = turnstile.Disabled{}
	}

	// Casos de uso.
	health := service.NewHealthService(healthTimeout, db, cache, storage)
	sessions := service.NewSessionService(redisclient.NewSessionStore(cache.RDB, redisPrefix), clock,
		service.SessionConfig{TTL: cfg.Session.TTL, RenewAfter: cfg.Session.RenewAfter})
	mailer, err := smtp.New(smtp.Config{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password, From: cfg.SMTP.From})
	if err != nil {
		return err
	}
	passwords, err := service.NewPasswordPolicy(argon2.New(argon2.DefaultParams), breached)
	if err != nil {
		return err
	}
	ids := system.UUIDGenerator{}
	codes := service.NewOneTimeCodes(redisclient.NewCodeStore(cache.RDB, redisPrefix), mailer, cfg.Security.CodeSecret,
		service.CodesConfig{TTL: cfg.Codes.TTL, MaxAttempts: cfg.Codes.MaxAttempts, ResendCooldown: cfg.Codes.ResendCooldown})
	verification := service.NewEmailVerificationService(accounts, codes, clock)
	passwordReset := service.NewPasswordResetService(accounts, codes, passwords, sessions, clock, ids)
	limiter := redisclient.NewRateLimiter(cache.RDB, redisPrefix)
	legal := service.LegalVersions{Terms: cfg.Legal.TermsVersion, Privacy: cfg.Legal.PrivacyVersion}
	auth := service.NewAuthService(service.AuthDeps{
		Accounts: accounts, Audit: accounts, Passwords: passwords, Sessions: sessions,
		Limiter: limiter, LoginLimit: toLimit(cfg.Limits.LoginPerAccount),
		Verification: verification, Clock: clock, IDs: ids, Legal: legal,
	})

	account := service.NewAccountService(service.AccountDeps{
		Accounts: accounts, Audit: accounts, Passwords: passwords, Sessions: sessions,
		Limiter: limiter, PasswordLimit: toLimit(cfg.Limits.LoginPerAccount), Clock: clock, IDs: ids,
	})

	adminUsers := service.NewAdminUserService(accounts, sessions, clock)
	twoFactor := service.NewTwoFactorService(accounts, accounts, codes, sessions)

	var google port.OAuthProvider = googleoauth.Disabled{}
	if cfg.Google.Enabled() {
		google = googleoauth.New(ctx, googleoauth.Config{ClientID: cfg.Google.ClientID,
			ClientSecret: cfg.Google.ClientSecret, RedirectURL: cfg.Google.RedirectURL})
	} else {
		log.Warn().Msg("APP__GOOGLE__CLIENT_ID vacío: acceso con Google desactivado")
	}
	oauth := service.NewOAuthService(service.OAuthDeps{
		Accounts: accounts, Audit: accounts, Sessions: sessions, Provider: google,
		States: redisclient.NewOAuthStateStore(cache.RDB, redisPrefix), Clock: clock, IDs: ids,
		Legal: legal, StateTTL: cfg.Google.StateTTL,
	})

	catalogRepo := postgres.NewCatalogRepository(db)
	catalogCache := redisclient.NewCache(cache.RDB, redisPrefix)
	catalog := service.NewCatalogService(catalogRepo, catalogCache)
	schemas := jsonschema.New()
	catalogAdmin := service.NewCatalogAdminService(catalogRepo, schemas, catalogCache)
	userLocation := service.NewUserLocationService(accounts, catalog)

	listingRepo := postgres.NewListingRepository(db)
	lenderRepo := postgres.NewLenderRepository(db)
	lenders := service.NewLenderService(accounts, lenderRepo, catalog, clock, cfg.Legal.LenderTermsVersion)
	listings := service.NewListingService(service.ListingDeps{
		Lenders: lenderRepo, Listings: listingRepo, Categories: catalogRepo, Catalog: catalog,
		Attributes: schemas, Settings: service.NewSettingsResolver(catalogRepo, clock), Clock: clock, IDs: ids,
		LocationSecret: []byte(cfg.Security.LocationSecret),
	})
	photos := service.NewPhotoService(service.PhotoDeps{
		Listings: listingRepo, Photos: postgres.NewPhotoRepository(db), Storage: storage,
		Jobs: queue.NewPhotoJobs(cache.RDB), Images: imaging.New(), IDs: ids, Clock: clock,
	})

	moderation := service.NewModerationService(service.ModerationDeps{
		Listings: listingRepo, Categories: catalogRepo, Accounts: accounts, Photos: photos, Mailer: mailer, Clock: clock,
	})
	providers := service.NewProviderService(service.ProviderDeps{
		Accounts: accounts, Providers: postgres.NewProviderRepository(db), Catalog: catalog, Clock: clock, IDs: ids,
		TermsVersion: cfg.Legal.ProviderTermsVersion,
	})

	// Adaptadores de entrada.
	app := apihttp.NewRouter(log,
		apihttp.Handlers{
			Health:       handler.NewHealthHandler(health),
			Auth:         handler.NewAuthHandler(auth),
			Email:        handler.NewEmailVerificationHandler(verification),
			Password:     handler.NewPasswordResetHandler(passwordReset),
			Me:           handler.NewMeHandler(account),
			Admin:        handler.NewAdminUserHandler(adminUsers),
			TwoFactor:    handler.NewTwoFactorHandler(twoFactor),
			OAuth:        handler.NewOAuthHandler(oauth),
			Catalog:      handler.NewCatalogHandler(catalog),
			Location:     handler.NewLocationHandler(userLocation),
			AdminCatalog: handler.NewAdminCatalogHandler(catalogAdmin),
			Listing:      handler.NewListingHandler(lenders, listings),
			Photo:        handler.NewPhotoHandler(photos),
			Moderation:   handler.NewModerationHandler(moderation),
			Provider:     handler.NewProviderHandler(providers),
		},
		apihttp.Middlewares{
			Session: middleware.SessionAuth(sessions, cfg.Session.CookieName),
			RateLimit: func(name string) fiber.Handler {
				return middleware.RateLimitByIP(limiter, name, toLimit(cfg.Limits.AuthPerIP))
			},
			Human: func(action string) fiber.Handler { return middleware.RequireHuman(human, action) },
		},
		apihttp.Options{TrustPrivateProxies: cfg.HTTP.TrustPrivateProxies},
	)

	// Trabajos en segundo plano (procesar fotos) en el mismo proceso. Para escalar, se apaga aquí
	// (APP__WORKER__ENABLED=false) y se corre el mismo binario solo como worker.
	if cfg.Worker.Enabled {
		jobs := worker.New(cache.RDB, log, cfg.Worker.Concurrency, photos)
		if err := jobs.Start(); err != nil {
			return err
		}
		defer jobs.Shutdown()
	}

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

func toLimit(l config.LimitConfig) domain.Limit { return domain.Limit{Max: l.Max, Window: l.Window} }
