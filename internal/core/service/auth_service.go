package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// LegalVersions identifica los textos que el usuario acepta al registrarse (Ley 29733).
type LegalVersions struct {
	Terms   string
	Privacy string
}

type RegisterInput struct {
	Email         string
	Name          string
	Password      string
	AdultDeclared bool // declara ser mayor de 18 años
	AcceptLegal   bool // acepta términos y política de privacidad
	Meta          domain.SessionMeta
}

// AuthResult es lo que recibe el BFF: el token va a la cookie; el usuario, a la interfaz.
type AuthResult struct {
	Token   string
	Session domain.Session
	User    domain.User
	// VerificationSent indica si salió el correo con el código (solo en el registro).
	VerificationSent bool
}

// VerificationSender envía el código de verificación de correo tras el registro.
type VerificationSender interface {
	SendCode(ctx context.Context, user domain.User) error
}

// AuthDeps agrupa las dependencias de AuthService.
type AuthDeps struct {
	Accounts  port.AccountRepository
	Audit     port.AuditLog
	Passwords *PasswordPolicy
	Sessions  *SessionService
	Limiter   port.RateLimiter
	// LoginLimit: intentos por cuenta (5 cada 15 minutos, decisión de clarify).
	LoginLimit   domain.Limit
	Verification VerificationSender
	Clock        port.Clock
	IDs          port.IDGenerator
	Legal        LegalVersions
}

// AuthService implementa el proveedor de acceso "password". Google (y, en la feature 022,
// el celular) serán servicios hermanos que terminan en el mismo SessionService.
type AuthService struct {
	AuthDeps
}

func NewAuthService(deps AuthDeps) *AuthService { return &AuthService{AuthDeps: deps} }

// Register crea la cuenta con rol cliente e inicia sesión. El correo queda sin verificar:
// puede navegar, pero no transaccionar hasta confirmarlo (docs/05).
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (AuthResult, error) {
	email, err := domain.NormalizeEmail(in.Email)
	if err != nil {
		return AuthResult{}, err
	}
	name, err := domain.NormalizeName(in.Name)
	if err != nil {
		return AuthResult{}, err
	}
	switch {
	case !in.AdultDeclared:
		return AuthResult{}, domain.ErrAdultRequired
	case !in.AcceptLegal:
		return AuthResult{}, domain.ErrConsentRequired
	}
	hash, err := s.Passwords.HashNew(ctx, in.Password, email)
	if err != nil {
		return AuthResult{}, err
	}

	now := s.Clock.Now()
	user := newClientUser(s.IDs.NewID(), email, name, now)
	identity := domain.AuthIdentity{
		ID: s.IDs.NewID(), UserID: user.ID, Provider: domain.ProviderPassword,
		ProviderSubject: email, SecretHash: hash, CreatedAt: now, LastUsedAt: &now,
	}
	if err := s.Accounts.CreateAccount(ctx, newAccount(user, identity, s.Legal, in.Meta.IP)); err != nil {
		return AuthResult{}, err
	}
	result, err := openSession(ctx, s.Sessions, user, domain.ProviderPassword, in.Meta)
	if err != nil {
		return AuthResult{}, err
	}
	// Si el correo no sale, la cuenta ya existe: el usuario puede pedir otro código.
	result.VerificationSent = s.Verification.SendCode(ctx, user) == nil
	return result, nil
}

