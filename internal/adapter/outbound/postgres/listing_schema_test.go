package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/migrations"
)

// La base protege las reglas de la 003 aunque falle el código: publicación completa fuera de
// borrador, zona de la misma ciudad, una sola foto de placa y calendario sin cruces.
func TestListingSchemaConstraints(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	var userID, categoryID, cityID, zoneID string
	must(t, db.Pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('ana@qatu.pe', 'Ana') RETURNING id`).Scan(&userID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'rotomartillo'`).Scan(&categoryID))
	must(t, db.Pool.QueryRow(ctx, `SELECT z.id, z.city_id FROM zones z WHERE z.slug = 'ayacucho'`).Scan(&zoneID, &cityID))

	var listingID string
	must(t, db.Pool.QueryRow(ctx, `
		INSERT INTO tool_listings (owner_id, category_id, city_id, title) VALUES ($1, $2, $3, 'Rotomartillo Bosch')
		RETURNING id`, userID, categoryID, cityID).Scan(&listingID))

	checkViolation := func(name, sql string, code string, args ...any) {
		t.Helper()
		_, err := db.Pool.Exec(ctx, sql, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Errorf("%s: quiero error %s, llegó %v", name, code, err)
		}
	}

	checkViolation("publicar un borrador incompleto",
		`UPDATE tool_listings SET status = 'published' WHERE id = $1`, "23514", listingID)
	checkViolation("recojo sin punto",
		`UPDATE tool_listings SET pickup_enabled = true WHERE id = $1`, "23514", listingID)
	checkViolation("rechazada sin motivo",
		`UPDATE tool_listings SET status = 'rejected' WHERE id = $1`, "23514", listingID)
	checkViolation("zona de otra ciudad",
		`UPDATE tool_listings SET zone_id = $2, city_id = gen_random_uuid() WHERE id = $1`, "23503", listingID, zoneID)

	// Completa: sí se publica.
	_, err := db.Pool.Exec(ctx, `
		UPDATE tool_listings SET zone_id = $2, price_day = 3500, replacement_value = 45000, deposit = 14000,
		       delivery_enabled = true, delivery_fee = 1000, status = 'published'
		WHERE id = $1`, listingID, zoneID)
	must(t, err)

	must(t, execErr(db, ctx, `INSERT INTO listing_photos (listing_id, kind, object_key) VALUES ($1, 'serial', 'k1')`, listingID))
	checkViolation("dos fotos de placa",
		`INSERT INTO listing_photos (listing_id, kind, object_key) VALUES ($1, 'serial', 'k2')`, "23505", listingID)

	must(t, execErr(db, ctx, `INSERT INTO availability_blocks (listing_id, period, reason)
		VALUES ($1, tstzrange('2026-10-01', '2026-10-05'), 'manual')`, listingID))
	checkViolation("bloqueos que se cruzan", `INSERT INTO availability_blocks (listing_id, period, reason)
		VALUES ($1, tstzrange('2026-10-04', '2026-10-06'), 'booking')`, "23P01", listingID)
	// Contiguo (el fin no incluye el último instante): sí se permite.
	must(t, execErr(db, ctx, `INSERT INTO availability_blocks (listing_id, period, reason)
		VALUES ($1, tstzrange('2026-10-05', '2026-10-06'), 'booking')`, listingID))

	var radius string
	must(t, db.Pool.QueryRow(ctx, `SELECT value::text FROM platform_settings WHERE key = 'listings.public_radius_m'`).Scan(&radius))
	if radius != "500" {
		t.Fatalf("radio público por defecto = %s", radius)
	}
}

// La 0006 se puede revertir sin dejar restos (tablas, ajustes ni el propósito de consentimiento).
func TestListingMigrationIsReversible(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	list, err := LoadMigrations(migrations.FS)
	must(t, err)
	m := NewMigrator(db.Pool, list)
	downTo(t, m, 6)
	var tables, settings int
	must(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name LIKE '%listing%' OR table_name = 'availability_blocks' OR table_name = 'lender_profiles'`).Scan(&tables))
	must(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM platform_settings WHERE key LIKE 'listings.%'`).Scan(&settings))
	if tables != 0 || settings != 0 {
		t.Fatalf("quedaron %d tablas y %d ajustes de la 0006", tables, settings)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("volver a subir: %v", err)
	}
}

