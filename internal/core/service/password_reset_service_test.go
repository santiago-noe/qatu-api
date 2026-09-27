package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type resetFixture struct {
	auth    authFixture
	reset   *PasswordResetService
	mailer  *capturingMailer
	codes   *memoryCodes
	session AuthResult // sesión abierta antes de recuperar la contraseña
}

// newResetFixture registra a Ana (contraseña "tornillo-verde-9") y arma el servicio de recuperación
// con los mismos dobles de prueba que el registro.
func newResetFixture(t *testing.T) resetFixture {
	t.Helper()
	f := newAuthFixture(t)
	res, err := f.svc.Register(context.Background(), validRegister())
	if err != nil {
		t.Fatal(err)
	}
	store, mailer := newMemoryCodes(), &capturingMailer{}
	codes := NewOneTimeCodes(store, mailer, "secreto", testCodesConfig)
	reset := NewPasswordResetService(f.accounts, codes, f.svc.Passwords, f.sessions, f.svc.Clock, &seqIDs{n: 100})
	return resetFixture{auth: f, reset: reset, mailer: mailer, codes: store, session: res}
}

func TestPasswordResetHappyPath(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()

	if err := f.reset.Request(ctx, " ANA@correo.pe "); err != nil {
		t.Fatal(err)
	}
	if f.mailer.to != "ana@correo.pe" || f.mailer.purpose != domain.CodePasswordReset {
		t.Fatalf("debe enviar un código de recuperación: %+v", f.mailer)
	}

	if err := f.reset.Reset(ctx, "ana@correo.pe", f.mailer.code, "nueva-escalera-7", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatal("la contraseña anterior ya no sirve")
	}
	if _, err := f.auth.svc.Login(ctx, "ana@correo.pe", "nueva-escalera-7", domain.SessionMeta{}); err != nil {
		t.Fatalf("la contraseña nueva sirve: %v", err)
	}
	if _, err := f.auth.sessions.Authenticate(ctx, f.session.Token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatal("recuperar la contraseña cierra las sesiones abiertas")
	}
	user, _ := f.auth.accounts.FindUserByEmail(ctx, "ana@correo.pe")
	if !user.IsVerified() {
		t.Fatal("recibir el código en el correo lo verifica")
	}
	if err := f.reset.Reset(ctx, "ana@correo.pe", f.mailer.code, "otra-escalera-8", ""); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatal("el código se usa una sola vez")
	}
}

func TestPasswordResetDoesNotRevealAccounts(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()

	for _, email := range []string{"nadie@correo.pe", "no-es-correo"} {
		if err := f.reset.Request(ctx, email); err != nil {
			t.Fatalf("%s: debe responder igual que una cuenta existente, llegó %v", email, err)
		}
	}
	if f.mailer.sends != 0 {
		t.Fatal("a un correo desconocido no se envía nada")
	}

	_ = f.reset.Request(ctx, "ana@correo.pe")
	if err := f.reset.Request(ctx, "ana@correo.pe"); err != nil {
		t.Fatalf("un segundo pedido antes del minuto no debe delatar la cuenta, llegó %v", err)
	}

	// Una contraseña débil falla igual exista o no la cuenta.
	for _, email := range []string{"ana@correo.pe", "nadie@correo.pe"} {
		if err := f.reset.Reset(ctx, email, "123456", "corta", ""); !errors.Is(err, domain.ErrPasswordTooShort) {
			t.Fatalf("%s: se esperaba ErrPasswordTooShort, llegó %v", email, err)
		}
	}
	if err := f.reset.Reset(ctx, "nadie@correo.pe", "123456", "nueva-escalera-7", ""); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatalf("una cuenta inexistente responde como código inválido, llegó %v", err)
	}
}

func TestPasswordResetRejectsBreachedPassword(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	_ = f.reset.Request(ctx, "ana@correo.pe")
	if err := f.reset.Reset(ctx, "ana@correo.pe", f.mailer.code, "1234567890", ""); !errors.Is(err, domain.ErrPasswordBreached) {
		t.Fatalf("se aplica la misma política que al registrarse, llegó %v", err)
	}
	if err := f.reset.Reset(ctx, "ana@correo.pe", f.mailer.code, "nueva-escalera-7", ""); err != nil {
		t.Fatalf("una contraseña rechazada no gasta el código: %v", err)
	}
}

func TestPasswordResetIgnoresDeletedAccounts(t *testing.T) {
	f := newResetFixture(t)
	u := f.session.User
	u.Status = domain.UserDeleted
	f.auth.accounts.users[u.ID] = u

	if err := f.reset.Request(context.Background(), "ana@correo.pe"); err != nil || f.mailer.sends != 0 {
		t.Fatal("una cuenta eliminada no recibe códigos de recuperación")
	}
}
