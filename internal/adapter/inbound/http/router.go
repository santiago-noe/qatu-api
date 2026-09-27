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
	Health *handler.HealthHandler
}

func NewRouter(log zerolog.Logger, h Handlers) *fiber.App {
	app := fiber.New(fiber.Config{AppName: "qatu-api"})
	app.Use(recover.New())
	app.Use(logging.Middleware(log))

	v1 := app.Group("/api/v1")
	v1.Get("/health", h.Health.Get)

	return app
}
