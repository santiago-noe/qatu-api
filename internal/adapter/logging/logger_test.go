package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

func TestMiddlewareLogsTheClientStatus(t *testing.T) {
	errBusiness := errors.New("correo registrado")
	statusOf := func(err error) int {
		if errors.Is(err, errBusiness) {
			return fiber.StatusConflict
		}
		return fiber.StatusInternalServerError
	}

	tests := []struct {
		name      string
		handler   fiber.Handler
		wantLevel string
		want      int
	}{
		{name: "éxito", handler: func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusCreated) }, wantLevel: "info", want: 201},
		{name: "error de negocio", handler: func(fiber.Ctx) error { return errBusiness }, wantLevel: "warn", want: 409},
		{name: "error inesperado", handler: func(fiber.Ctx) error { return errors.New("boom") }, wantLevel: "error", want: 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			app := fiber.New()
			app.Use(Middleware(zerolog.New(&buf), statusOf))
			app.Get("/", tt.handler)
			if _, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil)); err != nil {
				t.Fatal(err)
			}

			var line struct {
				Level  string `json:"level"`
				Status int    `json:"status"`
			}
			if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
				t.Fatalf("log inválido %q: %v", buf.String(), err)
			}
			if line.Level != tt.wantLevel || line.Status != tt.want {
				t.Fatalf("log = %+v, se esperaba %s %d", line, tt.wantLevel, tt.want)
			}
		})
	}
}
