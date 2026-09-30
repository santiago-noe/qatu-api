package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// ListingRepository implementa port.ListingRepository.
type ListingRepository struct {
	db *Client
}

func NewListingRepository(db *Client) *ListingRepository { return &ListingRepository{db: db} }

const exclusionViolation = "23P01"

// mapListingError traduce los rechazos de la base: el trigger de categoría (0007), la zona que no
// es de la ciudad y los bloqueos que se cruzan.
func mapListingError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == checkViolation && strings.Contains(pgErr.Message, "prohibida"):
		return domain.ErrListingProhibited
	case pgErr.Code == checkViolation && strings.HasPrefix(pgErr.Message, "tool_listings:"):
		return domain.ErrListingCategory
	case pgErr.Code == exclusionViolation:
		return domain.ErrAvailabilityOverlap
	case pgErr.Code == foreignKeyViolation && strings.Contains(pgErr.ConstraintName, "zone"):
		return domain.ErrListingPickupLocation
	case pgErr.Code == invalidTextInput:
		return domain.ErrNotFound
	}
	return err
}

// listingSelect es la única lectura de publicaciones. Los montos NULL (sin ofrecer) llegan como 0.
const listingSelect = `
	SELECT l.id, l.owner_id, l.category_id, l.city_id, COALESCE(l.zone_id::text, ''), l.title,
	       COALESCE(l.description, ''), l.attributes, COALESCE(l.replacement_value, 0), COALESCE(l.deposit, 0),
	       COALESCE(l.price_hour, 0), COALESCE(l.price_day, 0), COALESCE(l.price_weekend, 0),
	       COALESCE(l.price_week, 0), COALESCE(l.price_month, 0), l.accessories, COALESCE(l.usage_instructions, ''),
	       l.pickup_enabled, ST_Y(l.pickup_location), ST_X(l.pickup_location), ST_Y(l.public_location),
	       ST_X(l.public_location), COALESCE(l.public_radius_m, 0), l.delivery_enabled, COALESCE(l.delivery_fee, 0),
	       COALESCE((SELECT array_agg(d.zone_id::text ORDER BY d.zone_id) FROM listing_delivery_zones d
	                 WHERE d.listing_id = l.id), '{}'),
	       l.booking_mode, l.cancel_policy, l.min_verification, l.min_notice_hours, l.min_duration_hours,
	       l.max_duration_hours, l.status, COALESCE(l.rejection_reason, ''), l.first_published_at, l.version,
	       l.created_at, l.updated_at
	FROM tool_listings l`

func scanListing(row pgx.Row) (domain.ToolListing, error) {
	var l domain.ToolListing
	var pickLat, pickLng, pubLat, pubLng *float64
	err := row.Scan(&l.ID, &l.OwnerID, &l.CategoryID, &l.CityID, &l.ZoneID, &l.Title, &l.Description, &l.Attributes,
		&l.ReplacementValue, &l.Deposit, &l.Prices.Hour, &l.Prices.Day, &l.Prices.Weekend, &l.Prices.Week,
		&l.Prices.Month, &l.Accessories, &l.UsageInstructions, &l.PickupEnabled, &pickLat, &pickLng, &pubLat,
		&pubLng, &l.PublicRadiusM, &l.DeliveryEnabled, &l.DeliveryFee, &l.DeliveryZoneIDs, &l.BookingMode,
		&l.CancelPolicy, &l.MinVerification, &l.MinNoticeHours, &l.MinDurationHours, &l.MaxDurationHours,
		&l.Status, &l.RejectionReason, &l.FirstPublishedAt, &l.Version, &l.CreatedAt, &l.UpdatedAt)
	l.PickupLocation = point(pickLat, pickLng)
	l.PublicLocation = point(pubLat, pubLng)
	return l, err
}

func point(lat, lng *float64) *domain.GeoPoint {
	if lat == nil || lng == nil {
		return nil
	}
	return &domain.GeoPoint{Lat: *lat, Lng: *lng}
}

// nullable: 0 (o vacío) se guarda como NULL, que en la tabla significa "no se ofrece" o "aún sin dato".
func nullable[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}

func pointArgs(p *domain.GeoPoint) (any, any) {
	if p == nil {
		return nil, nil
	}
	return p.Lng, p.Lat
}

// listingValues son los parámetros $2..$34 de listingColumns, en su orden ($1 es el ID).
func listingValues(l domain.ToolListing) []any {
	pickLng, pickLat := pointArgs(l.PickupLocation)
	pubLng, pubLat := pointArgs(l.PublicLocation)
	return []any{
		l.OwnerID, l.CategoryID, l.CityID, nullable(l.ZoneID), l.Title, nullable(l.Description), l.Attributes,
		nullable(l.ReplacementValue), l.Deposit, nullable(l.Prices.Hour), nullable(l.Prices.Day),
		nullable(l.Prices.Weekend), nullable(l.Prices.Week), nullable(l.Prices.Month), l.Accessories,
		nullable(l.UsageInstructions), l.PickupEnabled, pickLng, pickLat, pubLng, pubLat, nullable(l.PublicRadiusM),
		l.DeliveryEnabled, l.DeliveryFee, l.BookingMode, l.CancelPolicy, l.MinVerification, l.MinNoticeHours,
		l.MinDurationHours, l.MaxDurationHours, l.Status, nullable(l.RejectionReason), l.FirstPublishedAt,
	}
}

// pointSQL arma un punto desde dos parámetros (longitud, latitud); NULL si faltan.
func pointSQL(lng, lat string) string {
	return `CASE WHEN ` + lng + `::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint(` + lng + `, ` + lat + `), 4326) END`
}

