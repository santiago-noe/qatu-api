package port

import (
	"context"
	"time"
)

// PasswordHasher calcula y verifica hashes de contraseña (argon2id).
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify indica si coincide y si conviene recalcular el hash con parámetros más nuevos.
	Verify(password, encoded string) (match, needsRehash bool, err error)
}

// BreachedPasswords indica si una contraseña aparece en filtraciones conocidas.
type BreachedPasswords interface {
	IsBreached(ctx context.Context, password string) (bool, error)
}

// HumanVerifier confirma que una petición la hizo una persona (Cloudflare Turnstile).
// action es el formulario de origen: un token de "register" no sirve en "password_forgot".
// Devuelve domain.ErrHumanCheckFailed o, si el servicio no responde, domain.ErrHumanCheckUnavailable.
type HumanVerifier interface {
	Verify(ctx context.Context, token, action, ip string) error
}

// Clock y IDGenerator permiten pruebas deterministas de los servicios.
type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() string
}
