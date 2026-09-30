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
	Catalog   *handler.CatalogHandler
	Location  *handler.LocationHandler
	// AdminCatalog: categorías, ciudades y ajustes de la plataforma.
	AdminCatalog *handler.AdminCatalogHandler
	// Listing: perfil de arrendador y sus publicaciones (feature 003).
	Listing *handler.ListingHandler
	Photo   *handler.PhotoHandler
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

	// Catálogo público (feature 002): sin sesión, con caché.
	v1.Get("/catalog/categories", h.Catalog.Categories)
	v1.Get("/cities", h.Catalog.Cities)
	v1.Get("/cities/:slug/zones", h.Catalog.Zones)
	v1.Get("/geo/zone", h.Catalog.ZoneAt)

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
	me.Get("/location", h.Location.Get)
	me.Put("/location", h.Location.Set)

	// Arrendador (feature 003): el perfil en la base decide, no el rol en la sesión (así activarlo
	// no obliga a volver a iniciar sesión).
	me.Get("/lender", h.Listing.GetLender)
	me.Put("/lender", h.Listing.SaveLender)
	me.Get("/lender/deposit-suggestion", h.Listing.DepositSuggestion)
	me.Get("/listings", h.Listing.List)
	me.Post("/listings", h.Listing.Create)
	me.Get("/listings/:id", h.Listing.Get)
	me.Put("/listings/:id", h.Listing.Update)
	me.Post("/listings/:id/submit", h.Listing.Submit)
	me.Post("/listings/:id/pause", h.Listing.Pause)
	me.Post("/listings/:id/resume", h.Listing.Resume)
	me.Post("/listings/:id/archive", h.Listing.Archive)
	me.Post("/listings/:id/duplicate", h.Listing.Duplicate)
	me.Get("/listings/:id/availability", h.Listing.Calendar)
	me.Post("/listings/:id/availability", h.Listing.BlockDates)
	me.Delete("/listings/:id/availability/:block", h.Listing.UnblockDates)
	me.Get("/listings/:id/photos", h.Photo.List)
	me.Post("/listings/:id/photos", h.Photo.RequestUpload)
	me.Put("/listings/:id/photos/order", h.Photo.Reorder)
	me.Post("/listings/:id/photos/:photo/complete", h.Photo.CompleteUpload)
	me.Delete("/listings/:id/photos/:photo", h.Photo.Delete)

	admin := v1.Group("/admin", staff(m, domain.RoleAdmin)...)
	admin.Get("/users", h.Admin.Find)
	admin.Get("/users/:id", h.Admin.Get)
	admin.Patch("/users/:id/roles", h.Admin.ChangeRoles)
	admin.Patch("/users/:id/status", h.Admin.ChangeStatus)
	admin.Get("/catalog/categories", h.AdminCatalog.Categories)
	admin.Post("/catalog/categories", h.AdminCatalog.CreateCategory)
	admin.Patch("/catalog/categories/:id", h.AdminCatalog.UpdateCategory)
	admin.Get("/catalog/categories/:id/cities", h.AdminCatalog.CategoryCities)
	admin.Put("/catalog/categories/:id/cities/:city", h.AdminCatalog.SetCategoryCityScope)
	admin.Get("/cities", h.AdminCatalog.Cities)
	admin.Post("/cities", h.AdminCatalog.CreateCity)
	admin.Patch("/cities/:slug", h.AdminCatalog.UpdateCity)
	admin.Get("/cities/:slug/zones", h.AdminCatalog.Zones)
	admin.Post("/cities/:slug/zones", h.AdminCatalog.CreateZone)
	admin.Patch("/cities/:slug/zones/:zone", h.AdminCatalog.UpdateZone)
	admin.Get("/settings", h.AdminCatalog.Settings)
	admin.Put("/settings", h.AdminCatalog.SetSetting)
	admin.Get("/settings/:key/history", h.AdminCatalog.SettingHistory)

	return app
}

// staff protege las rutas internas: sesión, alguno de los roles y segundo paso confirmado.
// Soporte y moderación usarán lo mismo con sus roles.
func staff(m Middlewares, roles ...domain.Role) []any {
	return []any{m.Session, middleware.RequireRole(roles...), middleware.RequireTwoFactor()}
}
