// Package http arma la aplicación Fiber: middlewares y rutas versionadas (/api/v1).
package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/rs/zerolog"

	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/handler"
	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/middleware"
	"github.com/santiago-noe/qatu-api/internal/adapter/logging"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Handlers agrupa los handlers de cada feature; se amplía al agregar features.
type Handlers struct {
	Health    *handler.HealthHandler
	Auth      *handler.AuthHandler
	Email     *handler.EmailVerificationHandler
	Password  *handler.PasswordResetHandler
	Me        *handler.MeHandler
	Admin     *handler.AdminUserHandler
	TwoFactor *handler.TwoFactorHandler
	OAuth     *handler.OAuthHandler
}

// Middlewares compartidos que dependen de servicios (se construyen en cmd/server).
type Middlewares struct {
	// Session exige una sesión válida (middleware.SessionAuth).
	Session fiber.Handler
	// RateLimit limita por IP una ruta pública; name separa los contadores.
	RateLimit func(name string) fiber.Handler
	// Human exige el captcha de Turnstile; action es el nombre del formulario en el widget.
	Human func(action string) fiber.Handler
}

type Options struct {
	// TrustPrivateProxies: c.IP() toma X-Forwarded-For solo si la petición viene de una red
	// privada o local (el BFF). Desde cualquier otra IP la cabecera se ignora.
	TrustPrivateProxies bool
}

func NewRouter(log zerolog.Logger, h Handlers, m Middlewares, opts Options) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:            "qatu-api",
		ErrorHandler:       handler.ErrorHandler,
		TrustProxy:         opts.TrustPrivateProxies,
		TrustProxyConfig:   fiber.TrustProxyConfig{Loopback: true, Private: true},
		ProxyHeader:        fiber.HeaderXForwardedFor,
		EnableIPValidation: true,
	})
	app.Use(recover.New())
	app.Use(logging.Middleware(log, handler.StatusOf))

	v1 := app.Group("/api/v1")
	v1.Get("/health", h.Health.Get)

	auth := v1.Group("/auth")
	auth.Post("/register", m.RateLimit("register"), m.Human("register"), h.Auth.Register)
	auth.Post("/login", m.RateLimit("login"), h.Auth.Login)
	auth.Post("/logout", m.Session, h.Auth.Logout)
	auth.Post("/logout-all", m.Session, h.Auth.LogoutAll)
	auth.Post("/email/verify", m.Session, h.Email.Verify)
	auth.Post("/email/resend", m.Session, h.Email.Resend)
	auth.Post("/password/forgot", m.RateLimit("password_forgot"), m.Human("password_forgot"), h.Password.Forgot)
	auth.Post("/password/reset", m.RateLimit("password_reset"), h.Password.Reset)
	auth.Post("/google/start", m.RateLimit("google"), h.OAuth.Start)
	auth.Post("/google/callback", m.RateLimit("google"), h.OAuth.Callback)
	auth.Post("/two-factor/send", m.Session, h.TwoFactor.Send)
	auth.Post("/two-factor/verify", m.Session, h.TwoFactor.Verify)

	me := v1.Group("/me", m.Session)
	me.Get("/", h.Me.Get)
	me.Patch("/", h.Me.Update)
	me.Post("/password", h.Me.ChangePassword)
	me.Get("/sessions", h.Me.Sessions)
	me.Delete("/sessions/:id", h.Me.RevokeSession)

	admin := v1.Group("/admin", staff(m, domain.RoleAdmin)...)
	admin.Get("/users/:id", h.Admin.Get)
	admin.Patch("/users/:id/roles", h.Admin.ChangeRoles)
	admin.Patch("/users/:id/status", h.Admin.ChangeStatus)

	return app
}

// staff protege las rutas internas: sesión, alguno de los roles y segundo paso confirmado.
// Soporte y moderación usarán lo mismo con sus roles.
func staff(m Middlewares, roles ...domain.Role) []any {
	return []any{m.Session, middleware.RequireRole(roles...), middleware.RequireTwoFactor()}
}
