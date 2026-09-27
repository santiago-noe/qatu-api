package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// memoryCodes es un CodeStore en memoria con la misma semántica que el de Redis.
type memoryCodes struct {
	mu        sync.Mutex
	codes     map[string]*storedCode
	cooldowns map[string]bool
}

type storedCode struct {
	hash     string
	attempts int
}

func newMemoryCodes() *memoryCodes {
	return &memoryCodes{codes: map[string]*storedCode{}, cooldowns: map[string]bool{}}
}

func codeKey(p domain.CodePurpose, subject string) string { return string(p) + "|" + subject }

func (m *memoryCodes) Save(_ context.Context, p domain.CodePurpose, subject, hash string, _ time.Duration, attempts int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[codeKey(p, subject)] = &storedCode{hash: hash, attempts: attempts}
	return nil
}

func (m *memoryCodes) Consume(_ context.Context, p domain.CodePurpose, subject, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := codeKey(p, subject)
	c, ok := m.codes[key]
	if !ok {
		return domain.ErrCodeInvalid
	}
	if c.hash == hash {
		delete(m.codes, key)
		return nil
	}
	c.attempts--
	if c.attempts <= 0 {
		delete(m.codes, key)
		return domain.ErrCodeExhausted
	}
	return domain.ErrCodeInvalid
}

func (m *memoryCodes) AcquireCooldown(_ context.Context, p domain.CodePurpose, subject string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := codeKey(p, subject)
	if m.cooldowns[key] {
		return false, nil
	}
	m.cooldowns[key] = true
	return true, nil
}

func (m *memoryCodes) resetCooldowns() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cooldowns = map[string]bool{}
}

// capturingMailer guarda el último código enviado.
type capturingMailer struct {
	to, code string
	sends    int
	fail     error
}

func (c *capturingMailer) SendEmailVerification(_ context.Context, to, _, code string, _ time.Duration) error {
	if c.fail != nil {
		return c.fail
	}
	c.to, c.code = to, code
	c.sends++
	return nil
}

var testCodesConfig = CodesConfig{TTL: 15 * time.Minute, MaxAttempts: 3, ResendCooldown: time.Minute}

type verificationFixture struct {
	svc      *EmailVerificationService
	accounts *memoryAccounts
	codes    *memoryCodes
	mailer   *capturingMailer
	session  domain.Session
}

func newVerificationFixture(t *testing.T) verificationFixture {
	t.Helper()
	accounts := newMemoryAccounts()
	user := domain.User{ID: "u-ana", Email: "ana@correo.pe", Name: "Ana", Status: domain.UserActive, Roles: []domain.Role{domain.RoleClient}}
	accounts.users[user.ID] = user
	store := newMemoryCodes()
	mailer := &capturingMailer{}
	codes := NewOneTimeCodes(store, "secreto", testCodesConfig)
	svc := NewEmailVerificationService(accounts, codes, mailer, &fakeClock{now: time.Now()})
	return verificationFixture{svc: svc, accounts: accounts, codes: store, mailer: mailer, session: domain.Session{UserID: user.ID}}
}

func TestEmailVerificationHappyPath(t *testing.T) {
	f := newVerificationFixture(t)
	ctx := context.Background()

	if err := f.svc.Resend(ctx, f.session); err != nil {
		t.Fatal(err)
	}
	if f.mailer.to != "ana@correo.pe" || !isDigits(f.mailer.code, 6) {
		t.Fatalf("debe enviar un código de 6 dígitos al correo: %+v", f.mailer)
	}
	for _, stored := range f.codes.codes {
		if stored.hash == f.mailer.code {
			t.Fatal("el código no se guarda en claro")
		}
	}

	user, err := f.svc.Verify(ctx, f.session, f.mailer.code, "1.2.3.4")
	if err != nil || user.EmailVerifiedAt == nil || !user.CanTransact() {
		t.Fatalf("Verify = %+v, %v", user, err)
	}
	if last := f.accounts.audits[len(f.accounts.audits)-1]; last.Action != domain.AuditEmailVerified {
		t.Fatalf("la verificación se audita: %+v", last)
	}

	if _, err := f.svc.Verify(ctx, f.session, f.mailer.code, ""); !errors.Is(err, domain.ErrEmailAlreadyVerified) {
		t.Fatalf("no se verifica dos veces, llegó %v", err)
	}
	if err := f.svc.Resend(ctx, f.session); !errors.Is(err, domain.ErrEmailAlreadyVerified) {
		t.Fatalf("no se reenvía a un correo ya verificado, llegó %v", err)
	}
}

func TestEmailVerificationWrongCodes(t *testing.T) {
	f := newVerificationFixture(t)
	ctx := context.Background()
	_ = f.svc.Resend(ctx, f.session)
	good := f.mailer.code
	wrong := "000000"
	if wrong == good {
		wrong = "111111"
	}

	if _, err := f.svc.Verify(ctx, f.session, "12ab", ""); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatalf("un código mal formado es inválido, llegó %v", err)
	}
	if _, err := f.svc.Verify(ctx, f.session, wrong, ""); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatalf("un código incorrecto es inválido, llegó %v", err)
	}
	_, _ = f.svc.Verify(ctx, f.session, wrong, "")
	if _, err := f.svc.Verify(ctx, f.session, wrong, ""); !errors.Is(err, domain.ErrCodeExhausted) {
		t.Fatalf("al tercer fallo se agotan los intentos, llegó %v", err)
	}
	if _, err := f.svc.Verify(ctx, f.session, good, ""); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatal("tras agotar los intentos, ni el código correcto sirve: hay que pedir otro")
	}
}

func TestEmailVerificationResendCooldownAndNewCode(t *testing.T) {
	f := newVerificationFixture(t)
	ctx := context.Background()
	_ = f.svc.Resend(ctx, f.session)
	first := f.mailer.code

	if err := f.svc.Resend(ctx, f.session); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("no se reenvía antes de un minuto, llegó %v", err)
	}

	f.codes.resetCooldowns() // pasó el minuto
	if err := f.svc.Resend(ctx, f.session); err != nil {
		t.Fatal(err)
	}
	if f.mailer.code != first {
		if _, err := f.svc.Verify(ctx, f.session, first, ""); !errors.Is(err, domain.ErrCodeInvalid) {
			t.Fatal("un código nuevo invalida el anterior")
		}
	}
	if _, err := f.svc.Verify(ctx, f.session, f.mailer.code, ""); err != nil {
		t.Fatalf("el código nuevo sirve: %v", err)
	}
}

func TestCodesArePurposeBound(t *testing.T) {
	store := newMemoryCodes()
	codes := NewOneTimeCodes(store, "secreto", testCodesConfig)
	ctx := context.Background()
	code, _ := codes.Issue(ctx, domain.CodeEmailVerification, "u1")
	if err := codes.Verify(ctx, domain.CodePasswordReset, "u1", code); !errors.Is(err, domain.ErrCodeInvalid) {
		t.Fatal("un código de verificación de correo no sirve para recuperar la contraseña")
	}
}

func TestRandomDigitsDistribution(t *testing.T) {
	seen := map[byte]int{}
	for range 2000 {
		code, err := randomDigits(6)
		if err != nil || !isDigits(code, 6) {
			t.Fatalf("código inválido %q %v", code, err)
		}
		for i := range code {
			seen[code[i]]++
		}
	}
	if len(seen) != 10 {
		t.Fatalf("deben aparecer los 10 dígitos: %v", seen)
	}
}
