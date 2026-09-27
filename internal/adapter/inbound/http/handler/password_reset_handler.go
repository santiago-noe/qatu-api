package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"
)

// PasswordResetter es lo que el handler necesita de service.PasswordResetService.
type PasswordResetter interface {
	Request(ctx context.Context, email string) error
	Reset(ctx context.Context, email, code, newPassword, ip string) error
}

type PasswordResetHandler struct {
	resetter PasswordResetter
}

func NewPasswordResetHandler(resetter PasswordResetter) *PasswordResetHandler {
	return &PasswordResetHandler{resetter: resetter}
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

// POST /api/v1/auth/password/forgot
// Siempre 202, exista o no el correo: la respuesta no revela qué cuentas existen.
func (h *PasswordResetHandler) Forgot(c fiber.Ctx) error {
	var req forgotPasswordRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if err := h.resetter.Request(c.Context(), req.Email); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusAccepted)
}

// POST /api/v1/auth/password/reset
// 204: la contraseña cambió y se cerraron todas las sesiones; el usuario vuelve a iniciar sesión.
func (h *PasswordResetHandler) Reset(c fiber.Ctx) error {
	var req resetPasswordRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if err := h.resetter.Reset(c.Context(), req.Email, req.Code, req.Password, c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
