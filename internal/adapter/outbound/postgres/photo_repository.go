package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// PhotoRepository implementa port.PhotoRepository.
type PhotoRepository struct {
	db *Client
}

func NewPhotoRepository(db *Client) *PhotoRepository { return &PhotoRepository{db: db} }

const photoSelect = `
	SELECT id, listing_id, kind, object_key, status, COALESCE(width, 0), COALESCE(height, 0), sort_order, created_at
	FROM listing_photos`

func scanPhoto(row pgx.Row) (domain.ListingPhoto, error) {
	var p domain.ListingPhoto
	err := row.Scan(&p.ID, &p.ListingID, &p.Kind, &p.ObjectKey, &p.Status, &p.Width, &p.Height, &p.SortOrder, &p.CreatedAt)
	return p, err
}

func (r *PhotoRepository) ListPhotos(ctx context.Context, listingID string) ([]domain.ListingPhoto, error) {
	rows, err := r.db.Pool.Query(ctx, photoSelect+`
		WHERE listing_id = $1 ORDER BY kind = 'serial', sort_order, created_at`, listingID)
	if err != nil {
		return nil, mapListingError(err)
	}
	photos, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ListingPhoto, error) { return scanPhoto(row) })
	return photos, mapListingError(err)
}

func (r *PhotoRepository) FindPhoto(ctx context.Context, photoID string) (domain.ListingPhoto, error) {
	p, err := scanPhoto(r.db.Pool.QueryRow(ctx, photoSelect+` WHERE id = $1`, photoID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ListingPhoto{}, domain.ErrNotFound
	}
	return p, mapListingError(err)
}

func (r *PhotoRepository) CreatePhoto(ctx context.Context, p domain.ListingPhoto) error {
	_, err := r.db.Pool.Exec(ctx, `
		INSERT INTO listing_photos (id, listing_id, kind, object_key, status, sort_order)
		VALUES ($1, $2, $3, $4, 'pending',
		        (SELECT COALESCE(max(sort_order) + 1, 0) FROM listing_photos WHERE listing_id = $2 AND kind = $3))`,
		p.ID, p.ListingID, p.Kind, p.ObjectKey)
	return mapListingError(err)
}

func (r *PhotoRepository) MarkPhotoReady(ctx context.Context, photoID string, width, height int) error {
	return r.exec(ctx, `UPDATE listing_photos SET status = 'ready', width = $2, height = $3 WHERE id = $1`, photoID, width, height)
}

func (r *PhotoRepository) MarkPhotoFailed(ctx context.Context, photoID string) error {
	return r.exec(ctx, `UPDATE listing_photos SET status = 'failed' WHERE id = $1`, photoID)
}

func (r *PhotoRepository) DeletePhoto(ctx context.Context, photoID string) error {
	return r.exec(ctx, `DELETE FROM listing_photos WHERE id = $1`, photoID)
}

// ReorderPhotos numera las fotos según su posición en ids, en una sola sentencia.
func (r *PhotoRepository) ReorderPhotos(ctx context.Context, listingID string, ids []string) error {
	_, err := r.db.Pool.Exec(ctx, `
		UPDATE listing_photos p SET sort_order = o.pos - 1
		FROM unnest($2::uuid[]) WITH ORDINALITY AS o(id, pos)
		WHERE p.id = o.id AND p.listing_id = $1`, listingID, ids)
	return mapListingError(err)
}

func (r *PhotoRepository) exec(ctx context.Context, sql string, args ...any) error {
	tag, err := r.db.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return mapListingError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
