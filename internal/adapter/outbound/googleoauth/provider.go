// Package googleoauth implementa port.OAuthProvider con Google (OAuth 2.0 + OpenID Connect, PKCE).
package googleoauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

const (
	issuer  = "https://accounts.google.com"
	jwksURL = "https://www.googleapis.com/oauth2/v3/certs"
)

// endpoint se declara aquí para no depender de golang.org/x/oauth2/google (y sus módulos de GCP).
var endpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type Provider struct {
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// New no llama a la red: las claves públicas de Google se descargan (y cachean) en el primer canje.
func New(ctx context.Context, cfg Config) *Provider {
	keys := oidc.NewRemoteKeySet(ctx, jwksURL)
	return newProvider(cfg, endpoint, oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: cfg.ClientID}))
}

func newProvider(cfg Config, ep oauth2.Endpoint, verifier *oidc.IDTokenVerifier) *Provider {
	return &Provider{
		oauth: &oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: ep, Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: verifier,
	}
}

// AuthURL pide la cuenta con PKCE (S256). select_account deja elegir entre varias cuentas de Google.
func (p *Provider) AuthURL(state, verifier string) (string, error) {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account")), nil
}

type claims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// Exchange canjea el código y valida el id_token: firma con las claves de Google, emisor,
// audiencia (nuestro client ID) y vencimiento.
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (port.OAuthProfile, error) {
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		// Código vencido, ya usado o de otro cliente: Google responde 400 invalid_grant.
		return port.OAuthProfile{}, fmt.Errorf("%w: %s", domain.ErrOAuthFailed, retrieve.ErrorCode)
	}
	if err != nil {
		return port.OAuthProfile{}, fmt.Errorf("google: canjear código: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return port.OAuthProfile{}, fmt.Errorf("%w: sin id_token", domain.ErrOAuthFailed)
	}
	idToken, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return port.OAuthProfile{}, fmt.Errorf("%w: id_token: %v", domain.ErrOAuthFailed, err)
	}
	var c claims
	if err := idToken.Claims(&c); err != nil {
		return port.OAuthProfile{}, fmt.Errorf("%w: claims: %v", domain.ErrOAuthFailed, err)
	}
	return port.OAuthProfile{
		Subject: idToken.Subject, Email: c.Email, EmailVerified: c.EmailVerified, Name: c.Name, AvatarURL: c.Picture,
	}, nil
}

// Disabled se usa en desarrollo sin APP__GOOGLE__CLIENT_ID: el botón responde "no disponible".
type Disabled struct{}

func (Disabled) AuthURL(string, string) (string, error) { return "", domain.ErrOAuthUnavailable }

func (Disabled) Exchange(context.Context, string, string) (port.OAuthProfile, error) {
	return port.OAuthProfile{}, domain.ErrOAuthUnavailable
}
