// Package logging configura zerolog y el middleware de registro de peticiones HTTP.
package logging

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

// New crea un logger JSON en producción y legible en desarrollo.
func New(production bool) zerolog.Logger {
	var out io.Writer = os.Stdout
	level := zerolog.InfoLevel
	if !production {
		out = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.TimeOnly}
		level = zerolog.DebugLevel
	}
	return zerolog.New(out).Level(level).With().Timestamp().Logger()
}

// Middleware registra método, ruta, estado y duración de cada petición.
// Los 5xx se registran como error; los 4xx como advertencia.
func Middleware(log zerolog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := StatusOf(c, err)

		event := log.Info()
		switch {
		case status >= fiber.StatusInternalServerError:
			event = log.Error().Err(err)
		case status >= fiber.StatusBadRequest:
			event = log.Warn()
		}
		event.
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", status).
			Dur("duration", time.Since(start)).
			Msg("http")
		return err
	}
}

// StatusOf devuelve el estado que recibirá el cliente: si el handler devolvió un error,
// Fiber lo aplica después del middleware, así que se toma del propio error.
func StatusOf(c fiber.Ctx, err error) int {
	if err == nil {
		return c.Response().StatusCode()
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return fiber.StatusInternalServerError
}
