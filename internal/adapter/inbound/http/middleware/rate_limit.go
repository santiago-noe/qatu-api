package middleware

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Limiter es lo que el middleware necesita de port.RateLimiter.
type Limiter interface {
	Allow(ctx context.Context, key string, limit domain.Limit) (bool, time.Duration, error)
}

// RateLimitByIP limita las peticiones de una IP a una ruta. name separa los contadores por ruta.
// c.IP() es la IP real del usuario cuando la petición llega por el BFF (proxy de confianza).
func RateLimitByIP(limiter Limiter, name string, limit domain.Limit) fiber.Handler {
	return func(c fiber.Ctx) error {
		allowed, retryAfter, err := limiter.Allow(c.Context(), name+":ip:"+c.IP(), limit)
		if err != nil {
			return err
		}
		if !allowed {
			return &domain.RateLimitError{RetryAfter: retryAfter}
		}
		return c.Next()
	}
}
