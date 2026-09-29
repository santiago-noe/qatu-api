package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type adminFixture struct {
	auth    authFixture
	svc     *AdminUserService
	ana     AuthResult // usuaria común con sesión abierta
	adminID string
}

func newAdminFixture(t *testing.T) adminFixture {
	t.Helper()
	f := newAuthFixture(t)
	ana, err := f.svc.Register(context.Background(), validRegister())
	if err != nil {
		t.Fatal(err)
	}
	admin := domain.User{ID: "u-admin", Email: "admin@qatu.pe", Status: domain.UserActive, Roles: []domain.Role{domain.RoleAdmin, domain.RoleClient}}
	f.accounts.users[admin.ID] = admin
	return adminFixture{auth: f, svc: NewAdminUserService(f.accounts, f.sessions, f.svc.Clock), ana: ana, adminID: admin.ID}
}

func TestChangeRoles(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()

	user, err := f.svc.ChangeRoles(ctx, f.adminID, f.ana.User.ID, []domain.Role{domain.RoleModerator}, nil, "1.2.3.4")
	if err != nil || !user.HasRole(domain.RoleModerator) || !user.HasRole(domain.RoleClient) {
		t.Fatalf("ChangeRoles = %+v, %v", user, err)
	}
	if _, err := f.auth.sessions.Authenticate(ctx, f.ana.Token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatal("cambiar roles cierra las sesiones: los roles viejos no deben seguir vigentes")
	}
	last := f.auth.accounts.audits[len(f.auth.accounts.audits)-1]
	if last.Action != domain.AuditRolesChanged || last.ActorID != f.adminID {
		t.Fatalf("se audita con el autor: %+v", last)
	}

	audits := len(f.auth.accounts.audits)
	if _, err := f.svc.ChangeRoles(ctx, f.adminID, f.ana.User.ID, []domain.Role{domain.RoleModerator}, nil, ""); err != nil || len(f.auth.accounts.audits) != audits {
		t.Fatal("repetir un rol que ya tiene no cambia ni audita nada")
	}
}

func TestChangeRolesRules(t *testing.T) {
	tests := []struct {
		name        string
		actor       string
		target      func(adminFixture) string
		add, remove []domain.Role
		want        error
	}{
		{"no asigna roles de oferta", "u-admin", func(f adminFixture) string { return f.ana.User.ID }, []domain.Role{domain.RoleLender}, nil, domain.ErrRoleNotAssignable},
		{"no quita el rol cliente", "u-admin", func(f adminFixture) string { return f.ana.User.ID }, nil, []domain.Role{domain.RoleClient}, domain.ErrRoleNotAssignable},
		{"no se quita su propio admin", "u-admin", func(adminFixture) string { return "u-admin" }, nil, []domain.Role{domain.RoleAdmin}, domain.ErrSelfLockout},
		{"usuario inexistente", "u-admin", func(adminFixture) string { return "u-nadie" }, []domain.Role{domain.RoleSupport}, nil, domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAdminFixture(t)
			if _, err := f.svc.ChangeRoles(context.Background(), tt.actor, tt.target(f), tt.add, tt.remove, ""); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, se esperaba %v", err, tt.want)
			}
		})
	}
}

func TestSuspendAndReactivate(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()

	if _, err := f.svc.ChangeStatus(ctx, f.adminID, f.ana.User.ID, domain.UserSuspended, "  ", ""); !errors.Is(err, domain.ErrSuspensionReason) {
		t.Fatalf("suspender exige un motivo, llegó %v", err)
	}
	user, err := f.svc.ChangeStatus(ctx, f.adminID, f.ana.User.ID, domain.UserSuspended, "Reporte de estafa", "")
	if err != nil || user.Status != domain.UserSuspended || user.CanTransact() {
		t.Fatalf("suspendida: %+v %v", user, err)
	}
	if _, err := f.auth.sessions.Authenticate(ctx, f.ana.Token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatal("suspender cierra las sesiones")
	}
	if _, err := f.auth.svc.Login(ctx, "ana@correo.pe", "tornillo-verde-9", domain.SessionMeta{}); err != nil {
		t.Fatalf("una cuenta suspendida puede entrar a ver su historial: %v", err)
	}

	user, err = f.svc.ChangeStatus(ctx, f.adminID, f.ana.User.ID, domain.UserActive, "ignorado", "")
	if err != nil || user.Status != domain.UserActive || user.SuspendedReason != "" {
		t.Fatalf("reactivar borra el motivo: %+v %v", user, err)
	}
}

func TestChangeStatusRules(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	if _, err := f.svc.ChangeStatus(ctx, f.adminID, f.adminID, domain.UserSuspended, "prueba", ""); !errors.Is(err, domain.ErrSelfLockout) {
		t.Fatalf("un admin no se suspende a sí mismo, llegó %v", err)
	}
	if _, err := f.svc.ChangeStatus(ctx, f.adminID, f.ana.User.ID, domain.UserDeleted, "", ""); !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("eliminar no es un cambio de estado de admin (va por ARCO), llegó %v", err)
	}
}

func TestGetByEmail(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()

	// El correo se normaliza igual que al registrarse: mayúsculas y espacios no impiden encontrarla.
	user, err := f.svc.GetByEmail(ctx, "  "+strings.ToUpper(f.ana.User.Email)+" ")
	if err != nil || user.ID != f.ana.User.ID {
		t.Fatalf("GetByEmail = %+v, %v", user, err)
	}
	if _, err := f.svc.GetByEmail(ctx, "nadie@qatu.pe"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("un correo sin cuenta = %v, quiero ErrNotFound", err)
	}
	if _, err := f.svc.GetByEmail(ctx, "no-es-correo"); !errors.Is(err, domain.ErrInvalidEmail) {
		t.Fatalf("un correo mal escrito = %v, quiero ErrInvalidEmail", err)
	}
}
