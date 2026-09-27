package port

import (
	"context"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// CodeStore guarda códigos de un solo uso (ya firmados) con vencimiento e intentos. Implementación: Redis.
type CodeStore interface {
	// Save reemplaza cualquier código anterior de la misma finalidad y sujeto.
	Save(ctx context.Context, purpose domain.CodePurpose, subject, codeHash string, ttl time.Duration, attempts int) error
	// Consume valida y borra el código de forma atómica. Un fallo resta un intento;
	// devuelve domain.ErrCodeInvalid o, al agotar los intentos, domain.ErrCodeExhausted.
	Consume(ctx context.Context, purpose domain.CodePurpose, subject, codeHash string) error
	// AcquireCooldown devuelve false si ya se envió un código hace menos de d.
	AcquireCooldown(ctx context.Context, purpose domain.CodePurpose, subject string, d time.Duration) (bool, error)
}

// Mailer envía los correos transaccionales. Implementación: SMTP genérico.
type Mailer interface {
	SendEmailVerification(ctx context.Context, to, name, code string, validFor time.Duration) error
}
