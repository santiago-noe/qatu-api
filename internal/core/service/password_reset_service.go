package service

import (
	"context"
	"errors"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// PasswordResetService recupera el acceso con un código de 6 dígitos enviado al correo.
// Nunca revela si un correo está registrado.
type PasswordResetService struct {
	accounts  port.AccountRepository
	codes     *OneTimeCodes
	passwords *PasswordPolicy
	sessions  *SessionService
	clock     port.Clock
	ids       port.IDGenerator
}

func NewPasswordResetService(accounts port.AccountRepository, codes *OneTimeCodes, passwords *PasswordPolicy,
	sessions *SessionService, clock port.Clock, ids port.IDGenerator) *PasswordResetService {
	return &PasswordResetService{accounts: accounts, codes: codes, passwords: passwords, sessions: sessions, clock: clock, ids: ids}
}

// Request envía el código si el correo pertenece a una cuenta activa o suspendida.
// Para un correo desconocido responde igual, sin enviar nada.
func (s *PasswordResetService) Request(ctx context.Context, email string) error {
	normalized, err := domain.NormalizeEmail(email)
	if err != nil {
		return nil
	}
	user, err := s.findAccount(ctx, normalized)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.codes.Send(ctx, domain.CodePasswordReset, user)
	if errors.Is(err, domain.ErrTooManyRequests) {
		return nil // ya se envió uno hace menos de un minuto; responder distinto revelaría la cuenta
	}
	return err
}

// Reset cambia la contraseña con el código y cierra todas las sesiones de la cuenta.
func (s *PasswordResetService) Reset(ctx context.Context, email, code, newPassword, ip string) error {
	normalized, err := domain.NormalizeEmail(email)
	if err != nil {
		return domain.ErrCodeInvalid
	}
	// La contraseña se valida antes de buscar la cuenta: la respuesta no cambia según exista o no.
	hash, err := s.passwords.HashNew(ctx, newPassword, normalized)
	if err != nil {
		return err
	}
	user, err := s.findAccount(ctx, normalized)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrCodeInvalid
	}
	if err != nil {
		return err
	}
	if err := s.codes.Verify(ctx, domain.CodePasswordReset, user.ID, code); err != nil {
		return err
	}

	update := port.PasswordUpdate{
		UserID: user.ID, IdentityID: s.ids.NewID(), Email: user.Email, SecretHash: hash, At: s.clock.Now(),
		VerifyEmail: user.EmailVerifiedAt == nil,
		Audit:       domain.AuditEntry{ActorID: user.ID, Action: domain.AuditPasswordReset, Entity: "user", EntityID: user.ID, IP: ip},
	}
	if err := s.accounts.SetPassword(ctx, update); err != nil {
		return err
	}
	// Quien tenía la contraseña anterior pierde el acceso en todos los dispositivos.
	return s.sessions.RevokeAll(ctx, user.ID, "")
}

// findAccount busca por correo ya normalizado; una cuenta eliminada se trata como inexistente.
func (s *PasswordResetService) findAccount(ctx context.Context, email string) (domain.User, error) {
	user, err := s.accounts.FindUserByEmail(ctx, email)
	if err != nil {
		return domain.User{}, err
	}
	if user.CanSignIn() != nil {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}
