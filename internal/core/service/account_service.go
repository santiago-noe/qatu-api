package service

import (
	"context"
	"errors"
	"slices"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// AccountDeps agrupa las dependencias de AccountService.
type AccountDeps struct {
	Accounts  port.AccountRepository
	Audit     port.AuditLog
	Passwords *PasswordPolicy
	Sessions  *SessionService
	Limiter   port.RateLimiter
	// PasswordLimit limita los intentos de contraseña actual incorrecta (mismo criterio que el login).
	PasswordLimit domain.Limit
	Clock         port.Clock
	IDs           port.IDGenerator
}

// AccountService es "mi cuenta": perfil, contraseña y sesiones del usuario conectado.
type AccountService struct {
	AccountDeps
}

func NewAccountService(deps AccountDeps) *AccountService { return &AccountService{AccountDeps: deps} }

// Me devuelve el usuario de la sesión actual.
func (s *AccountService) Me(ctx context.Context, session domain.Session) (domain.User, error) {
	return s.Accounts.FindUser(ctx, session.UserID)
}

// ProfileUpdate: los campos nil no cambian. Ciudad y distrito llegan con la feature 002.
type ProfileUpdate struct {
	Name *string
}

func (s *AccountService) UpdateProfile(ctx context.Context, session domain.Session, in ProfileUpdate, ip string) (domain.User, error) {
	user, err := s.Accounts.FindUser(ctx, session.UserID)
	if err != nil || in.Name == nil {
		return user, err
	}
	name, err := domain.NormalizeName(*in.Name)
	if err != nil {
		return domain.User{}, err
	}
	if name == user.Name {
		return user, nil
	}
	audit := domain.AuditEntry{ActorID: user.ID, Action: domain.AuditProfileUpdated, Entity: "user", EntityID: user.ID,
		Before: map[string]any{"name": user.Name}, After: map[string]any{"name": name}, IP: ip}
	if err := s.Accounts.UpdateName(ctx, user.ID, name, audit); err != nil {
		return domain.User{}, err
	}
	user.Name = name
	return user, nil
}

// ChangePassword cambia la contraseña (o la agrega si la cuenta entró con Google) y cierra
// las demás sesiones; la actual se conserva.
func (s *AccountService) ChangePassword(ctx context.Context, session domain.Session, current, next, ip string) error {
	user, err := s.Accounts.FindUser(ctx, session.UserID)
	if err != nil {
		return err
	}
	identity, err := s.Accounts.FindUserIdentity(ctx, user.ID, domain.ProviderPassword)
	hasPassword := err == nil
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if hasPassword {
		if err := s.checkCurrent(ctx, user.ID, current, identity.SecretHash); err != nil {
			return err
		}
	}

	hash, err := s.Passwords.HashNew(ctx, next, user.Email)
	if err != nil {
		return err
	}
	identityID := identity.ID
	if !hasPassword {
		identityID = s.IDs.NewID()
	}
	update := port.PasswordUpdate{
		UserID: user.ID, IdentityID: identityID, Email: user.Email, SecretHash: hash, At: s.Clock.Now(),
		Audit: domain.AuditEntry{ActorID: user.ID, Action: domain.AuditPasswordChanged, Entity: "user", EntityID: user.ID,
			After: map[string]any{"added": !hasPassword}, IP: ip},
	}
	if err := s.Accounts.SetPassword(ctx, update); err != nil {
		return err
	}
	return s.Sessions.RevokeAll(ctx, user.ID, session.ID)
}

// SessionView es una sesión en la lista "mis sesiones activas".
type SessionView struct {
	domain.Session
	Current bool
}

// ListSessions devuelve las sesiones del usuario, la más reciente primero.
func (s *AccountService) ListSessions(ctx context.Context, session domain.Session) ([]SessionView, error) {
	sessions, err := s.Sessions.List(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(sessions, func(a, b domain.Session) int { return b.RenewedAt.Compare(a.RenewedAt) })
	views := make([]SessionView, len(sessions))
	for i, item := range sessions {
		views[i] = SessionView{Session: item, Current: item.ID == session.ID}
	}
	return views, nil
}

// RevokeSession cierra una sesión propia por su ID (puede ser la actual).
func (s *AccountService) RevokeSession(ctx context.Context, session domain.Session, id, ip string) error {
	if err := s.Sessions.RevokeByID(ctx, session.UserID, id); err != nil {
		return err
	}
	_ = s.Audit.Record(ctx, domain.AuditEntry{ActorID: session.UserID, Action: domain.AuditSessionRevoked,
		Entity: "session", EntityID: id, IP: ip})
	return nil
}

// checkCurrent verifica la contraseña actual con límite de intentos por usuario.
func (s *AccountService) checkCurrent(ctx context.Context, userID, current, hash string) error {
	key := "password_change:user:" + userID
	allowed, retryAfter, err := s.Limiter.Allow(ctx, key, s.PasswordLimit)
	if err != nil {
		return err
	}
	if !allowed {
		return &domain.RateLimitError{RetryAfter: retryAfter}
	}
	match, _, err := s.Passwords.Verify(current, hash)
	if err != nil {
		return err
	}
	if !match {
		return domain.ErrCurrentPasswordInvalid
	}
	_ = s.Limiter.Reset(ctx, key)
	return nil
}
