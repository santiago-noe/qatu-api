package handler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestToAPIError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"error de negocio", domain.ErrEmailTaken, fiber.StatusConflict, "correo_registrado"},
		{"error envuelto", fmt.Errorf("registro: %w", domain.ErrPasswordBreached), fiber.StatusUnprocessableEntity, "contrasena_filtrada"},
		{"credenciales", domain.ErrInvalidCredentials, fiber.StatusUnauthorized, "credenciales_invalidas"},
		{"fallo de Google sin detalles técnicos", fmt.Errorf("%w: invalid_grant", domain.ErrOAuthFailed), fiber.StatusUnauthorized, "google_fallido"},
		{"error de Fiber", fiber.ErrNotFound, fiber.StatusNotFound, "Not Found"},
		{"código de middleware", fiber.NewError(fiber.StatusUnauthorized, "no_autenticado"), fiber.StatusUnauthorized, "no_autenticado"},
		{"APIError propio", badRequest("x"), fiber.StatusBadRequest, "solicitud_invalida"},
		{"error desconocido no filtra detalles", errors.New("pq: connection refused at 10.0.0.3"), fiber.StatusInternalServerError, "error_interno"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toAPIError(tt.err)
			if got.Status != tt.wantStatus || got.Code != tt.wantCode {
				t.Fatalf("toAPIError = %+v, se esperaba %d %s", got, tt.wantStatus, tt.wantCode)
			}
			if got.Code == "no_autenticado" && got.Message != domain.ErrSessionInvalid.Error() {
				t.Fatalf("el mensaje debe ser legible, llegó %q", got.Message)
			}
			if got.Code == "google_fallido" && got.Message != domain.ErrOAuthFailed.Error() {
				t.Fatalf("el mensaje no incluye el detalle técnico, llegó %q", got.Message)
			}
			if StatusOf(tt.err) != tt.wantStatus {
				t.Fatal("StatusOf debe coincidir con la respuesta")
			}
		})
	}
}