// Login valida correo y contraseña. Cualquier fallo devuelve el mismo error genérico.
func (s *AuthService) Login(ctx context.Context, email, password string, meta domain.SessionMeta) (AuthResult, error) {
	normalized, err := domain.NormalizeEmail(email)
	if err != nil {
		return AuthResult{}, domain.ErrInvalidCredentials
	}
	// Límite por cuenta (exista o no, para no revelarla). Frena a quien prueba contraseñas
	// desde muchas IP contra la misma cuenta; el límite por IP lo pone el middleware.
	limitKey := "login:account:" + normalized
	allowed, retryAfter, err := s.Limiter.Allow(ctx, limitKey, s.LoginLimit)
	if err != nil {
		return AuthResult{}, err
	}
	if !allowed {
		return AuthResult{}, &domain.RateLimitError{RetryAfter: retryAfter}
	}
	identity, err := s.Accounts.FindIdentity(ctx, domain.ProviderPassword, normalized)
	if errors.Is(err, domain.ErrNotFound) {
		s.Passwords.VerifyDummy(password)
		return AuthResult{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return AuthResult{}, err
	}

	match, needsRehash, err := s.Passwords.Verify(password, identity.SecretHash)
	if err != nil {
		return AuthResult{}, fmt.Errorf("login: %w", err)
	}
	if !match {
		return AuthResult{}, domain.ErrInvalidCredentials
	}

	user, err := s.Accounts.FindUser(ctx, identity.UserID)
	if err != nil {
		return AuthResult{}, err
	}
	if user.CanSignIn() != nil {
		// Una cuenta eliminada responde igual que una contraseña incorrecta.
		return AuthResult{}, domain.ErrInvalidCredentials
	}

	_ = s.Limiter.Reset(ctx, limitKey) // un inicio correcto limpia los intentos fallidos
	if needsRehash {
		s.rehash(ctx, identity, password, user.ID, meta.IP)
	}
	if err := s.Accounts.MarkIdentityUsed(ctx, identity.ID, s.Clock.Now()); err != nil {
		return AuthResult{}, err
	}
	result, err := openSession(ctx, s.Sessions, user, domain.ProviderPassword, meta)
	if err != nil {
		return AuthResult{}, err
	}
	recordLogin(ctx, s.Audit, result, meta.IP)
	return result, nil
}

// Logout cierra la sesión actual.
func (s *AuthService) Logout(ctx context.Context, session domain.Session) error {
	return s.Sessions.Revoke(ctx, session)
}

// LogoutAll cierra todas las sesiones del usuario, incluida la actual.
func (s *AuthService) LogoutAll(ctx context.Context, session domain.Session, ip string) error {
	if err := s.Sessions.RevokeAll(ctx, session.UserID, ""); err != nil {
		return err
	}
	_ = s.Audit.Record(ctx, domain.AuditEntry{
		ActorID: session.UserID, Action: domain.AuditSessionsRevoked, Entity: "user", EntityID: session.UserID, IP: ip,
	})
	return nil
}

// Piezas comunes a todos los proveedores de acceso (contraseña, Google y, en 022, celular).

// newClientUser: toda cuenta nueva nace activa y con rol cliente.
func newClientUser(id, email, name string, now time.Time) domain.User {
	return domain.User{
		ID: id, Email: email, Name: name, AdultDeclaredAt: &now, Status: domain.UserActive,
		Roles: []domain.Role{domain.RoleClient}, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
}

// newAccount arma el alta con los consentimientos vigentes (Ley 29733) y su auditoría.
func newAccount(user domain.User, identity domain.AuthIdentity, legal LegalVersions, ip string) port.NewAccount {
	return port.NewAccount{
		User:     user,
		Identity: identity,
		Consents: []domain.Consent{
			{Purpose: domain.ConsentTerms, Version: legal.Terms, GrantedAt: user.CreatedAt},
			{Purpose: domain.ConsentPrivacy, Version: legal.Privacy, GrantedAt: user.CreatedAt},
		},
		Audit: domain.AuditEntry{
			ActorID: user.ID, Action: domain.AuditUserRegistered, Entity: "user", EntityID: user.ID,
			After: map[string]any{"provider": identity.Provider}, IP: ip,
		},
	}
}

func openSession(ctx context.Context, sessions *SessionService, user domain.User, provider domain.AuthProvider, meta domain.SessionMeta) (AuthResult, error) {
	token, session, err := sessions.Create(ctx, user, provider, meta)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{Token: token, Session: session, User: user}, nil
}

// recordLogin audita un inicio de sesión; si la auditoría falla, el acceso sigue.
func recordLogin(ctx context.Context, audit port.AuditLog, r AuthResult, ip string) {
	_ = audit.Record(ctx, domain.AuditEntry{
		ActorID: r.User.ID, Action: domain.AuditUserLogin, Entity: "user", EntityID: r.User.ID,
		After: map[string]any{"provider": r.Session.Provider, "session": r.Session.ID}, IP: ip,
	})
}

// rehash actualiza hashes creados con parámetros de argon2id anteriores. Si falla, el login sigue.
func (s *AuthService) rehash(ctx context.Context, identity domain.AuthIdentity, password, userID, ip string) {
	hash, err := s.Passwords.Rehash(password)
	if err != nil || s.Accounts.UpdateIdentitySecret(ctx, identity.ID, hash) != nil {
		return
	}
	_ = s.Audit.Record(ctx, domain.AuditEntry{
		ActorID: userID, Action: domain.AuditPasswordRehashed, Entity: "auth_identity", EntityID: identity.ID, IP: ip,
	})
}
