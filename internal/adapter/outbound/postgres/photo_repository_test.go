package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestPhotoRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewPhotoRepository(db)
	ctx := context.Background()

	var ownerID, categoryID, cityID, listingID string
	must(t, db.Pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('ana@qatu.pe', 'Ana') RETURNING id`).Scan(&ownerID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'rotomartillo'`).Scan(&categoryID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM cities WHERE slug = 'ayacucho'`).Scan(&cityID))
	must(t, db.Pool.QueryRow(ctx, `
		INSERT INTO tool_listings (owner_id, category_id, city_id, title) VALUES ($1, $2, $3, 'Rotomartillo Bosch')
		RETURNING id`, ownerID, categoryID, cityID).Scan(&listingID))

	ids := make([]string, 3)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i+1)
		must(t, repo.CreatePhoto(ctx, domain.ListingPhoto{ID: ids[i], ListingID: listingID, Kind: domain.PhotoPublic,
			ObjectKey: domain.OriginalPhotoKey(listingID, ids[i])}))
	}
	serial := "00000000-0000-0000-0000-000000000009"
	must(t, repo.CreatePhoto(ctx, domain.ListingPhoto{ID: serial, ListingID: listingID, Kind: domain.PhotoSerial,
		ObjectKey: domain.OriginalPhotoKey(listingID, serial)}))

	photos, err := repo.ListPhotos(ctx, listingID)
	if err != nil || len(photos) != 4 || photos[0].ID != ids[0] || photos[2].SortOrder != 2 || photos[3].Kind != domain.PhotoSerial {
		t.Fatalf("públicas en orden de llegada y la placa al final: %+v, %v", photos, err)
	}
	if photos[0].Status != domain.PhotoPending {
		t.Fatal("nace pendiente")
	}

	must(t, repo.MarkPhotoReady(ctx, ids[0], 1600, 1200))
	must(t, repo.MarkPhotoFailed(ctx, ids[1]))
	if p, _ := repo.FindPhoto(ctx, ids[0]); p.Status != domain.PhotoReady || p.Width != 1600 {
		t.Fatalf("lista: %+v", p)
	}
	if n, _ := NewListingRepository(db).ReadyPhotos(ctx, listingID); n != 1 {
		t.Fatalf("una lista para publicar: %d", n)
	}

	must(t, repo.ReorderPhotos(ctx, listingID, []string{ids[2], ids[0], ids[1]}))
	photos, _ = repo.ListPhotos(ctx, listingID)
	if photos[0].ID != ids[2] || photos[1].ID != ids[0] {
		t.Fatalf("nuevo orden: %s, %s", photos[0].ID, photos[1].ID)
	}

	stale, err := repo.ListStalePending(ctx, time.Now().Add(time.Minute), 10)
	if err != nil || len(stale) != 2 {
		t.Fatalf("pendientes más viejas que el corte (la lista y la fallida no): %d, %v", len(stale), err)
	}
	if stale, _ := repo.ListStalePending(ctx, time.Now().Add(-time.Hour), 10); len(stale) != 0 {
		t.Fatal("las recientes no se tocan")
	}

	must(t, repo.DeletePhoto(ctx, ids[1]))
	if _, err := repo.FindPhoto(ctx, ids[1]); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("borrada: %v", err)
	}
	if err := repo.MarkPhotoReady(ctx, ids[1], 1, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("marcar una borrada: %v", err)
	}
	if _, err := repo.FindPhoto(ctx, "no-es-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ID inválido: %v", err)
	}
}
