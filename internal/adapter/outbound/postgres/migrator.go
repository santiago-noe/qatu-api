package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Número arbitrario y fijo para el bloqueo: evita que dos procesos migren a la vez.
const migrationLockID = 72_870_001

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Migration es un par de archivos up/down con el mismo número de versión.
type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

// LoadMigrations lee y valida los archivos: pares completos y versiones sin huecos desde 1.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migraciones: %w", err)
	}

	byVersion := map[int]*Migration{}
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			continue // embed.go y otros archivos
		}
		version, _ := strconv.Atoi(m[1])
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		mig := byVersion[version]
		if mig == nil {
			mig = &Migration{Version: version, Name: m[2]}
			byVersion[version] = mig
		}
		if mig.Name != m[2] {
			return nil, fmt.Errorf("migraciones: la versión %04d tiene nombres distintos (%s, %s)", version, mig.Name, m[2])
		}
		if m[3] == "up" {
			mig.Up = string(body)
		} else {
			mig.Down = string(body)
		}
	}

	list := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.Up == "" || m.Down == "" {
			return nil, fmt.Errorf("migraciones: %04d_%s debe tener .up.sql y .down.sql", m.Version, m.Name)
		}
		list = append(list, *m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Version < list[j].Version })
	for i, m := range list {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migraciones: se esperaba la versión %04d y se encontró %04d", i+1, m.Version)
		}
	}
	return list, nil
}

// Migrator aplica y revierte migraciones, cada una en su transacción.
type Migrator struct {
	pool       *pgxpool.Pool
	migrations []Migration
}

func NewMigrator(pool *pgxpool.Pool, migrations []Migration) *Migrator {
	return &Migrator{pool: pool, migrations: migrations}
}

// Up aplica todas las pendientes y devuelve las aplicadas.
func (m *Migrator) Up(ctx context.Context) ([]Migration, error) {
	var applied []Migration
	err := m.withLock(ctx, func(conn *pgxpool.Conn) error {
		current, err := currentVersion(ctx, conn)
		if err != nil {
			return err
		}
		for _, mig := range m.migrations {
			if mig.Version <= current {
				continue
			}
			if err := apply(ctx, conn, mig.Up, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", mig.Version, mig.Name); err != nil {
				return fmt.Errorf("migración %04d_%s: %w", mig.Version, mig.Name, err)
			}
			applied = append(applied, mig)
		}
		return nil
	})
	return applied, err
}

// Down revierte la última migración aplicada. Devuelve nil si no había ninguna.
func (m *Migrator) Down(ctx context.Context) (*Migration, error) {
	var reverted *Migration
	err := m.withLock(ctx, func(conn *pgxpool.Conn) error {
		current, err := currentVersion(ctx, conn)
		if err != nil || current == 0 {
			return err
		}
		mig := m.migrations[current-1]
		if err := apply(ctx, conn, mig.Down, "DELETE FROM schema_migrations WHERE version = $1", mig.Version); err != nil {
			return fmt.Errorf("revertir %04d_%s: %w", mig.Version, mig.Name, err)
		}
		reverted = &mig
		return nil
	})
	return reverted, err
}

// Version devuelve la última versión aplicada (0 si ninguna).
func (m *Migrator) Version(ctx context.Context) (int, error) {
	var version int
	err := m.withLock(ctx, func(conn *pgxpool.Conn) error {
		v, err := currentVersion(ctx, conn)
		version = v
		return err
	})
	return version, err
}

func (m *Migrator) withLock(ctx context.Context, fn func(*pgxpool.Conn) error) error {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migraciones: conexión: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	return fn(conn)
}

func currentVersion(ctx context.Context, conn *pgxpool.Conn) (int, error) {
	var v int
	err := conn.QueryRow(ctx, "SELECT COALESCE(max(version), 0) FROM schema_migrations").Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// apply ejecuta el SQL y registra el cambio en la misma transacción: o se aplica todo o nada.
func apply(ctx context.Context, conn *pgxpool.Conn, sql, record string, args ...any) error {
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, record, args...)
		return err
	})
}
