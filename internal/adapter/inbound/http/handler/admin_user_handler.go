package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// AdminUsers es lo que el handler necesita de service.AdminUserService.
type AdminUsers interface {
	Get(ctx context.Context, userID string) (domain.User, error)
	ChangeRoles(ctx context.Context, actorID, userID string, add, remove []domain.Role, ip string) (domain.User, error)
	ChangeStatus(ctx context.Context, actorID, userID string, status domain.UserStatus, reason, ip string) (domain.User, error)
}

// AdminUserHandler atiende /api/v1/admin/users. Las rutas exigen sesión, rol admin y segundo paso.
type AdminUserHandler struct {
	admin AdminUsers
}

func NewAdminUserHandler(admin AdminUsers) *AdminUserHandler { return &AdminUserHandler{admin: admin} }

type changeRolesRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type changeStatusRequest struct {
	Status string `json:"status"` // active | suspended
	Reason string `json:"reason"`
}

// adminUserResponse incluye el motivo de suspensión, que el usuario común no ve.
type adminUserResponse struct {
	userResponse
	SuspendedReason string `json:"suspended_reason,omitempty"`
}

func toAdminUserResponse(u domain.User) adminUserResponse {
	return adminUserResponse{userResponse: toUserResponse(u), SuspendedReason: u.SuspendedReason}
}

// GET /api/v1/admin/users/:id
func (h *AdminUserHandler) Get(c fiber.Ctx) error {
	user, err := h.admin.Get(c.Context(), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(toAdminUserResponse(user))
}

// PATCH /api/v1/admin/users/:id/roles
func (h *AdminUserHandler) ChangeRoles(c fiber.Ctx) error {
	var req changeRolesRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	add, err := parseRoles(req.Add)
	if err != nil {
		return err
	}
	remove, err := parseRoles(req.Remove)
	if err != nil {
		return err
	}
	user, err := h.admin.ChangeRoles(c.Context(), actorID(c), c.Params("id"), add, remove, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toAdminUserResponse(user))
}

// PATCH /api/v1/admin/users/:id/status
func (h *AdminUserHandler) ChangeStatus(c fiber.Ctx) error {
	var req changeStatusRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	user, err := h.admin.ChangeStatus(c.Context(), actorID(c), c.Params("id"), domain.UserStatus(req.Status), req.Reason, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toAdminUserResponse(user))
}

func parseRoles(raw []string) ([]domain.Role, error) {
	roles := make([]domain.Role, 0, len(raw))
	for _, r := range raw {
		role, ok := domain.ParseRole(r)
		if !ok {
			return nil, badRequest("Rol desconocido: " + r)
		}
		roles = append(roles, role)
	}
	return roles, nil
}
