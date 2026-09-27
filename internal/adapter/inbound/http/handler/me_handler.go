package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/middleware"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// AccountUseCases es lo que el handler necesita de service.AccountService.
type AccountUseCases interface {
	Me(ctx context.Context, session domain.Session) (domain.User, error)
	UpdateProfile(ctx context.Context, session domain.Session, in service.ProfileUpdate, ip string) (domain.User, error)
	ChangePassword(ctx context.Context, session domain.Session, current, next, ip string) error
	ListSessions(ctx context.Context, session domain.Session) ([]service.SessionView, error)
	RevokeSession(ctx context.Context, session domain.Session, id, ip string) error
}

// MeHandler atiende /api/v1/me: la cuenta del usuario conectado. Todas las rutas requieren sesión.
type MeHandler struct {
	account AccountUseCases
}

func NewMeHandler(account AccountUseCases) *MeHandler { return &MeHandler{account: account} }

type updateProfileRequest struct {
	Name *string `json:"name"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"` // vacío si la cuenta aún no tiene contraseña
	NewPassword     string `json:"new_password"`
}

type sessionResponse struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used_at"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	Current   bool      `json:"current"`
}

// GET /api/v1/me
func (h *MeHandler) Get(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	user, err := h.account.Me(c.Context(), session)
	if err != nil {
		return err
	}
	return c.JSON(toUserResponse(user))
}

// PATCH /api/v1/me
func (h *MeHandler) Update(c fiber.Ctx) error {
	var req updateProfileRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	session, _ := middleware.SessionFrom(c)
	user, err := h.account.UpdateProfile(c.Context(), session, service.ProfileUpdate{Name: req.Name}, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toUserResponse(user))
}

// POST /api/v1/me/password — 204; se cierran las demás sesiones.
func (h *MeHandler) ChangePassword(c fiber.Ctx) error {
	var req changePasswordRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	session, _ := middleware.SessionFrom(c)
	if err := h.account.ChangePassword(c.Context(), session, req.CurrentPassword, req.NewPassword, c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GET /api/v1/me/sessions
func (h *MeHandler) Sessions(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	views, err := h.account.ListSessions(c.Context(), session)
	if err != nil {
		return err
	}
	res := make([]sessionResponse, len(views))
	for i, v := range views {
		res[i] = sessionResponse{ID: v.ID, Provider: string(v.Provider), CreatedAt: v.CreatedAt, LastUsed: v.RenewedAt,
			IP: v.IP, UserAgent: v.UserAgent, Current: v.Current}
	}
	return c.JSON(fiber.Map{"sessions": res})
}

// DELETE /api/v1/me/sessions/:id — 204.
func (h *MeHandler) RevokeSession(c fiber.Ctx) error {
	session, _ := middleware.SessionFrom(c)
	if err := h.account.RevokeSession(c.Context(), session, c.Params("id"), c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
