package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// --- Dobles de prueba ---

type memoryAccounts struct {
	mu         sync.Mutex
	users      map[string]domain.User
	identities map[string]domain.AuthIdentity // clave: provider|subject
	audits     []domain.AuditEntry
	consents   []domain.Consent
}

func newMemoryAccounts() *memoryAccounts {
	return &memoryAccounts{users: map[string]domain.User{}, identities: map[string]domain.AuthIdentity{}}
}

func identityKey(p domain.AuthProvider, subject string) string { return string(p) + "|" + subject }

func (m *memoryAccounts) CreateAccount(_ context.Context, a port.NewAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == a.User.Email {
			return domain.ErrEmailTaken
		}
	}
	m.users[a.User.ID] = a.User
	m.identities[identityKey(a.Identity.Provider, a.Identity.ProviderSubject)] = a.Identity
	m.consents = append(m.consents, a.Consents...)
	m.audits = append(m.audits, a.Audit)
	return nil
}

func (m *memoryAccounts) FindIdentity(_ context.Context, p domain.AuthProvider, subject string) (domain.AuthIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.identities[identityKey(p, subject)]
	if !ok {
		return domain.AuthIdentity{}, domain.ErrNotFound
	}
	return id, nil
}

func (m *memoryAccounts) FindUser(_ context.Context, id string) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (m *memoryAccounts) FindUserByEmail(_ context.Context, email string) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memoryAccounts) SetPassword(_ context.Context, up port.PasswordUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := identityKey(domain.ProviderPassword, up.Email)
	identity, ok := m.identities[key]
	if !ok {
		identity = domain.AuthIdentity{ID: up.IdentityID, UserID: up.UserID, Provider: domain.ProviderPassword, ProviderSubject: up.Email}
	}
	identity.SecretHash = up.SecretHash
	m.identities[key] = identity
	if up.VerifyEmail {
		u := m.users[up.UserID]
		u.EmailVerifiedAt = &up.At
		m.users[up.UserID] = u
	}
	m.audits = append(m.audits, up.Audit)
	return nil
}

func (m *memoryAccounts) FindUserIdentity(_ context.Context, userID string, p domain.AuthProvider) (domain.AuthIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.identities {
		if id.UserID == userID && id.Provider == p {
			return id, nil
		}
	}
	return domain.AuthIdentity{}, domain.ErrNotFound
}

func (m *memoryAccounts) UpdateName(_ context.Context, userID, name string, audit domain.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.users[userID]
	u.Name = name
	m.users[userID] = u
	m.audits = append(m.audits, audit)
	return nil
}

func (m *memoryAccounts) ChangeRoles(_ context.Context, c port.RoleChange) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[c.UserID]
	if !ok {
		return domain.ErrNotFound
	}
	roles := slices.DeleteFunc(slices.Concat(u.Roles, c.Add), func(r domain.Role) bool { return slices.Contains(c.Remove, r) })
	slices.Sort(roles)
	u.Roles = roles
	m.users[c.UserID] = u
	m.audits = append(m.audits, c.Audit)
	return nil
}

func (m *memoryAccounts) ChangeStatus(_ context.Context, c port.StatusChange) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[c.UserID]
	if !ok {
		return domain.ErrNotFound
	}
	u.Status, u.SuspendedReason = c.Status, c.Reason
	m.users[c.UserID] = u
	m.audits = append(m.audits, c.Audit)
	return nil
}

func (m *memoryAccounts) MarkIdentityUsed(context.Context, string, time.Time) error { return nil }

func (m *memoryAccounts) UpdateIdentitySecret(_ context.Context, identityID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, id := range m.identities {
		if id.ID == identityID {
			id.SecretHash = hash
			m.identities[k] = id
		}
	}
	return nil
}

func (m *memoryAccounts) MarkEmailVerified(_ context.Context, userID string, at time.Time, audit domain.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.EmailVerifiedAt = &at
	m.users[userID] = u
	m.audits = append(m.audits, audit)
	return nil
}

func (m *memoryAccounts) Record(_ context.Context, e domain.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, e)
	return nil
}

// plainHasher: "v1:" o "v2:" + contraseña. v1 simula parámetros viejos (pide recalcular).
type plainHasher struct{ calls int }

func (h *plainHasher) Hash(p string) (string, error) { return "v2:" + p, nil }
func (h *plainHasher) Verify(p, encoded string) (bool, bool, error) {
	h.calls++
	version, secret, _ := strings.Cut(encoded, ":")
	return secret == p, version == "v1", nil
}

type breachedSet map[string]bool

func (b breachedSet) IsBreached(_ context.Context, p string) (bool, error) {
	return b[strings.ToLower(p)], nil
}

