package service

import (
	"context"
	"slices"
	"strings"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// AdminUserService: roles internos y suspensión de cuentas. Solo lo usan administradores.
type AdminUserService struct {
	accounts port.AccountRepository
	sessions *SessionService
	clock    port.Clock
}

func NewAdminUserService(accounts port.AccountRepository, sessions *SessionService, clock port.Clock) *AdminUserService {
	return &AdminUserService{accounts: accounts, sessions: sessions, clock: clock}
}

func (s *AdminUserService) Get(ctx context.Context, userID string) (domain.User, error) {
	return s.accounts.FindUser(ctx, userID)
}

func (s *AdminUserService) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	normalized, err := domain.NormalizeEmail(email)
	if err != nil {
		return domain.User{}, err
	}
	return s.accounts.FindUserByEmail(ctx, normalized)
}

// ChangeRoles agrega y quita roles internos. actorID vacío = el sistema (comando de arranque).
// Las sesiones del usuario se cierran: cada sesión guarda una copia de los roles.
func (s *AdminUserService) ChangeRoles(ctx context.Context, actorID, userID string, add, remove []domain.Role, ip string) (domain.User, error) {
	for _, r := range slices.Concat(add, remove) {
		if !r.IsInternal() {
			return domain.User{}, domain.ErrRoleNotAssignable
		}
	}
	if actorID == userID && slices.Contains(remove, domain.RoleAdmin) {
		return domain.User{}, domain.ErrSelfLockout
	}
	user, err := s.accounts.FindUser(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}

	// Solo lo que cambia de verdad: agregar lo que falta y quitar lo que tiene.
	add = slices.DeleteFunc(slices.Clone(add), user.HasRole)
	remove = slices.DeleteFunc(slices.Clone(remove), func(r domain.Role) bool { return !user.HasRole(r) })
	if len(add) == 0 && len(remove) == 0 {
		return user, nil
	}

	after := slices.DeleteFunc(slices.Concat(user.Roles, add), func(r domain.Role) bool { return slices.Contains(remove, r) })
	slices.Sort(after)
	change := port.RoleChange{
		UserID: userID, Add: add, Remove: remove, GrantedBy: actorID, At: s.clock.Now(),
		Audit: domain.AuditEntry{ActorID: actorID, Action: domain.AuditRolesChanged, Entity: "user", EntityID: userID,
			Before: map[string]any{"roles": user.Roles}, After: map[string]any{"roles": after}, IP: ip},
	}
	if err := s.accounts.ChangeRoles(ctx, change); err != nil {
		return domain.User{}, err
	}
	if err := s.sessions.RevokeAll(ctx, userID, ""); err != nil {
		return domain.User{}, err
	}
	user.Roles = after
	return user, nil
}

// ChangeStatus suspende (con motivo) o reactiva una cuenta. Al suspender se cierran sus sesiones:
// podrá volver a entrar para ver su historial, pero no transaccionar (docs/05).
func (s *AdminUserService) ChangeStatus(ctx context.Context, actorID, userID string, status domain.UserStatus, reason, ip string) (domain.User, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case status != domain.UserActive && status != domain.UserSuspended:
		return domain.User{}, domain.ErrInvalidStatus
	case status == domain.UserSuspended && reason == "":
		return domain.User{}, domain.ErrSuspensionReason
	case status == domain.UserSuspended && actorID == userID:
		return domain.User{}, domain.ErrSelfLockout
	}
	user, err := s.accounts.FindUser(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if user.Status == domain.UserDeleted {
		return domain.User{}, domain.ErrNotFound
	}
	if status == domain.UserActive {
		reason = ""
	}
	if user.Status == status && user.SuspendedReason == reason {
		return user, nil
	}

	change := port.StatusChange{
		UserID: userID, Status: status, Reason: reason,
		Audit: domain.AuditEntry{ActorID: actorID, Action: domain.AuditStatusChanged, Entity: "user", EntityID: userID,
			Before: map[string]any{"status": user.Status}, After: map[string]any{"status": status, "reason": reason}, IP: ip},
	}
	if err := s.accounts.ChangeStatus(ctx, change); err != nil {
		return domain.User{}, err
	}
	if status == domain.UserSuspended {
		if err := s.sessions.RevokeAll(ctx, userID, ""); err != nil {
			return domain.User{}, err
		}
	}
	user.Status, user.SuspendedReason = status, reason
	return user, nil
}
