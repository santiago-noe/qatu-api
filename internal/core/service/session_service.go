package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// SessionConfig: 30 días renovables con el uso (decisión de clarify de la feature 001).
type SessionConfig struct {
	TTL time.Duration
	// RenewAfter evita escribir en Redis en cada petición: solo se renueva si pasó este tiempo.
	RenewAfter time.Duration
}

// SessionService es común a todos los proveedores de acceso: contraseña, Google y,
// en la feature 022, celular. Todos terminan en Create.
type SessionService struct {
	store port.SessionStore
	clock port.Clock
	cfg   SessionConfig
}

func NewSessionService(store port.SessionStore, clock port.Clock, cfg SessionConfig) *SessionService {
	return &SessionService{store: store, clock: clock, cfg: cfg}
}

// Create inicia una sesión y devuelve el token para la cookie. En Redis solo se guarda su hash.
func (s *SessionService) Create(ctx context.Context, user domain.User, provider domain.AuthProvider, meta domain.SessionMeta) (string, domain.Session, error) {
	if err := user.CanSignIn(); err != nil {
		return "", domain.Session{}, err
	}
	token, err := newToken()
	if err != nil {
		return "", domain.Session{}, err
	}
	now := s.clock.Now()
	session := domain.Session{
		ID:        SessionID(token),
		UserID:    user.ID,
		Roles:     slices.Clone(user.Roles),
		Provider:  provider,
		CreatedAt: now,
		RenewedAt: now,
		ExpiresAt: now.Add(s.cfg.TTL),
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	}
	if err := s.store.Save(ctx, session); err != nil {
		return "", domain.Session{}, fmt.Errorf("sesión: %w", err)
	}
	return token, session, nil
}

// Authenticate valida el token y renueva el vencimiento si corresponde.
func (s *SessionService) Authenticate(ctx context.Context, token string) (domain.Session, error) {
	if token == "" {
		return domain.Session{}, domain.ErrSessionInvalid
	}
	session, err := s.store.Get(ctx, SessionID(token))
	if err != nil {
		return domain.Session{}, err
	}
	now := s.clock.Now()
	if session.IsExpired(now) {
		return domain.Session{}, domain.ErrSessionInvalid
	}
	if now.Sub(session.RenewedAt) >= s.cfg.RenewAfter {
		session, err = s.store.Update(ctx, session.ID, func(x *domain.Session) {
			x.RenewedAt, x.ExpiresAt = now, now.Add(s.cfg.TTL)
		})
		if err != nil && !errors.Is(err, domain.ErrSessionInvalid) {
			return domain.Session{}, fmt.Errorf("sesión: renovar: %w", err)
		}
		return session, err
	}
	return session, nil
}

// CompleteTwoFactor marca que la sesión confirmó el segundo paso. Es por sesión: otro
// dispositivo del mismo usuario debe confirmar su propio código.
func (s *SessionService) CompleteTwoFactor(ctx context.Context, id string) (domain.Session, error) {
	now := s.clock.Now()
	return s.store.Update(ctx, id, func(x *domain.Session) { x.TwoFactorAt = &now })
}

// Revoke cierra la sesión actual (cerrar sesión en este dispositivo).
func (s *SessionService) Revoke(ctx context.Context, session domain.Session) error {
	return s.store.Delete(ctx, session.UserID, session.ID)
}

// RevokeByID cierra una sesión del propio usuario desde "mis sesiones activas".
func (s *SessionService) RevokeByID(ctx context.Context, userID, id string) error {
	return s.store.Delete(ctx, userID, id)
}

// RevokeAll cierra todas las sesiones del usuario salvo keepID (vacío = todas).
// Se usa en "cerrar en todos", al cambiar la contraseña, al suspender y al cambiar roles.
func (s *SessionService) RevokeAll(ctx context.Context, userID, keepID string) error {
	return s.store.DeleteAllForUser(ctx, userID, keepID)
}

func (s *SessionService) List(ctx context.Context, userID string) ([]domain.Session, error) {
	return s.store.ListForUser(ctx, userID)
}

// SessionID es el hash SHA-256 del token: un volcado de Redis no permite secuestrar sesiones.
func SessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newToken genera 256 bits aleatorios en base64 URL.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sesión: token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
