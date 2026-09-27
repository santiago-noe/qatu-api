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

// PasswordUpdate crea o reemplaza la contraseña de una cuenta.
type PasswordUpdate struct {
	UserID     string
	IdentityID string // se usa si la cuenta aún no tenía contraseña (por ejemplo, entró con Google)
	Email      string
	SecretHash string
	At         time.Time
	// VerifyEmail: recibir el código en el correo demuestra que es suyo.
	VerifyEmail bool
	Audit       domain.AuditEntry
}

// AccountRepository persiste cuentas e identidades de acceso. Implementación: Postgres.
type AccountRepository interface {
	// CreateAccount devuelve domain.ErrEmailTaken si el correo o la identidad ya existen.
	CreateAccount(ctx context.Context, account NewAccount) error
	// FindIdentity devuelve domain.ErrNotFound si no existe.
	FindIdentity(ctx context.Context, provider domain.AuthProvider, subject string) (domain.AuthIdentity, error)
	// FindUser devuelve el usuario con sus roles, o domain.ErrNotFound.
	FindUser(ctx context.Context, id string) (domain.User, error)
	// FindUserByEmail devuelve el usuario con sus roles, o domain.ErrNotFound.
	FindUserByEmail(ctx context.Context, email string) (domain.User, error)
	// SetPassword crea o reemplaza la identidad "password" y audita, en una transacción.
	SetPassword(ctx context.Context, update PasswordUpdate) error
	// FindUserIdentity devuelve la identidad del usuario para un proveedor, o domain.ErrNotFound.
	FindUserIdentity(ctx context.Context, userID string, provider domain.AuthProvider) (domain.AuthIdentity, error)
	// UpdateName cambia el nombre y audita en una transacción.
	UpdateName(ctx context.Context, userID, name string, audit domain.AuditEntry) error
	MarkIdentityUsed(ctx context.Context, identityID string, at time.Time) error
	UpdateIdentitySecret(ctx context.Context, identityID, secretHash string) error
	// MarkEmailVerified confirma el correo y registra la auditoría en la misma transacción.
	MarkEmailVerified(ctx context.Context, userID string, at time.Time, audit domain.AuditEntry) error
}

// AuditLog registra acciones fuera de una transacción de negocio (por ejemplo, inicios de sesión).
type AuditLog interface {
	Record(ctx context.Context, entry domain.AuditEntry) error
}
