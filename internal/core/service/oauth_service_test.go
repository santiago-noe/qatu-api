package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// fakeOAuth imita a Google: la URL lleva el state, y el canje solo funciona con el código
// "codigo-ok" y el mismo verificador PKCE que se usó al armar la URL.
type fakeOAuth struct {
	verifier string
	profile  port.OAuthProfile
}

func (f *fakeOAuth) AuthURL(state, verifier string) (string, error) {
	f.verifier = verifier
	return "https://accounts.google.com/auth?state=" + state, nil
}

func (f *fakeOAuth) Exchange(_ context.Context, code, verifier string) (port.OAuthProfile, error) {
	if code != "codigo-ok" || verifier != f.verifier {
		return port.OAuthProfile{}, domain.ErrOAuthFailed
	}
	return f.profile, nil
}

// memoryStates tiene la misma semántica que el store de Redis (un solo uso).
type memoryStates struct {
	mu     sync.Mutex
	states map[string]port.OAuthState
}

func (m *memoryStates) Save(_ context.Context, state string, data port.OAuthState, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[state] = data
	return nil
}

func (m *memoryStates) Consume(_ context.Context, state string) (port.OAuthState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.states[state]
	if !ok {
		return port.OAuthState{}, domain.ErrOAuthState
	}
	delete(m.states, state)
	return data, nil
}

var (
	anaGoogle = port.OAuthProfile{Subject: "sub-ana", Email: "Ana@Gmail.com", EmailVerified: true,
		Name: "Ana Quispe", AvatarURL: "https://lh3.googleusercontent.com/ana"}
	accepted = OAuthConsents{AdultDeclared: true, AcceptLegal: true}
)

type oauthFixture struct {
	svc      *OAuthService
	accounts *memoryAccounts
	google   *fakeOAuth
}

func newOAuthFixture(t *testing.T, profile port.OAuthProfile) oauthFixture {
	t.Helper()
	accounts := newMemoryAccounts()
	sessions, _, clock := newTestSessions()
	google := &fakeOAuth{profile: profile}
	svc := NewOAuthService(OAuthDeps{
		Accounts: accounts, Audit: accounts, Sessions: sessions, Provider: google,
		States: &memoryStates{states: map[string]port.OAuthState{}}, Clock: clock, IDs: &seqIDs{},
		Legal: LegalVersions{Terms: "t-1", Privacy: "p-1"}, StateTTL: 10 * time.Minute,
	})
	return oauthFixture{svc: svc, accounts: accounts, google: google}
}

// signIn hace la ida y la vuelta completas con los consentimientos dados.
func (f oauthFixture) signIn(t *testing.T, consents OAuthConsents) (AuthResult, error) {
	t.Helper()
	_, state, err := f.svc.Start(context.Background(), consents)
	if err != nil {
		t.Fatal(err)
	}
	return f.svc.Callback(context.Background(), "codigo-ok", state, domain.SessionMeta{IP: "190.1.2.3"})
}

func (f oauthFixture) lastAudit() domain.AuditEntry {
	return f.accounts.audits[len(f.accounts.audits)-1]
}

func TestOAuthRegistersNewAccount(t *testing.T) {
	f := newOAuthFixture(t, anaGoogle)
	res, err := f.signIn(t, accepted)
	if err != nil {
		t.Fatal(err)
	}
	u := res.User
	if u.Email != "ana@gmail.com" || u.Name != "Ana Quispe" || !u.IsVerified() || !u.HasRole(domain.RoleClient) ||
		u.AvatarURL == "" || u.AdultDeclaredAt == nil {
		t.Fatalf("cuenta nueva = %+v", u)
	}
	if res.Token == "" || res.Session.Provider != domain.ProviderGoogle {
		t.Fatalf("debe abrir una sesión de Google: %+v", res.Session)
	}
	if len(f.accounts.consents) != 2 {
		t.Fatalf("se registran términos y privacidad: %+v", f.accounts.consents)
	}
	if f.lastAudit().Action != domain.AuditUserLogin {
		t.Fatalf("el inicio de sesión se audita: %+v", f.lastAudit())
	}

	// Segunda vez: misma identidad, misma cuenta, aunque ya no marque las casillas.
	again, err := f.signIn(t, OAuthConsents{})
	if err != nil || again.User.ID != u.ID {
		t.Fatalf("la segunda vez entra a la misma cuenta: %+v %v", again.User, err)
	}
}

