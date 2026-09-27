package port

import (
	"context"
	"time"
)

// OAuthProfile es lo que un proveedor externo confirma de la persona (Google hoy).
type OAuthProfile struct {
	Subject       string // identificador estable en el proveedor ("sub"); nunca cambia
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
}

// OAuthProvider es un proveedor OAuth 2.0 / OpenID Connect con PKCE. Implementación: Google.
type OAuthProvider interface {
	// AuthURL arma la URL del proveedor con el state y el reto PKCE derivado de verifier.
	// Devuelve domain.ErrOAuthUnavailable si el proveedor no está configurado.
	AuthURL(state, verifier string) (string, error)
	// Exchange canjea el código y valida el id_token. Un código inválido o un token que no
	// pasa la validación devuelven domain.ErrOAuthFailed.
	Exchange(ctx context.Context, code, verifier string) (OAuthProfile, error)
}

// OAuthState es lo que se guarda entre la ida al proveedor y la vuelta.
type OAuthState struct {
	Verifier string `json:"verifier"` // PKCE: nunca sale del servidor
	// Consentimientos dados antes de ir al proveedor; solo se usan si se crea la cuenta.
	AdultDeclared bool `json:"adult_declared"`
	AcceptLegal   bool `json:"accept_legal"`
}

// OAuthStateStore guarda el state de un solo uso. Implementación: Redis.
type OAuthStateStore interface {
	Save(ctx context.Context, state string, data OAuthState, ttl time.Duration) error
	// Consume devuelve y borra el state de forma atómica, o domain.ErrOAuthState.
	Consume(ctx context.Context, state string) (OAuthState, error)
}
