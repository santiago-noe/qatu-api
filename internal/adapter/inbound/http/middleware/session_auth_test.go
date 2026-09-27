package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type stubAuth map[string]domain.Session

func (s stubAuth) Authenticate(_ context.Context, token string) (domain.Session, error) {
	if session, ok := s[token]; ok {
		return session, nil
	}
	return domain.Session{}, domain.ErrSessionInvalid
}

const cookie = "qatu_session"

func newApp() *fiber.App {
	auth := stubAuth{
		"tok-cliente": {UserID: "u1", Roles: []domain.Role{domain.RoleClient}},
		"tok-admin":   {UserID: "u2", Roles: []domain.Role{domain.RoleClient, domain.RoleAdmin}},
	}
	app := fiber.New()
	app.Get("/me", SessionAuth(auth, cookie), func(c fiber.Ctx) error {
		session, _ := SessionFrom(c)
		return c.SendString(session.UserID)
	})
	app.Get("/admin", SessionAuth(auth, cookie), RequireRole(domain.RoleAdmin), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	return app
}

func TestSessionAuthAndRequireRole(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		cookie string
		bearer string
		want   int
	}{
		{name: "sin sesión", path: "/me", want: fiber.StatusUnauthorized},
		{name: "token inválido", path: "/me", cookie: "falso", want: fiber.StatusUnauthorized},
		{name: "cookie válida", path: "/me", cookie: "tok-cliente", want: fiber.StatusOK},
		{name: "bearer válido", path: "/me", bearer: "tok-cliente", want: fiber.StatusOK},
		{name: "rol insuficiente", path: "/admin", cookie: "tok-cliente", want: fiber.StatusForbidden},
		{name: "rol suficiente", path: "/admin", cookie: "tok-admin", want: fiber.StatusOK},
	}

	app := newApp()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(fiber.MethodGet, tt.path, nil)
			if tt.cookie != "" {
				req.Header.Set("Cookie", cookie+"="+tt.cookie)
			}
			if tt.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tt.bearer)
			}
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tt.want {
				t.Fatalf("status = %d, se esperaba %d", res.StatusCode, tt.want)
			}
		})
	}
}