// downTo revierte migraciones hasta revertir la versión indicada (las posteriores primero).
func downTo(t *testing.T, m *Migrator, version int) {
	t.Helper()
	for {
		down, err := m.Down(context.Background())
		if err != nil {
			t.Fatalf("Down: %v", err)
		}
		if down == nil {
			t.Fatalf("no quedan migraciones para revertir hasta la %d", version)
		}
		if down.Version <= version {
			return
		}
	}
}

func execErr(db *Client, ctx context.Context, sql string, args ...any) error {
	_, err := db.Pool.Exec(ctx, sql, args...)
	return err
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Spec 002: una categoría prohibida no se publica. Al prohibirla, sus publicaciones activas salen
// del catálogo con un motivo; los borradores se quedan pero no pueden enviarse.
func TestProhibitedCategoryListings(t *testing.T) {
	db := newTestDB(t)
	repo := NewCatalogRepository(db)
	ctx := context.Background()

	var userID, cityID, zoneID string
	must(t, db.Pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('ana@qatu.pe', 'Ana') RETURNING id`).Scan(&userID))
	must(t, db.Pool.QueryRow(ctx, `SELECT z.id, z.city_id FROM zones z WHERE z.slug = 'ayacucho'`).Scan(&zoneID, &cityID))
	var leaf domain.Category
	var err error
	{
		var id string
		must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'rotomartillo'`).Scan(&id))
		leaf, err = repo.FindCategory(ctx, id)
		must(t, err)
	}
	root, err := repo.FindCategory(ctx, leaf.ParentID)
	must(t, err)

	insert := func(categoryID, status string) (string, error) {
		var id string
		err := db.Pool.QueryRow(ctx, `
			INSERT INTO tool_listings (owner_id, category_id, city_id, zone_id, title, price_day, replacement_value,
			                           deposit, delivery_enabled, delivery_fee, status)
			VALUES ($1, $2, $3, $4, 'Rotomartillo Bosch', 3500, 45000, 14000, true, 1000, $5) RETURNING id`,
			userID, categoryID, cityID, zoneID, status).Scan(&id)
		return id, err
	}
	published, err := insert(leaf.ID, "published")
	must(t, err)
	draft, err := insert(leaf.ID, "draft")
	must(t, err)

	checkRejected := func(name string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: quiero 23514, llegó %v", name, err)
		}
	}
	_, err = insert(root.ID, "draft")
	checkRejected("una raíz no es un tipo de herramienta", err)
	var trade string
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'gasfiteria'`).Scan(&trade))
	_, err = insert(trade, "draft")
	checkRejected("un oficio no es una herramienta", err)

	// El admin prohíbe la raíz (Construcción): alcanza a sus tipos.
	root.Prohibited = true
	must(t, repo.UpdateCategory(ctx, root, domain.AuditEntry{ActorID: userID, Action: domain.AuditCategoryUpdated, Entity: "category"}))

	var status, reason string
	must(t, db.Pool.QueryRow(ctx, `SELECT status, rejection_reason FROM tool_listings WHERE id = $1`, published).Scan(&status, &reason))
	if status != "rejected" || reason != domain.ProhibitedCategoryReason {
		t.Fatalf("la publicada sale del catálogo con motivo: %s %q", status, reason)
	}
	must(t, db.Pool.QueryRow(ctx, `SELECT status FROM tool_listings WHERE id = $1`, draft).Scan(&status))
	if status != "draft" {
		t.Fatalf("el borrador se queda: %s", status)
	}
	var retired string
	must(t, db.Pool.QueryRow(ctx, `SELECT after->>'listings' FROM audit_log WHERE action = $1 AND entity_id = $2`,
		domain.AuditListingsRetired, root.ID).Scan(&retired))
	if retired != "1" {
		t.Fatalf("la auditoría cuenta las publicaciones retiradas: %s", retired)
	}

	_, err = db.Pool.Exec(ctx, `UPDATE tool_listings SET status = 'in_review' WHERE id = $1`, draft)
	checkRejected("enviar un borrador de una categoría prohibida", err)
	_, err = db.Pool.Exec(ctx, `UPDATE tool_listings SET status = 'published', rejection_reason = NULL WHERE id = $1`, published)
	checkRejected("volver a publicar una retirada", err)
}
