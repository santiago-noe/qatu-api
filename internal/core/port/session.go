package port

import (
	"context"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// SessionStore guarda sesiones por su ID (hash del token). Implementación: Redis.
type SessionStore interface {
	// Save crea o reemplaza la sesión con vencimiento en s.ExpiresAt.
	Save(ctx context.Context, s domain.Session) error
	// Get devuelve domain.ErrSessionInvalid si no existe o venció.
	Get(ctx context.Context, id string) (domain.Session, error)
	// Update aplica change a la sesión vigente de forma atómica (renovación, segundo paso) y
	// devuelve cómo quedó. Si ya no existe, domain.ErrSessionInvalid: nunca revive una sesión cerrada.
	Update(ctx context.Context, id string, change func(*domain.Session)) (domain.Session, error)
	Delete(ctx context.Context, userID, id string) error
	// DeleteAllForUser cierra todas las sesiones del usuario salvo exceptID (vacío = todas).
	DeleteAllForUser(ctx context.Context, userID, exceptID string) error
	ListForUser(ctx context.Context, userID string) ([]domain.Session, error)
}
