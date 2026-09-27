// Command migrations aplica o revierte las migraciones SQL de migrations/.
//
//	migrations up       aplica todas las pendientes
//	migrations down     revierte la última aplicada
//	migrations version  muestra la versión actual
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/postgres"
	"github.com/santiago-noe/qatu-api/internal/config"
	"github.com/santiago-noe/qatu-api/migrations"
)

var errUsage = errors.New("uso: migrations up | down | version")

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return errUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	list, err := postgres.LoadMigrations(migrations.FS)
	if err != nil {
		return err
	}
	db, err := postgres.New(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer db.Close()
	migrator := postgres.NewMigrator(db.Pool, list)

	switch args[0] {
	case "up":
		applied, err := migrator.Up(ctx)
		for _, m := range applied {
			fmt.Printf("aplicada %04d_%s\n", m.Version, m.Name)
		}
		if err == nil && len(applied) == 0 {
			fmt.Println("sin migraciones pendientes")
		}
		return err
	case "down":
		reverted, err := migrator.Down(ctx)
		if err == nil && reverted == nil {
			fmt.Println("no hay migraciones aplicadas")
		} else if reverted != nil {
			fmt.Printf("revertida %04d_%s\n", reverted.Version, reverted.Name)
		}
		return err
	case "version":
		v, err := migrator.Version(ctx)
		if err == nil {
			fmt.Printf("versión %04d de %04d\n", v, len(list))
		}
		return err
	default:
		return errUsage
	}
}
