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

// Clock y IDGenerator permiten pruebas deterministas de los servicios.
type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() string
}
