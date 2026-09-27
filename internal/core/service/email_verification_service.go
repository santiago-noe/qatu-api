package service

import (
	"context"
	"fmt"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// EmailVerificationService confirma el correo con un código de 6 dígitos (nivel 0, docs/05).
type EmailVerificationService struct {
	accounts port.AccountRepository
	codes    *OneTimeCodes
	mailer   port.Mailer
	clock    port.Clock
}

func NewEmailVerificationService(accounts port.AccountRepository, codes *OneTimeCodes, mailer port.Mailer, clock port.Clock) *EmailVerificationService {
	return &EmailVerificationService{accounts: accounts, codes: codes, mailer: mailer, clock: clock}
}

// SendCode envía un código nuevo al correo del usuario (como máximo uno por minuto).
func (s *EmailVerificationService) SendCode(ctx context.Context, user domain.User) error {
	if user.EmailVerifiedAt != nil {
		return domain.ErrEmailAlreadyVerified
	}
	if user.Email == "" {
		return domain.ErrInvalidEmail
	}
	code, err := s.codes.Issue(ctx, domain.CodeEmailVerification, user.ID)
	if err != nil {
		return err
	}
	if err := s.mailer.SendEmailVerification(ctx, user.Email, user.Name, code, s.codes.ValidFor()); err != nil {
		return fmt.Errorf("verificación de correo: enviar: %w", err)
	}
	return nil
}

// Resend envía un código nuevo al usuario de la sesión.
func (s *EmailVerificationService) Resend(ctx context.Context, session domain.Session) error {
	user, err := s.accounts.FindUser(ctx, session.UserID)
	if err != nil {
		return err
	}
	return s.SendCode(ctx, user)
}

// Verify confirma el correo si el código es correcto y devuelve el usuario actualizado.
func (s *EmailVerificationService) Verify(ctx context.Context, session domain.Session, code, ip string) (domain.User, error) {
	user, err := s.accounts.FindUser(ctx, session.UserID)
	if err != nil {
		return domain.User{}, err
	}
	if user.EmailVerifiedAt != nil {
		return domain.User{}, domain.ErrEmailAlreadyVerified
	}
	if err := s.codes.Verify(ctx, domain.CodeEmailVerification, user.ID, code); err != nil {
		return domain.User{}, err
	}

	now := s.clock.Now()
	audit := domain.AuditEntry{
		ActorID: user.ID, Action: domain.AuditEmailVerified, Entity: "user", EntityID: user.ID,
		After: map[string]any{"email": user.Email}, IP: ip,
	}
	if err := s.accounts.MarkEmailVerified(ctx, user.ID, now, audit); err != nil {
		return domain.User{}, err
	}
	user.EmailVerifiedAt = &now
	return user, nil
}
