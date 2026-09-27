package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// stubHuman acepta solo el token "humano" para la acción esperada.
type stubHuman struct{ action string }

func (s stubHuman) Verify(_ context.Context, token, action, _ string) error {
	if token != "humano" || action != s.action {
		return domain.ErrHumanCheckFailed
	}
	return nil
}

func TestRequireHuman(t *testing.T) {
	var got error
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		got = err
		return c.SendStatus(fiber.StatusForbidden)
	}})
	app.Post("/register", RequireHuman(stubHuman{action: "register"}, "register"), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusCreated)
	})

	for _, tt := range []struct {
		name, token string
		want        int
	}{
		{"sin token", "", fiber.StatusForbidden},
		{"token inválido", "bot", fiber.StatusForbidden},
		{"token válido", "humano", fiber.StatusCreated},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got = nil
			req := httptest.NewRequest(fiber.MethodPost, "/register", nil)
			if tt.token != "" {
				req.Header.Set(HeaderTurnstileToken, tt.token)
			}
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tt.want {
				t.Fatalf("status = %d, se esperaba %d", res.StatusCode, tt.want)
			}
			if tt.want == fiber.StatusForbidden && !errors.Is(got, domain.ErrHumanCheckFailed) {
				t.Fatalf("debe devolver ErrHumanCheckFailed, llegó %v", got)
			}
		})
	}
}