// memoryLimiter cuenta intentos por clave sin ventana de tiempo.
type memoryLimiter struct {
	mu     sync.Mutex
	counts map[string]int
}

func newMemoryLimiter() *memoryLimiter { return &memoryLimiter{counts: map[string]int{}} }

func (l *memoryLimiter) Allow(_ context.Context, key string, limit domain.Limit) (bool, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts[key] >= limit.Max {
		return false, limit.Window, nil
	}
	l.counts[key]++
	return true, 0, nil
}

func (l *memoryLimiter) Reset(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.counts, key)
	return nil
}

// recordingSender registra a quién se le envió el código de verificación.
type recordingSender struct{ sent []string }

func (r *recordingSender) SendCode(_ context.Context, u domain.User) error {
	r.sent = append(r.sent, u.Email)
	return nil
}

type seqIDs struct{ n int }

func (s *seqIDs) NewID() string { s.n++; return fmt.Sprintf("id-%d", s.n) }

type authFixture struct {
	svc      *AuthService
	accounts *memoryAccounts
	hasher   *plainHasher
	sessions *SessionService
}

func newAuthFixture(t *testing.T) authFixture {
	t.Helper()
	accounts := newMemoryAccounts()
	hasher := &plainHasher{}
	sessions, _, clock := newTestSessions()
	passwords, err := NewPasswordPolicy(hasher, breachedSet{"1234567890": true})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAuthService(AuthDeps{
		Accounts: accounts, Audit: accounts, Passwords: passwords,
		Sessions: sessions, Limiter: newMemoryLimiter(), LoginLimit: domain.Limit{Max: 5, Window: 15 * time.Minute},
		Verification: &recordingSender{}, Clock: clock, IDs: &seqIDs{},
		Legal: LegalVersions{Terms: "2026-09", Privacy: "2026-09"},
	})
	return authFixture{svc: svc, accounts: accounts, hasher: hasher, sessions: sessions}
}

func validRegister() RegisterInput {
	return RegisterInput{Email: " Ana@Correo.PE ", Name: "Ana Pérez", Password: "tornillo-verde-9", AdultDeclared: true, AcceptLegal: true}
}

// --- Registro ---

func TestRegister(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()

	res, err := f.svc.Register(ctx, validRegister())
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Email != "ana@correo.pe" || !res.User.HasRole(domain.RoleClient) || res.User.IsVerified() {
		t.Fatalf("usuario inesperado: %+v", res.User)
	}
	if res.User.CanTransact() {
		t.Fatal("sin verificar el correo no puede transaccionar")
	}
	if _, err := f.sessions.Authenticate(ctx, res.Token); err != nil {
		t.Fatal("el registro inicia sesión")
	}
	identity, _ := f.accounts.FindIdentity(ctx, domain.ProviderPassword, "ana@correo.pe")
	if identity.SecretHash == "tornillo-verde-9" {
		t.Fatal("la contraseña no se guarda en claro")
	}
	if sender := f.svc.Verification.(*recordingSender); !res.VerificationSent || len(sender.sent) != 1 || sender.sent[0] != "ana@correo.pe" {
		t.Fatal("el registro envía el código de verificación al correo")
	}
	if len(f.accounts.consents) != 2 || f.accounts.audits[0].Action != domain.AuditUserRegistered {
		t.Fatalf("faltan consentimientos o auditoría: %+v %+v", f.accounts.consents, f.accounts.audits)
	}

	if _, err := f.svc.Register(ctx, validRegister()); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("un correo repetido debe fallar, llegó %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RegisterInput)
		want   error
	}{
		{"correo inválido", func(in *RegisterInput) { in.Email = "sin-arroba" }, domain.ErrInvalidEmail},
		{"nombre vacío", func(in *RegisterInput) { in.Name = "  " }, domain.ErrInvalidName},
		{"sin declarar mayoría de edad", func(in *RegisterInput) { in.AdultDeclared = false }, domain.ErrAdultRequired},
		{"sin aceptar términos", func(in *RegisterInput) { in.AcceptLegal = false }, domain.ErrConsentRequired},
		{"contraseña corta", func(in *RegisterInput) { in.Password = "corta" }, domain.ErrPasswordTooShort},
		{"contraseña filtrada", func(in *RegisterInput) { in.Password = "1234567890" }, domain.ErrPasswordBreached},
		{"contraseña igual al correo", func(in *RegisterInput) { in.Password = "ana@correo.pe" }, domain.ErrPasswordIsEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAuthFixture(t)
			in := validRegister()
			tt.mutate(&in)
			if _, err := f.svc.Register(context.Background(), in); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, se esperaba %v", err, tt.want)
			}
			if len(f.accounts.users) != 0 {
				t.Fatal("no debe crearse la cuenta")
			}
		})
	}
}

