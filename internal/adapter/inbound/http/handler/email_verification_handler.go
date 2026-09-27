package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/middleware"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// EmailVerifier es lo que el handler necesita de service.EmailVerificationService.
type EmailVerifier interface {
	Resend(ctx context.Context, session domain.Session) error
	Verify(ctx context.Context, session domain.Session, code, ip string) (domain.User, error)
}

type EmailVerificationHandler struct {
	verifier EmailVerifier
}

func NewEmailVerificationHandler(verifier EmailVerifier) *EmailVerificationHandler {
	return &EmailVerificationHandler{verifier: verifier}
}

type verifyEmailRequest struct {
	Code string `json:"code"`
}

// POST /api/v1/auth/email/verify (requiere sesión)
func (h *EmailVerificationHandler) Verify(c fiber.Ctx) error {
	var req verifyEmailRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	session, _ := middleware.SessionFrom(c)
	user, err := h.verifier.Verify(c.Context(), session, req.Code, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toUserResponse(user))
}

// POST /api/v1/auth/email/resend (requiere sesión). 202: el correo se envía, no se confirma la entrega.
func (h *EmailVerificationHandler) Resend(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	if err := h.verifier.Resend(c.Context(), session); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusAccepted)
}
