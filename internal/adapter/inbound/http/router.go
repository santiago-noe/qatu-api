// Package http arma la aplicación Fiber: middlewares y rutas versionadas (/api/v1).
package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/rs/zerolog"

	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/handler"
	"github.com/santiago-noe/qatu-api/internal/adapter/logging"
)

// Handlers agrupa los handlers de cada feature; se amplía al agregar features.
type Handlers struct {
	Health   *handler.HealthHandler
	Auth     *handler.AuthHandler
	Email    *handler.EmailVerificationHandler
	Password *handler.PasswordResetHandler
}

// Middlewares compartidos que dependen de servicios (se construyen en cmd/server).
type Middlewares struct {
	// Session exige una sesión válida (middleware.SessionAuth).
	Session fiber.Handler
}

func NewRouter(log zerolog.Logger, h Handlers, m Middlewares) *fiber.App {
	app := fiber.New(fiber.Config{AppName: "qatu-api", ErrorHandler: handler.ErrorHandler})
	app.Use(recover.New())
	app.Use(logging.Middleware(log, handler.StatusOf))

	v1 := app.Group("/api/v1")
	v1.Get("/health", h.Health.Get)

	auth := v1.Group("/auth")
	auth.Post("/register", h.Auth.Register)
	auth.Post("/login", h.Auth.Login)
	auth.Post("/logout", m.Session, h.Auth.Logout)
	auth.Post("/logout-all", m.Session, h.Auth.LogoutAll)
	auth.Post("/email/verify", m.Session, h.Email.Verify)
	auth.Post("/email/resend", m.Session, h.Email.Resend)
	auth.Post("/password/forgot", h.Password.Forgot)
	auth.Post("/password/reset", h.Password.Reset)

	v1.Get("/me", m.Session, h.Auth.Me)

	return app
}