// --- Inicio de sesión ---

func TestLogin(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Register(ctx, validRegister()); err != nil {
		t.Fatal(err)
	}

	res, err := f.svc.Login(ctx, "ANA@correo.pe", "tornillo-verde-9", domain.SessionMeta{IP: "1.2.3.4"})
	if err != nil || res.Token == "" || res.User.Email != "ana@correo.pe" {
		t.Fatalf("login correcto falló: %+v %v", res, err)
	}
	if last := f.accounts.audits[len(f.accounts.audits)-1]; last.Action != domain.AuditUserLogin {
		t.Fatalf("el login se audita, llegó %+v", last)
	}
}

func TestLoginFailuresAreGeneric(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	reg, _ := f.svc.Register(ctx, validRegister())

	deleted := reg.User
	deleted.Status = domain.UserDeleted
	f.accounts.users[deleted.ID] = deleted

	f2 := newAuthFixture(t)
	_, _ = f2.svc.Register(ctx, validRegister())

	for _, tt := range []struct {
		name, email, password string
		f                     authFixture
	}{
		{"contraseña incorrecta", "ana@correo.pe", "otra-contrasena-9", f2},
		{"correo inexistente", "nadie@correo.pe", "tornillo-verde-9", f2},
		{"correo mal escrito", "no-es-correo", "tornillo-verde-9", f2},
		{"cuenta eliminada", "ana@correo.pe", "tornillo-verde-9", f},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.f.svc.Login(ctx, tt.email, tt.password, domain.SessionMeta{}); !errors.Is(err, domain.ErrInvalidCredentials) {
				t.Fatalf("debe responder el mismo error genérico, llegó %v", err)
			}
		})
	}
}

func TestLoginWithUnknownEmailStillVerifiesAHash(t *testing.T) {
	f := newAuthFixture(t)
	before := f.hasher.calls
	_, _ = f.svc.Login(context.Background(), "nadie@correo.pe", "x-cualquier-9", domain.SessionMeta{})
	if f.hasher.calls != before+1 {
		t.Fatal("con un correo inexistente también se verifica un hash, para no revelar por el tiempo de respuesta")
	}
}

func TestLoginRehashesOldPasswords(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Register(ctx, validRegister()); err != nil {
		t.Fatal(err)
	}
	key := identityKey(domain.ProviderPassword, "ana@correo.pe")
	old := f.accounts.identities[key]
	old.SecretHash = "v1:tornillo-verde-9"
	f.accounts.identities[key] = old

	if _, err := f.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	if got := f.accounts.identities[key].SecretHash; got != "v2:tornillo-verde-9" {
		t.Fatalf("el hash viejo debe recalcularse, quedó %q", got)
	}
}

func TestLogoutAll(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	reg, _ := f.svc.Register(ctx, validRegister())
	other, _ := f.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{})

	if err := f.svc.LogoutAll(ctx, reg.Session, ""); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{reg.Token, other.Token} {
		if _, err := f.sessions.Authenticate(ctx, tok); !errors.Is(err, domain.ErrSessionInvalid) {
			t.Fatal("cerrar en todos invalida todas las sesiones")
		}
	}
}

func TestLoginLimitPerAccount(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Register(ctx, validRegister())

	for range 5 {
		_, _ = f.svc.Login(ctx, "ana@correo.pe", "mala-contrasena-9", domain.SessionMeta{})
	}
	_, err := f.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{})
	var rl *domain.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("tras 5 intentos se bloquea incluso con la contraseña correcta, llegó %v", err)
	}

	// Un correo inexistente se limita igual: el bloqueo no revela si la cuenta existe.
	for range 5 {
		_, _ = f.svc.Login(ctx, "nadie@correo.pe", "x-cualquier-9", domain.SessionMeta{})
	}
	if _, err := f.svc.Login(ctx, "nadie@correo.pe", "x-cualquier-9", domain.SessionMeta{}); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("se esperaba el mismo bloqueo, llegó %v", err)
	}
}

func TestSuccessfulLoginResetsAttempts(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Register(ctx, validRegister())
	for range 4 {
		_, _ = f.svc.Login(ctx, "ana@correo.pe", "mala-contrasena-9", domain.SessionMeta{})
	}
	if _, err := f.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		_, _ = f.svc.Login(ctx, "ana@correo.pe", "mala-contrasena-9", domain.SessionMeta{})
	}
	if _, err := f.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{}); err != nil {
		t.Fatalf("tras un inicio correcto el contador vuelve a cero: %v", err)
	}
}
