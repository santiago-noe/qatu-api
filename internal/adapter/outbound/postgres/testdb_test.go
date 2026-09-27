package postgres

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/santiago-noe/qatu-api/migrations"
)

// newTestDB crea una base de datos temporal con todas las migraciones y la borra al terminar.
// Se ejecuta si existe QATU_TEST_DATABASE_URL (una URL con permiso para crear bases).
func newTestDB(t *testing.T) *Client {
	t.Helper()
	adminURL := os.Getenv("QATU_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("QATU_TEST_DATABASE_URL no definido: se omite la integración con Postgres")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("Postgres no disponible: %v", err)
	}
	name := fmt.Sprintf("qatu_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(adminURL)
	u.Path = "/" + name
	db, err := New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close(ctx)
	})

	list, err := LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewMigrator(db.Pool, list).Up(ctx); err != nil {
		t.Fatalf("migraciones: %v", err)
	}
	return db
}
