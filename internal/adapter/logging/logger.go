// Package logging configura zerolog y el middleware de registro de peticiones HTTP.
package logging

import (
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
// errorStatus traduce el error devuelto por el handler al estado HTTP que recibirá el cliente
// (es la misma traducción que usa el ErrorHandler, así el log y la respuesta coinciden).
// Los 5xx se registran como error; los 4xx como advertencia.
func Middleware(log zerolog.Logger, errorStatus func(error) int) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		status := c.Response().StatusCode()
		if err != nil {
			status = errorStatus(err)
		}

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
