package service

import (
	"context"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// TwoFactorService: segundo paso obligatorio para los roles internos (soporte, moderador,
// admin). El código llega al correo. No depende del proveedor de acceso: una sesión de
// contraseña, de Google o, en la feature 022, de celular queda pendiente igual.
type TwoFactorService struct {
	accounts port.AccountRepository
	audit    port.AuditLog
	codes    *OneTimeCodes
	sessions *SessionService
}

func NewTwoFactorService(accounts port.AccountRepository, audit port.AuditLog, codes *OneTimeCodes, sessions *SessionService) *TwoFactorService {
	return &TwoFactorService{accounts: accounts, audit: audit, codes: codes, sessions: sessions}
}

// Send envía el código al correo del usuario de la sesión (como máximo uno por minuto).
func (s *TwoFactorService) Send(ctx context.Context, session domain.Session) error {
	if !session.NeedsTwoFactor() {
		return domain.ErrTwoFactorNotPending
	}
	user, err := s.accounts.FindUser(ctx, session.UserID)
	if err != nil {
		return err
	}
	return s.codes.Send(ctx, domain.CodeTwoFactor, user)
}

// Verify consume el código y habilita las rutas internas en esta sesión.
func (s *TwoFactorService) Verify(ctx context.Context, session domain.Session, code, ip string) error {
	if !session.NeedsTwoFactor() {
		return domain.ErrTwoFactorNotPending
	}
	if err := s.codes.Verify(ctx, domain.CodeTwoFactor, session.UserID, code); err != nil {
		return err
	}
	if _, err := s.sessions.CompleteTwoFactor(ctx, session.ID); err != nil {
		return err
	}
	_ = s.audit.Record(ctx, domain.AuditEntry{
		ActorID: session.UserID, Action: domain.AuditTwoFactorPassed, Entity: "user", EntityID: session.UserID,
		After: map[string]any{"session": session.ID}, IP: ip,
	})
	return nil
}
