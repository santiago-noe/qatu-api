package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/middleware"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// TwoFactor es lo que el handler necesita de service.TwoFactorService.
type TwoFactor interface {
	Send(ctx context.Context, session domain.Session) error
	Verify(ctx context.Context, session domain.Session, code, ip string) error
}

// TwoFactorHandler atiende /api/v1/auth/two-factor. Las rutas requieren sesión.
type TwoFactorHandler struct {
	twoFactor TwoFactor
}

func NewTwoFactorHandler(twoFactor TwoFactor) *TwoFactorHandler {
	return &TwoFactorHandler{twoFactor: twoFactor}
}

// POST /api/v1/auth/two-factor/send — 202: el correo se envía, no se confirma la entrega.
func (h *TwoFactorHandler) Send(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	if err := h.twoFactor.Send(c.Context(), session); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusAccepted)
}

// POST /api/v1/auth/two-factor/verify — 204; desde aquí la sesión entra a las rutas internas.
func (h *TwoFactorHandler) Verify(c fiber.Ctx) error {
	var req codeRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	session, _ := middleware.SessionFrom(c)
	if err := h.twoFactor.Verify(c.Context(), session, req.Code, c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
