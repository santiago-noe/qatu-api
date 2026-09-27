package middleware

import (
	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// HeaderTurnstileToken lleva el token del widget de Turnstile. Lo pone el BFF de qatu-app;
// ir en una cabecera deja el cuerpo de cada formulario sin cambios.
const HeaderTurnstileToken = "X-Turnstile-Token"

// RequireHuman exige un token de Turnstile válido para action (el mismo nombre que el
// formulario usa en el widget). Va después de RateLimitByIP: el límite es local y barato,
// la verificación es una llamada a Cloudflare.
func RequireHuman(verifier port.HumanVerifier, action string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := verifier.Verify(c.Context(), c.Get(HeaderTurnstileToken), action, c.IP()); err != nil {
			return err
		}
		return c.Next()
	}
}
