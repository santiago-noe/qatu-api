package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// memorySessions es un SessionStore en memoria para probar el servicio sin Redis.
type memorySessions struct {
	mu       sync.Mutex
	sessions map[string]domain.Session
	extends  int
}

func newMemorySessions() *memorySessions {
	return &memorySessions{sessions: map[string]domain.Session{}}
}

func (m *memorySessions) Save(_ context.Context, s domain.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *memorySessions) Get(_ context.Context, id string) (domain.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return domain.Session{}, domain.ErrSessionInvalid
	}
	return s, nil
}

func (m *memorySessions) Extend(_ context.Context, id string, renewedAt, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return domain.ErrSessionInvalid
	}
	s.RenewedAt, s.ExpiresAt = renewedAt, expiresAt
	m.sessions[id] = s
	m.extends++
	return nil
}

func (m *memorySessions) Delete(_ context.Context, userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok && s.UserID == userID {
		delete(m.sessions, id)
	}
	return nil
}

func (m *memorySessions) DeleteAllForUser(_ context.Context, userID, exceptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.UserID == userID && id != exceptID {
			delete(m.sessions, id)
		}
	}
	return nil
}

func (m *memorySessions) ListForUser(_ context.Context, userID string) ([]domain.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Session
	for _, s := range m.sessions {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out, nil
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

var testSessionConfig = SessionConfig{TTL: 30 * 24 * time.Hour, RenewAfter: 24 * time.Hour}

func newTestSessions() (*SessionService, *memorySessions, *fakeClock) {
	store := newMemorySessions()
	clock := &fakeClock{now: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	return NewSessionService(store, clock, testSessionConfig), store, clock
}

var ana = domain.User{ID: "u-ana", Status: domain.UserActive, Roles: []domain.Role{domain.RoleClient}}

func TestSessionCreateAndAuthenticate(t *testing.T) {
	svc, store, _ := newTestSessions()
	ctx := context.Background()

	token, session, err := svc.Create(ctx, ana, domain.ProviderPassword, domain.SessionMeta{IP: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 40 {
		t.Fatalf("el token debe tener 256 bits, llegó %q", token)
	}
	if _, stored := store.sessions[token]; stored {
		t.Fatal("el token no debe guardarse en claro")
	}
	if session.ID != SessionID(token) {
		t.Fatal("el ID debe ser el hash del token")
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil || got.UserID != "u-ana" || !got.HasAnyRole(domain.RoleClient) {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}
	if _, err := svc.Authenticate(ctx, "token-falso"); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("un token desconocido es inválido, llegó %v", err)
	}
	if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatal("un token vacío es inválido")
	}
}

func TestSessionRenewsWithUse(t *testing.T) {
	svc, store, clock := newTestSessions()
	ctx := context.Background()
	token, first, _ := svc.Create(ctx, ana, domain.ProviderPassword, domain.SessionMeta{})

	clock.Advance(2 * time.Hour)
	if _, err := svc.Authenticate(ctx, token); err != nil || store.extends != 0 {
		t.Fatalf("antes de RenewAfter no se escribe en Redis: extends=%d err=%v", store.extends, err)
	}

	clock.Advance(25 * time.Hour)
	renewed, err := svc.Authenticate(ctx, token)
	if err != nil || store.extends != 1 {
		t.Fatalf("después de RenewAfter se renueva: extends=%d err=%v", store.extends, err)
	}
	if !renewed.ExpiresAt.After(first.ExpiresAt) {
		t.Fatal("la renovación debe extender el vencimiento")
	}
}

func TestSessionExpires(t *testing.T) {
	svc, _, clock := newTestSessions()
	ctx := context.Background()
	token, _, _ := svc.Create(ctx, ana, domain.ProviderPassword, domain.SessionMeta{})

	clock.Advance(31 * 24 * time.Hour)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("sin uso por 31 días la sesión vence, llegó %v", err)
	}
}

func TestSessionRevoke(t *testing.T) {
	svc, _, _ := newTestSessions()
	ctx := context.Background()
	phone, _, _ := svc.Create(ctx, ana, domain.ProviderPassword, domain.SessionMeta{})
	laptop, current, _ := svc.Create(ctx, ana, domain.ProviderGoogle, domain.SessionMeta{})
	tablet, _, _ := svc.Create(ctx, ana, domain.ProviderPassword, domain.SessionMeta{})
	otherUser, _, _ := svc.Create(ctx, domain.User{ID: "u-beto", Status: domain.UserActive}, domain.ProviderPassword, domain.SessionMeta{})

	// Cerrar en todos menos en el dispositivo actual.
	if err := svc.RevokeAll(ctx, "u-ana", current.ID); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{phone, tablet} {
		if _, err := svc.Authenticate(ctx, tok); !errors.Is(err, domain.ErrSessionInvalid) {
			t.Fatal("las demás sesiones deben quedar cerradas")
		}
	}
	if _, err := svc.Authenticate(ctx, laptop); err != nil {
		t.Fatal("la sesión actual se conserva")
	}
	if _, err := svc.Authenticate(ctx, otherUser); err != nil {
		t.Fatal("no se tocan las sesiones de otros usuarios")
	}

	// Cerrar la sesión actual.
	if err := svc.Revoke(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, laptop); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatal("tras cerrar sesión el token ya no sirve")
	}
}

func TestDeletedAccountCannotCreateSession(t *testing.T) {
	svc, _, _ := newTestSessions()
	deleted := domain.User{ID: "u-x", Status: domain.UserDeleted}
	if _, _, err := svc.Create(context.Background(), deleted, domain.ProviderPassword, domain.SessionMeta{}); !errors.Is(err, domain.ErrAccountDeleted) {
		t.Fatalf("una cuenta eliminada no inicia sesión, llegó %v", err)
	}
}

// Prueba de extensibilidad (spec 001): un proveedor nuevo, como el OTP de la feature 022,
// crea sesiones con el mismo servicio, sin cambios en el usuario ni en las sesiones.
func TestSessionIsIndependentOfProvider(t *testing.T) {
	svc, _, _ := newTestSessions()
	ctx := context.Background()
	for _, provider := range []domain.AuthProvider{domain.ProviderPassword, domain.ProviderGoogle, domain.ProviderPhoneOTP, "fake_otp"} {
		token, _, err := svc.Create(ctx, ana, provider, domain.SessionMeta{})
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		got, err := svc.Authenticate(ctx, token)
		if err != nil || got.Provider != provider || got.UserID != ana.ID {
			t.Fatalf("%s: la sesión debe funcionar igual para cualquier proveedor: %+v %v", provider, got, err)
		}
	}
}
