package port

import (
	"context"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// RateLimiter cuenta acciones por clave en una ventana deslizante. Implementación: Redis.
type RateLimiter interface {
	// Allow registra un intento; si se superó el límite devuelve false y cuánto esperar.
	Allow(ctx context.Context, key string, limit domain.Limit) (allowed bool, retryAfter time.Duration, err error)
	// Reset borra los intentos de la clave (por ejemplo, tras un inicio de sesión correcto).
	Reset(ctx context.Context, key string) error
}
