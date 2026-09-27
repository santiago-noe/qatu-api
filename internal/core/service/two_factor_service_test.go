package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type twoFactorFixture struct {
	svc      *TwoFactorService
	sessions *SessionService
	accounts *memoryAccounts
	mailer   *capturingMailer
	session  domain.Session
}

func newTwoFactorFixture(t *testing.T, roles ...domain.Role) twoFactorFixture {
	t.Helper()
	accounts := newMemoryAccounts()
	user := domain.User{ID: "u-staff", Email: "staff@qatu.pe", Name: "Staff", Status: domain.UserActive, Roles: roles}
	accounts.users[user.ID] = user
	sessions, _, _ := newTestSessions()
	_, session, err := sessions.Create(context.Background(), user, domain.ProviderPassword, domain.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	mailer := &capturingMailer{}
	codes := NewOneTimeCodes(newMemoryCodes(), mailer, "secreto", testCodesConfig)
	return twoFactorFixture{
		svc: NewTwoFactorService(accounts, accounts, codes, sessions), sessions: sessions,
		accounts: accounts, mailer: mailer, session: session,
	}
}

func TestTwoFactorHappyPath(t *testing.T) {
	f := newTwoFactorFixture(t, domain.RoleClient, domain.RoleAdmin)
	ctx := context.Background()

	if err := f.svc.Send(ctx, f.session); err != nil {
		t.Fatal(err)
	}
	if f.mailer.to != "staff@qatu.pe" || f.mailer.purpose != domain.CodeTwoFactor {
		t.Fatalf("debe enviar un código de dos pasos al correo: %+v", f.mailer)
	}
	if err := f.svc.Verify(ctx, f.session, f.mailer.code, "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	stored, err := f.sessions.store.Get(ctx, f.session.ID)
	if err != nil || stored.NeedsTwoFactor() {
		t.Fatalf("la sesión queda confirmada: %+v %v", stored, err)
	}
	if last := f.accounts.audits[len(f.accounts.audits)-1]; last.Action != domain.AuditTwoFactorPassed {
		t.Fatalf("el segundo paso se audita: %+v", last)
	}
	if err := f.svc.Verify(ctx, stored, f.mailer.code, ""); !errors.Is(err, domain.ErrTwoFactorNotPending) {
		t.Fatalf("una sesión confirmada no vuelve a pedir código, llegó %v", err)
	}
}

func TestTwoFactorRejectsWrongOrForeignCodes(t *testing.T) {
	f := newTwoFactorFixture(t, domain.RoleClient, domain.RoleModerator)
	ctx := context.Background()

	if err := f.svc.Verify(ctx, f.session, "123456", ""); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatalf("sin código enviado no se confirma, llegó %v", err)
	}
	// Un código de verificación de correo no sirve como segundo paso.
	emailCode, _ := f.svc.codes.Issue(ctx, domain.CodeEmailVerification, f.session.UserID)
	if err := f.svc.Verify(ctx, f.session, emailCode, ""); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatalf("un código de otra finalidad es inválido, llegó %v", err)
	}
	if stored, _ := f.sessions.store.Get(ctx, f.session.ID); !stored.NeedsTwoFactor() {
		t.Fatal("tras un código incorrecto la sesión sigue pendiente")
	}
}

func TestTwoFactorNotForClients(t *testing.T) {
	f := newTwoFactorFixture(t, domain.RoleClient, domain.RoleLender)
	if err := f.svc.Send(context.Background(), f.session); !errors.Is(err, domain.ErrTwoFactorNotPending) {
		t.Fatalf("un cliente no recibe códigos de dos pasos, llegó %v", err)
	}
	if f.mailer.sends != 0 {
		t.Fatal("no debe salir ningún correo")
	}
}
