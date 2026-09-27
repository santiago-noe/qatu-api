package service

import (
	"context"
	"fmt"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// PasswordPolicy reúne la política de contraseñas y el hash. La usan el registro, la
// recuperación y el cambio de contraseña, para que la regla sea una sola.
type PasswordPolicy struct {
	hasher   port.PasswordHasher
	breached port.BreachedPasswords
	// dummyHash se verifica cuando el usuario no existe, para que la respuesta tarde lo mismo
	// y no revele qué correos están registrados.
	dummyHash string
}

func NewPasswordPolicy(hasher port.PasswordHasher, breached port.BreachedPasswords) (*PasswordPolicy, error) {
	dummy, err := hasher.Hash("qatu-dummy-password-for-timing")
	if err != nil {
		return nil, err
	}
	return &PasswordPolicy{hasher: hasher, breached: breached, dummyHash: dummy}, nil
}

// HashNew valida una contraseña nueva (longitud, distinta del correo, no filtrada) y la hashea.
func (p *PasswordPolicy) HashNew(ctx context.Context, password, email string) (string, error) {
	if err := domain.ValidatePasswordShape(password, email); err != nil {
		return "", err
	}
	breached, err := p.breached.IsBreached(ctx, password)
	if err != nil {
		return "", fmt.Errorf("contraseñas filtradas: %w", err)
	}
	if breached {
		return "", domain.ErrPasswordBreached
	}
	hash, err := p.hasher.Hash(password)
	if err != nil {
		return "", fmt.Errorf("contraseña: %w", err)
	}
	return hash, nil
}

// Verify compara con el hash guardado e indica si conviene recalcularlo.
func (p *PasswordPolicy) Verify(password, hash string) (match, needsRehash bool, err error) {
	return p.hasher.Verify(password, hash)
}

// VerifyDummy gasta el mismo tiempo que una verificación real (usuario inexistente).
func (p *PasswordPolicy) VerifyDummy(password string) {
	_, _, _ = p.hasher.Verify(password, p.dummyHash)
}

// Rehash recalcula el hash con los parámetros actuales (sin volver a validar la política).
func (p *PasswordPolicy) Rehash(password string) (string, error) {
	return p.hasher.Hash(password)
}