func (r *ListingRepository) CreateListing(ctx context.Context, l domain.ToolListing, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		args := append([]any{l.ID}, listingValues(l)...)
		if _, err := tx.Exec(ctx, `
			INSERT INTO tool_listings (id, owner_id, category_id, city_id, zone_id, title, description, attributes,
			       replacement_value, deposit, price_hour, price_day, price_weekend, price_week, price_month,
			       accessories, usage_instructions, pickup_enabled, pickup_location, public_location, public_radius_m,
			       delivery_enabled, delivery_fee, booking_mode, cancel_policy, min_verification, min_notice_hours,
			       min_duration_hours, max_duration_hours, status, rejection_reason, first_published_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			        `+pointSQL("$19", "$20")+`, `+pointSQL("$21", "$22")+`, $23, $24, $25, $26, $27, $28, $29,
			        $30, $31, $32, $33, $34)`, args...); err != nil {
			return err
		}
		if err := replaceDeliveryZones(ctx, tx, l.ID, l.DeliveryZoneIDs); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return mapListingError(err)
}

func (r *ListingRepository) UpdateListing(ctx context.Context, l domain.ToolListing, expectedVersion int, audit domain.AuditEntry) (domain.ToolListing, error) {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		args := append([]any{l.ID}, listingValues(l)...)
		args = append(args, expectedVersion)
		tag, err := tx.Exec(ctx, `
			UPDATE tool_listings SET owner_id = $2, category_id = $3, city_id = $4, zone_id = $5, title = $6,
			       description = $7, attributes = $8, replacement_value = $9, deposit = $10, price_hour = $11,
			       price_day = $12, price_weekend = $13, price_week = $14, price_month = $15, accessories = $16,
			       usage_instructions = $17, pickup_enabled = $18, pickup_location = `+pointSQL("$19", "$20")+`,
			       public_location = `+pointSQL("$21", "$22")+`, public_radius_m = $23, delivery_enabled = $24,
			       delivery_fee = $25, booking_mode = $26, cancel_policy = $27, min_verification = $28,
			       min_notice_hours = $29, min_duration_hours = $30, max_duration_hours = $31, status = $32,
			       rejection_reason = $33, first_published_at = $34, version = version + 1
			WHERE id = $1 AND version = $35`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tool_listings WHERE id = $1)`, l.ID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return domain.ErrListingVersion
			}
			return domain.ErrNotFound
		}
		if err := replaceDeliveryZones(ctx, tx, l.ID, l.DeliveryZoneIDs); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	if err != nil {
		return domain.ToolListing{}, mapListingError(err)
	}
	return r.FindListing(ctx, l.ID)
}

func replaceDeliveryZones(ctx context.Context, tx pgx.Tx, listingID string, zoneIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM listing_delivery_zones WHERE listing_id = $1`, listingID); err != nil {
		return err
	}
	if len(zoneIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO listing_delivery_zones (listing_id, zone_id) SELECT $1, unnest($2::uuid[])`, listingID, zoneIDs)
	return err
}

func (r *ListingRepository) FindListing(ctx context.Context, id string) (domain.ToolListing, error) {
	l, err := scanListing(r.db.Pool.QueryRow(ctx, listingSelect+` WHERE l.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ToolListing{}, domain.ErrNotFound
	}
	return l, mapListingError(err)
}

func (r *ListingRepository) ListOwnerListings(ctx context.Context, ownerID string) ([]domain.ToolListing, error) {
	rows, err := r.db.Pool.Query(ctx, listingSelect+` WHERE l.owner_id = $1 ORDER BY l.updated_at DESC, l.id`, ownerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ToolListing, error) { return scanListing(row) })
}

func (r *ListingRepository) OwnerHasPublished(ctx context.Context, ownerID string) (bool, error) {
	var published bool
	err := r.db.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tool_listings WHERE owner_id = $1 AND first_published_at IS NOT NULL)`,
		ownerID).Scan(&published)
	return published, err
}

func (r *ListingRepository) ReadyPhotos(ctx context.Context, listingID string) (int, error) {
	var n int
	err := r.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM listing_photos WHERE listing_id = $1 AND kind = 'public' AND status = 'ready'`,
		listingID).Scan(&n)
	return n, err
}

func (r *ListingRepository) ListBlocks(ctx context.Context, listingID string, from, to time.Time) ([]domain.AvailabilityBlock, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT id, listing_id, lower(period), upper(period), reason, COALESCE(note, ''), COALESCE(created_by::text, ''), created_at
		FROM availability_blocks
		WHERE listing_id = $1 AND period && tstzrange($2, $3)
		ORDER BY lower(period)`, listingID, from, to)
	if err != nil {
		return nil, mapListingError(err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.AvailabilityBlock, error) {
		var b domain.AvailabilityBlock
		err := row.Scan(&b.ID, &b.ListingID, &b.Start, &b.End, &b.Reason, &b.Note, &b.CreatedBy, &b.CreatedAt)
		return b, err
	})
}

func (r *ListingRepository) AddBlock(ctx context.Context, b domain.AvailabilityBlock, audit domain.AuditEntry) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO availability_blocks (listing_id, period, reason, note, created_by)
			VALUES ($1, tstzrange($2, $3), $4, NULLIF($5, ''), NULLIF($6, '')::uuid)
			RETURNING id`, b.ListingID, b.Start, b.End, b.Reason, b.Note, b.CreatedBy).Scan(&id); err != nil {
			return err
		}
		audit.EntityID = b.ListingID
		return insertAudit(ctx, tx, audit)
	})
	return id, mapListingError(err)
}

func (r *ListingRepository) DeleteManualBlock(ctx context.Context, listingID, blockID string, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			DELETE FROM availability_blocks WHERE id = $1 AND listing_id = $2 AND reason = 'manual'`, blockID, listingID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, audit)
	})
	return mapListingError(err)
}
