package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type accountFixture struct {
	auth    authFixture
	svc     *AccountService
	session AuthResult // sesión del registro (la "actual")
}

func newAccountFixture(t *testing.T) accountFixture {
	t.Helper()
	f := newAuthFixture(t)
	res, err := f.svc.Register(context.Background(), validRegister())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAccountService(AccountDeps{
		Accounts: f.accounts, Audit: f.accounts, Passwords: f.svc.Passwords, Sessions: f.sessions,
		Limiter: newMemoryLimiter(), PasswordLimit: domain.Limit{Max: 3, Window: 15 * time.Minute},
		Clock: f.svc.Clock, IDs: &seqIDs{n: 500},
	})
	return accountFixture{auth: f, svc: svc, session: res}
}

func strPtr(s string) *string { return &s }

func TestUpdateProfile(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()

	user, err := f.svc.UpdateProfile(ctx, f.session.Session, ProfileUpdate{Name: strPtr("  Ana   María ")}, "")
	if err != nil || user.Name != "Ana María" {
		t.Fatalf("UpdateProfile = %+v, %v", user, err)
	}
	if last := f.auth.accounts.audits[len(f.auth.accounts.audits)-1]; last.Action != domain.AuditProfileUpdated {
		t.Fatalf("el cambio de perfil se audita: %+v", last)
	}
	if _, err := f.svc.UpdateProfile(ctx, f.session.Session, ProfileUpdate{Name: strPtr("   ")}, ""); !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("un nombre vacío es inválido, llegó %v", err)
	}
	audits := len(f.auth.accounts.audits)
	if _, err := f.svc.UpdateProfile(ctx, f.session.Session, ProfileUpdate{}, ""); err != nil || len(f.auth.accounts.audits) != audits {
		t.Fatal("sin cambios no se escribe ni se audita nada")
	}
}

func TestChangePassword(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	other, _ := f.auth.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{})

	if err := f.svc.ChangePassword(ctx, f.session.Session, "no-es-la-actual", "nueva-escalera-7", ""); !errors.Is(err, domain.ErrCurrentPasswordInvalid) {
		t.Fatalf("sin la contraseña actual no se cambia, llegó %v", err)
	}
	if err := f.svc.ChangePassword(ctx, f.session.Session, "tornillo-verde-9", "1234567890", ""); !errors.Is(err, domain.ErrPasswordBreached) {
		t.Fatalf("se aplica la misma política que al registrarse, llegó %v", err)
	}
	if err := f.svc.ChangePassword(ctx, f.session.Session, "tornillo-verde-9", "nueva-escalera-7", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := f.auth.sessions.Authenticate(ctx, f.session.Token); err != nil {
		t.Fatal("la sesión desde la que se cambió se conserva")
	}
	if _, err := f.auth.sessions.Authenticate(ctx, other.Token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatal("las demás sesiones se cierran")
	}
	if _, err := f.auth.svc.Login(ctx, "ana@correo.pe", "nueva-escalera-7", domain.SessionMeta{}); err != nil {
		t.Fatalf("la contraseña nueva sirve: %v", err)
	}
}

func TestChangePasswordLimitsWrongCurrent(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	for range 3 {
		_ = f.svc.ChangePassword(ctx, f.session.Session, "no-es-la-actual", "nueva-escalera-7", "")
	}
	if err := f.svc.ChangePassword(ctx, f.session.Session, "tornillo-verde-9", "nueva-escalera-7", ""); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("tras varios intentos se bloquea (una sesión robada no puede adivinar la contraseña), llegó %v", err)
	}
}

func TestGoogleOnlyAccountCanAddPassword(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	// Simula una cuenta que entró con Google: sin identidad password.
	for key, id := range f.auth.accounts.identities {
		if id.Provider == domain.ProviderPassword {
			delete(f.auth.accounts.identities, key)
		}
	}
	if err := f.svc.ChangePassword(ctx, f.session.Session, "", "nueva-escalera-7", ""); err != nil {
		t.Fatalf("sin contraseña previa se agrega sin pedir la actual: %v", err)
	}
	if _, err := f.auth.svc.Login(ctx, "ana@correo.pe", "nueva-escalera-7", domain.SessionMeta{}); err != nil {
		t.Fatalf("ya puede entrar con contraseña: %v", err)
	}
}

func TestSessionsListAndRevoke(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()
	f.auth.svc.Clock.(*fakeClock).Advance(time.Hour)
	other, _ := f.auth.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{UserAgent: "Firefox"})

	list, err := f.svc.ListSessions(ctx, f.session.Session)
	if err != nil || len(list) != 2 {
		t.Fatalf("dos sesiones, llegó %d %v", len(list), err)
	}
	if list[0].ID != other.Session.ID || list[0].Current || !list[1].Current {
		t.Fatalf("la más reciente primero y la actual marcada: %+v", list)
	}

	if err := f.svc.RevokeSession(ctx, f.session.Session, other.Session.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.sessions.Authenticate(ctx, other.Token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatal("la sesión cerrada ya no sirve")
	}
}
