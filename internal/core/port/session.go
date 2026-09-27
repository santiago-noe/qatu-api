package port

import (
	"context"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// SessionStore guarda sesiones por su ID (hash del token). Implementación: Redis.
type SessionStore interface {
	// Save crea o reemplaza la sesión con vencimiento en s.ExpiresAt.
	Save(ctx context.Context, s domain.Session) error
	// Get devuelve domain.ErrSessionInvalid si no existe o venció.
	Get(ctx context.Context, id string) (domain.Session, error)
	// Extend actualiza el vencimiento de una sesión existente (renovación con el uso).
	Extend(ctx context.Context, id string, renewedAt, expiresAt time.Time) error
	Delete(ctx context.Context, userID, id string) error
	// DeleteAllForUser cierra todas las sesiones del usuario salvo exceptID (vacío = todas).
	DeleteAllForUser(ctx context.Context, userID, exceptID string) error
	ListForUser(ctx context.Context, userID string) ([]domain.Session, error)
}
