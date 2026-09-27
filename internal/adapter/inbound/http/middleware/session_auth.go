// Package middleware contiene la autenticación por sesión y el control de roles.
package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

const sessionLocal = "qatu.session"

// Authenticator es lo que el middleware necesita del SessionService.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (domain.Session, error)
}

// SessionAuth exige una sesión válida. Acepta la cookie de sesión (la reenvía el BFF de
// qatu-app) o "Authorization: Bearer <token>" (herramientas como Bruno).
func SessionAuth(auth Authenticator, cookieName string) fiber.Handler {
	return func(c fiber.Ctx) error {
		session, err := auth.Authenticate(c.Context(), tokenFrom(c, cookieName))
		if errors.Is(err, domain.ErrSessionInvalid) {
			return fiber.NewError(fiber.StatusUnauthorized, "no_autenticado")
		}
		if err != nil {
			return err
		}
		c.Locals(sessionLocal, session)
		return c.Next()
	}
}

// RequireRole exige al menos uno de los roles. Va después de SessionAuth.
func RequireRole(roles ...domain.Role) fiber.Handler {
	return func(c fiber.Ctx) error {
		session, ok := SessionFrom(c)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "no_autenticado")
		}
		if !session.HasAnyRole(roles...) {
			return fiber.NewError(fiber.StatusForbidden, "sin_permiso")
		}
		return c.Next()
	}
}

// RequireTwoFactor exige que una sesión con roles internos haya confirmado el segundo paso.
// Va después de RequireRole en toda ruta interna (soporte, moderación, administración).
func RequireTwoFactor() fiber.Handler {
	return func(c fiber.Ctx) error {
		session, ok := SessionFrom(c)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "no_autenticado")
		}
		if session.NeedsTwoFactor() {
			return fiber.NewError(fiber.StatusForbidden, "dos_pasos_requerido")
		}
		return c.Next()
	}
}

// SessionFrom devuelve la sesión que dejó SessionAuth.
func SessionFrom(c fiber.Ctx) (domain.Session, bool) {
	session, ok := c.Locals(sessionLocal).(domain.Session)
	return session, ok
}

func tokenFrom(c fiber.Ctx, cookieName string) string {
	if token := c.Cookies(cookieName); token != "" {
		return token
	}
	if header := c.Get(fiber.HeaderAuthorization); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	return ""
}