func TestOAuthNewAccountNeedsConsents(t *testing.T) {
	f := newOAuthFixture(t, anaGoogle)
	if _, err := f.signIn(t, OAuthConsents{AcceptLegal: true}); !errors.Is(err, domain.ErrOAuthSignupRequired) {
		t.Fatalf("sin declarar la mayoría de edad no se crea la cuenta, llegó %v", err)
	}
	if len(f.accounts.users) != 0 {
		t.Fatal("no debe crearse ninguna cuenta")
	}
}

func TestOAuthLinksVerifiedAccount(t *testing.T) {
	f := newOAuthFixture(t, anaGoogle)
	verified := time.Now()
	f.accounts.users["u-ana"] = domain.User{ID: "u-ana", Email: "ana@gmail.com", EmailVerifiedAt: &verified,
		Status: domain.UserActive, Roles: []domain.Role{domain.RoleClient}}

	res, err := f.signIn(t, OAuthConsents{})
	if err != nil || res.User.ID != "u-ana" {
		t.Fatalf("debe entrar a la cuenta existente: %+v %v", res.User, err)
	}
	if _, err := f.accounts.FindIdentity(context.Background(), domain.ProviderGoogle, "sub-ana"); err != nil {
		t.Fatal("Google queda vinculado a la cuenta")
	}
	if !containsAction(f.accounts.audits, domain.AuditIdentityLinked) {
		t.Fatal("la vinculación se audita")
	}
}

func TestOAuthDoesNotLinkUnverifiedAccount(t *testing.T) {
	f := newOAuthFixture(t, anaGoogle)
	// Alguien registró este correo con contraseña pero nunca lo confirmó: podría no ser su dueño.
	f.accounts.users["u-x"] = domain.User{ID: "u-x", Email: "ana@gmail.com", Status: domain.UserActive}

	if _, err := f.signIn(t, accepted); !errors.Is(err, domain.ErrOAuthLinkNeedsPassword) {
		t.Fatalf("no se vincula una cuenta sin correo verificado, llegó %v", err)
	}
	if _, err := f.accounts.FindIdentity(context.Background(), domain.ProviderGoogle, "sub-ana"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("no debe quedar ninguna vinculación")
	}
}

func TestOAuthRejectsUnverifiedGoogleEmail(t *testing.T) {
	profile := anaGoogle
	profile.EmailVerified = false
	f := newOAuthFixture(t, profile)
	if _, err := f.signIn(t, accepted); !errors.Is(err, domain.ErrOAuthEmailUnverified) {
		t.Fatalf("sin correo verificado en Google no se entra, llegó %v", err)
	}
}

func TestOAuthStateIsSingleUse(t *testing.T) {
	f := newOAuthFixture(t, anaGoogle)
	ctx := context.Background()
	_, state, _ := f.svc.Start(ctx, accepted)
	if _, err := f.svc.Callback(ctx, "codigo-ok", state, domain.SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Callback(ctx, "codigo-ok", state, domain.SessionMeta{}); !errors.Is(err, domain.ErrOAuthState) {
		t.Fatalf("un state ya usado se rechaza, llegó %v", err)
	}
	if _, err := f.svc.Callback(ctx, "codigo-ok", "inventado", domain.SessionMeta{}); !errors.Is(err, domain.ErrOAuthState) {
		t.Fatalf("un state desconocido se rechaza, llegó %v", err)
	}
}

func TestOAuthStaffNeedsTwoFactor(t *testing.T) {
	f := newOAuthFixture(t, anaGoogle)
	verified := time.Now()
	f.accounts.users["u-admin"] = domain.User{ID: "u-admin", Email: "ana@gmail.com", EmailVerifiedAt: &verified,
		Status: domain.UserActive, Roles: []domain.Role{domain.RoleClient, domain.RoleAdmin}}
	res, err := f.signIn(t, OAuthConsents{})
	if err != nil || !res.Session.NeedsTwoFactor() {
		t.Fatalf("con Google, un rol interno también confirma el segundo paso: %+v %v", res.Session, err)
	}
}

func TestDisplayName(t *testing.T) {
	if got := displayName("  Ana   Quispe ", "ana@gmail.com"); got != "Ana Quispe" {
		t.Fatalf("usa el nombre de Google: %q", got)
	}
	if got := displayName("", "ana.q@gmail.com"); got != "ana.q" {
		t.Fatalf("sin nombre usa el correo: %q", got)
	}
}

func containsAction(audits []domain.AuditEntry, action string) bool {
	for _, a := range audits {
		if a.Action == action {
			return true
		}
	}
	return false
}
