package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// OAuthUseCases es lo que el handler necesita de service.OAuthService.
type OAuthUseCases interface {
	Start(ctx context.Context, consents service.OAuthConsents) (authURL, state string, err error)
	Callback(ctx context.Context, code, state string, meta domain.SessionMeta) (service.AuthResult, error)
}

// OAuthHandler atiende /api/v1/auth/google. Lo llama el BFF, que redirige al navegador:
// la vuelta de Google llega al dominio de la app, donde se guarda la cookie de sesión.
type OAuthHandler struct {
	oauth OAuthUseCases
}

func NewOAuthHandler(oauth OAuthUseCases) *OAuthHandler { return &OAuthHandler{oauth: oauth} }

type oauthStartRequest struct {
	AdultDeclared bool `json:"adult_declared"`
	AcceptLegal   bool `json:"accept_legal"`
}

type oauthCallbackRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// POST /api/v1/auth/google/start → {url, state}. El BFF guarda el state en una cookie propia
// y lo compara a la vuelta, para que nadie inicie el flujo por otra persona.
func (h *OAuthHandler) Start(c fiber.Ctx) error {
	var req oauthStartRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	authURL, state, err := h.oauth.Start(c.Context(), service.OAuthConsents{
		AdultDeclared: req.AdultDeclared, AcceptLegal: req.AcceptLegal,
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"url": authURL, "state": state})
}

// POST /api/v1/auth/google/callback → misma respuesta que el inicio de sesión.
func (h *OAuthHandler) Callback(c fiber.Ctx) error {
	var req oauthCallbackRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Code == "" || req.State == "" {
		return domain.ErrOAuthState
	}
	res, err := h.oauth.Callback(c.Context(), req.Code, req.State, sessionMeta(c))
	if err != nil {
		return err
	}
	return c.JSON(toAuthResponse(res))
}
