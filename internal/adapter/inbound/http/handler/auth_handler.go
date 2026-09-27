package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/middleware"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// AuthUseCases es lo que el handler necesita de service.AuthService.
type AuthUseCases interface {
	Register(ctx context.Context, in service.RegisterInput) (service.AuthResult, error)
	Login(ctx context.Context, email, password string, meta domain.SessionMeta) (service.AuthResult, error)
	Logout(ctx context.Context, session domain.Session) error
	LogoutAll(ctx context.Context, session domain.Session, ip string) error
	Me(ctx context.Context, session domain.Session) (domain.User, error)
}

type AuthHandler struct {
	auth AuthUseCases
}

func NewAuthHandler(auth AuthUseCases) *AuthHandler { return &AuthHandler{auth: auth} }

type registerRequest struct {
	Email         string `json:"email"`
	Name          string `json:"name"`
	Password      string `json:"password"`
	AdultDeclared bool   `json:"adult_declared"`
	AcceptLegal   bool   `json:"accept_legal"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResponse es la vista pública del usuario (sin datos sensibles).
type userResponse struct {
	ID                string        `json:"id"`
	Email             string        `json:"email,omitempty"`
	EmailVerified     bool          `json:"email_verified"`
	Name              string        `json:"name"`
	AvatarURL         string        `json:"avatar_url,omitempty"`
	Roles             []domain.Role `json:"roles"`
	Status            string        `json:"status"`
	VerificationLevel int           `json:"verification_level"`
	CanTransact       bool          `json:"can_transact"`
}

// authResponse: el BFF guarda session.token en su cookie httpOnly; nunca llega al navegador por JSON.
type authResponse struct {
	Session struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"session"`
	User userResponse `json:"user"`
	// VerificationSent solo aparece en el registro: si es false, la app ofrece reenviar el código.
	VerificationSent *bool `json:"verification_sent,omitempty"`
}

func toUserResponse(u domain.User) userResponse {
	return userResponse{
		ID: u.ID, Email: u.Email, EmailVerified: u.EmailVerifiedAt != nil, Name: u.Name, AvatarURL: u.AvatarURL,
		Roles: u.Roles, Status: string(u.Status), VerificationLevel: u.VerificationLevel, CanTransact: u.CanTransact(),
	}
}

func toAuthResponse(r service.AuthResult) authResponse {
	var res authResponse
	res.Session.Token, res.Session.ExpiresAt = r.Token, r.Session.ExpiresAt
	res.User = toUserResponse(r.User)
	return res
}

func sessionMeta(c fiber.Ctx) domain.SessionMeta {
	return domain.SessionMeta{IP: c.IP(), UserAgent: c.Get(fiber.HeaderUserAgent)}
}

// POST /api/v1/auth/register
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req registerRequest
	if err := c.Bind().JSON(&req); err != nil {
		return badRequest("El cuerpo debe ser JSON válido.")
	}
	res, err := h.auth.Register(c.Context(), service.RegisterInput{
		Email: req.Email, Name: req.Name, Password: req.Password,
		AdultDeclared: req.AdultDeclared, AcceptLegal: req.AcceptLegal, Meta: sessionMeta(c),
	})
	if err != nil {
		return err
	}
	body := toAuthResponse(res)
	body.VerificationSent = &res.VerificationSent
	return c.Status(fiber.StatusCreated).JSON(body)
}

// POST /api/v1/auth/login
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req loginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return badRequest("El cuerpo debe ser JSON válido.")
	}
	res, err := h.auth.Login(c.Context(), req.Email, req.Password, sessionMeta(c))
	if err != nil {
		return err
	}
	return c.JSON(toAuthResponse(res))
}

// POST /api/v1/auth/logout (requiere sesión)
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	if err := h.auth.Logout(c.Context(), session); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/v1/auth/logout-all (requiere sesión)
func (h *AuthHandler) LogoutAll(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	if err := h.auth.LogoutAll(c.Context(), session, c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GET /api/v1/me (requiere sesión)
func (h *AuthHandler) Me(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	user, err := h.auth.Me(c.Context(), session)
	if err != nil {
		return err
	}
	return c.JSON(toUserResponse(user))
}
