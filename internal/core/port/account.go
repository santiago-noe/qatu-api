package port

import (
	"context"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// NewAccount agrupa todo lo que se crea al registrarse, en una sola transacción.
type NewAccount struct {
	User     domain.User
	Identity domain.AuthIdentity
	Consents []domain.Consent
	Audit    domain.AuditEntry
}

// AccountRepository persiste cuentas e identidades de acceso. Implementación: Postgres.
type AccountRepository interface {
	// CreateAccount devuelve domain.ErrEmailTaken si el correo o la identidad ya existen.
	CreateAccount(ctx context.Context, account NewAccount) error
	// FindIdentity devuelve domain.ErrNotFound si no existe.
	FindIdentity(ctx context.Context, provider domain.AuthProvider, subject string) (domain.AuthIdentity, error)
	// FindUser devuelve el usuario con sus roles, o domain.ErrNotFound.
	FindUser(ctx context.Context, id string) (domain.User, error)
	MarkIdentityUsed(ctx context.Context, identityID string, at time.Time) error
	UpdateIdentitySecret(ctx context.Context, identityID, secretHash string) error
}

// AuditLog registra acciones fuera de una transacción de negocio (por ejemplo, inicios de sesión).
type AuditLog interface {
	Record(ctx context.Context, entry domain.AuditEntry) error
}
