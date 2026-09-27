// Command admin administra roles internos desde el servidor, sin pasar por la API.
// Sirve para crear el primer admin (nadie puede asignarlo por la API si aún no existe).
//
//	admin grant  <correo> <rol>   agrega un rol interno (support, moderator, admin)
//	admin revoke <correo> <rol>   quita un rol interno
//
// La acción queda en audit_log sin autor (el sistema) y cierra las sesiones del usuario.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/postgres"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/redisclient"
	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/system"
	"github.com/santiago-noe/qatu-api/internal/config"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

var errUsage = errors.New("uso: admin grant|revoke <correo> <rol>")

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 || (args[0] != "grant" && args[0] != "revoke") {
		return errUsage
	}
	role, ok := domain.ParseRole(args[2])
	if !ok {
		return fmt.Errorf("rol desconocido: %s", args[2])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := postgres.New(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer db.Close()
	cache := redisclient.New(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	defer cache.Close()

	sessions := service.NewSessionService(redisclient.NewSessionStore(cache.RDB, "qatu:"), system.Clock{},
		service.SessionConfig{TTL: cfg.Session.TTL, RenewAfter: cfg.Session.RenewAfter})
	admin := service.NewAdminUserService(postgres.NewAccountRepository(db), sessions, system.Clock{})

	user, err := admin.GetByEmail(ctx, args[1])
	if err != nil {
		return fmt.Errorf("usuario %s: %w", args[1], err)
	}
	var add, remove []domain.Role
	if args[0] == "grant" {
		add = []domain.Role{role}
	} else {
		remove = []domain.Role{role}
	}
	updated, err := admin.ChangeRoles(ctx, "", user.ID, add, remove, "")
	if err != nil {
		return err
	}
	fmt.Printf("%s ahora tiene los roles %v\n", updated.Email, updated.Roles)
	return nil
}
