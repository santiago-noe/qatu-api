package domain

import (
	"slices"
	"time"
)

// Session es una sesión iniciada. No depende de cómo entró el usuario:
// Provider solo queda como dato de auditoría (contraseña, Google o, en 022, celular).
type Session struct {
	// ID es el hash del token; el token real solo lo tiene el navegador.
	ID        string       `json:"id"`
	UserID    string       `json:"user_id"`
	Roles     []Role       `json:"roles"`
	Provider  AuthProvider `json:"provider"`
	CreatedAt time.Time    `json:"created_at"`
	ExpiresAt time.Time    `json:"expires_at"`
	RenewedAt time.Time    `json:"renewed_at"`
	IP        string       `json:"ip,omitempty"`
	UserAgent string       `json:"user_agent,omitempty"`
}

func (s Session) IsExpired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

func (s Session) HasAnyRole(roles ...Role) bool {
	return slices.ContainsFunc(roles, func(r Role) bool { return slices.Contains(s.Roles, r) })
}

// SessionMeta son datos del dispositivo para la lista "mis sesiones activas".
type SessionMeta struct {
	IP        string
	UserAgent string
}
