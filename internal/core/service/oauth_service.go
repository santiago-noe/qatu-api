package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// OAuthConsents son las casillas que la persona marca antes de ir a Google (pantalla de
// registro). Solo se usan si la cuenta no existe y hay que crearla.
type OAuthConsents struct {
	AdultDeclared bool
	AcceptLegal   bool
}

// OAuthDeps agrupa las dependencias de OAuthService.
type OAuthDeps struct {
	Accounts port.AccountRepository
	Audit    port.AuditLog
	Sessions *SessionService
	Provider port.OAuthProvider
	States   port.OAuthStateStore
	Clock    port.Clock
	IDs      port.IDGenerator
	Legal    LegalVersions
	// StateTTL: tiempo para volver de Google (10 minutos, spec 001).
	StateTTL time.Duration
}

// OAuthService implementa el proveedor de acceso "google". Termina en el mismo
// SessionService que la contraseña: la sesión no depende de cómo se entró.
type OAuthService struct {
	OAuthDeps
}

func NewOAuthService(deps OAuthDeps) *OAuthService { return &OAuthService{OAuthDeps: deps} }

// Start genera el state (anti-CSRF) y el verificador PKCE, los guarda y devuelve la URL de Google.
func (s *OAuthService) Start(ctx context.Context, consents OAuthConsents) (authURL, state string, err error) {
	if state, err = newToken(); err != nil {
		return "", "", err
	}
	verifier, err := newToken() // 43 caracteres base64url: válido como code_verifier (RFC 7636)
	if err != nil {
		return "", "", err
	}
	if authURL, err = s.Provider.AuthURL(state, verifier); err != nil {
		return "", "", err
	}
	data := port.OAuthState{Verifier: verifier, AdultDeclared: consents.AdultDeclared, AcceptLegal: consents.AcceptLegal}
	if err := s.States.Save(ctx, state, data, s.StateTTL); err != nil {
		return "", "", err
	}
	return authURL, state, nil
}

// Callback consume el state, canjea el código y entra a la cuenta: la existente, una
// vinculada por correo verificado o una nueva.
func (s *OAuthService) Callback(ctx context.Context, code, state string, meta domain.SessionMeta) (AuthResult, error) {
	stored, err := s.States.Consume(ctx, state)
	if err != nil {
		return AuthResult{}, err
	}
	profile, err := s.Provider.Exchange(ctx, code, stored.Verifier)
	if err != nil {
		return AuthResult{}, err
	}
	user, err := s.resolveUser(ctx, profile, stored, meta.IP)
	if err != nil {
		return AuthResult{}, err
	}
	result, err := openSession(ctx, s.Sessions, user, domain.ProviderGoogle, meta)
	if err != nil {
		return AuthResult{}, err
	}
	recordLogin(ctx, s.Audit, result, meta.IP)
	return result, nil
}

// resolveUser aplica las reglas de vinculación de la spec 001.
func (s *OAuthService) resolveUser(ctx context.Context, p port.OAuthProfile, stored port.OAuthState, ip string) (domain.User, error) {
	identity, err := s.Accounts.FindIdentity(ctx, domain.ProviderGoogle, p.Subject)
	if err == nil {
		_ = s.Accounts.MarkIdentityUsed(ctx, identity.ID, s.Clock.Now())
		return s.Accounts.FindUser(ctx, identity.UserID)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, err
	}

	// Sin correo verificado en Google no se crea ni se vincula nada: no prueba que sea suyo.
	if !p.EmailVerified {
		return domain.User{}, domain.ErrOAuthEmailUnverified
	}
	email, err := domain.NormalizeEmail(p.Email)
	if err != nil {
		return domain.User{}, domain.ErrOAuthFailed
	}
	existing, err := s.Accounts.FindUserByEmail(ctx, email)
	switch {
	case err == nil:
		return s.link(ctx, existing, p, ip)
	case errors.Is(err, domain.ErrNotFound):
		return s.register(ctx, email, p, stored, ip)
	default:
		return domain.User{}, err
	}
}

// link vincula Google a la cuenta con el mismo correo, solo si Qatu también lo verificó.
// Si no, quien registró ese correo sin confirmarlo podría no ser su dueño: debe entrar con
// su contraseña y vincular Google desde el perfil.
func (s *OAuthService) link(ctx context.Context, user domain.User, p port.OAuthProfile, ip string) (domain.User, error) {
	if user.EmailVerifiedAt == nil {
		return domain.User{}, domain.ErrOAuthLinkNeedsPassword
	}
	audit := domain.AuditEntry{
		ActorID: user.ID, Action: domain.AuditIdentityLinked, Entity: "user", EntityID: user.ID,
		After: map[string]any{"provider": domain.ProviderGoogle, "automatic": true}, IP: ip,
	}
	if err := s.Accounts.LinkIdentity(ctx, s.googleIdentity(user.ID, p.Subject), audit); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

// register crea la cuenta con el correo ya verificado por Google. Los consentimientos
// (mayoría de edad, términos) se marcaron antes de salir hacia Google.
func (s *OAuthService) register(ctx context.Context, email string, p port.OAuthProfile, stored port.OAuthState, ip string) (domain.User, error) {
	if !stored.AdultDeclared || !stored.AcceptLegal {
		return domain.User{}, domain.ErrOAuthSignupRequired
	}
	now := s.Clock.Now()
	user := newClientUser(s.IDs.NewID(), email, displayName(p.Name, email), now)
	user.EmailVerifiedAt, user.AvatarURL = &now, p.AvatarURL
	if err := s.Accounts.CreateAccount(ctx, newAccount(user, s.googleIdentity(user.ID, p.Subject), s.Legal, ip)); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (s *OAuthService) googleIdentity(userID, subject string) domain.AuthIdentity {
	now := s.Clock.Now()
	return domain.AuthIdentity{
		ID: s.IDs.NewID(), UserID: userID, Provider: domain.ProviderGoogle,
		ProviderSubject: subject, CreatedAt: now, LastUsedAt: &now,
	}
}

// displayName usa el nombre de Google; si falta o no es válido, la parte del correo antes de la @.
func displayName(googleName, email string) string {
	if name, err := domain.NormalizeName(googleName); err == nil {
		return name
	}
	local, _, _ := strings.Cut(email, "@")
	return local
}
