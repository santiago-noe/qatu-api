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

// ProviderRepository implementa port.ProviderRepository.
type ProviderRepository struct {
	db *Client
}

func NewProviderRepository(db *Client) *ProviderRepository { return &ProviderRepository{db: db} }

// mapProviderError traduce los rechazos de la base: el trigger de oficio, un distrito o una
// categoría que no existen, las franjas o bloqueos que se cruzan.
func mapProviderError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == checkViolation && strings.HasPrefix(pgErr.Message, "provider_trades:"):
		return domain.ErrProviderTrade
	case pgErr.Code == exclusionViolation && pgErr.ConstraintName == "weekly_availability_no_overlap":
		return domain.ErrProviderSchedule
	case pgErr.Code == exclusionViolation:
		return domain.ErrAvailabilityOverlap
	case pgErr.Code == foreignKeyViolation && strings.Contains(pgErr.ConstraintName, "zone"):
		return domain.ErrProviderCoverage
	case pgErr.Code == foreignKeyViolation && strings.Contains(pgErr.ConstraintName, "category"):
		return domain.ErrProviderTrade
	case pgErr.Code == invalidTextInput:
		return domain.ErrNotFound
	}
	return err
}

func (r *ProviderRepository) FindProvider(ctx context.Context, userID string) (domain.ProviderProfile, error) {
	var p domain.ProviderProfile
	err := r.db.Pool.QueryRow(ctx, `
		SELECT user_id, COALESCE(business_name, ''), phone, city_id, COALESCE(bio, ''), years_experience,
		       work_warranty_days, accepts_urgent, status, COALESCE(rejection_reason, ''), verified_at,
		       first_published_at, version, created_at, updated_at,
		       COALESCE((SELECT array_agg(z.zone_id::text ORDER BY z.zone_id) FROM provider_coverage_zones z
		                 WHERE z.provider_id = p.user_id), '{}')
		FROM provider_profiles p WHERE user_id = $1`, userID).
		Scan(&p.UserID, &p.BusinessName, &p.Phone, &p.CityID, &p.Bio, &p.YearsExperience, &p.WarrantyDays,
			&p.AcceptsUrgent, &p.Status, &p.RejectionReason, &p.VerifiedAt, &p.FirstPublishedAt, &p.Version,
			&p.CreatedAt, &p.UpdatedAt, &p.CoverageZoneIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProviderProfile{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ProviderProfile{}, mapProviderError(err)
	}
	if p.Trades, err = r.trades(ctx, userID); err != nil {
		return domain.ProviderProfile{}, err
	}
	p.Weekly, err = r.weekly(ctx, userID)
	return p, err
}

// trades lee los oficios en el orden del proveedor, cada uno con sus paquetes.
func (r *ProviderRepository) trades(ctx context.Context, providerID string) ([]domain.ProviderTrade, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT category_id, COALESCE(hourly_rate, 0), min_hours FROM provider_trades
		WHERE provider_id = $1 ORDER BY sort_order, category_id`, providerID)
	if err != nil {
		return nil, err
	}
	trades, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ProviderTrade, error) {
		var t domain.ProviderTrade
		err := row.Scan(&t.CategoryID, &t.HourlyRate, &t.MinHours)
		return t, err
	})
	if err != nil {
		return nil, err
	}
	rows, err = r.db.Pool.Query(ctx, `
		SELECT id, category_id, title, COALESCE(description, ''), price, duration_minutes FROM service_packages
		WHERE provider_id = $1 ORDER BY sort_order, created_at, id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pkg domain.ServicePackage
		var categoryID string
		if err := rows.Scan(&pkg.ID, &categoryID, &pkg.Title, &pkg.Description, &pkg.Price, &pkg.DurationMinutes); err != nil {
			return nil, err
		}
		for i := range trades {
			if trades[i].CategoryID == categoryID {
				trades[i].Packages = append(trades[i].Packages, pkg)
			}
		}
	}
	return trades, rows.Err()
}

func (r *ProviderRepository) weekly(ctx context.Context, providerID string) ([]domain.WeeklySlot, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT weekday, start_minute, end_minute FROM weekly_availability
		WHERE provider_id = $1 ORDER BY weekday, start_minute`, providerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.WeeklySlot, error) {
		var s domain.WeeklySlot
		err := row.Scan(&s.Weekday, &s.Start, &s.End)
		return s, err
	})
}

func (r *ProviderRepository) CreateProvider(ctx context.Context, p domain.ProviderProfile, consent domain.Consent, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO provider_profiles (user_id, business_name, phone, city_id, bio, years_experience,
			       work_warranty_days, accepts_urgent, status)
			VALUES ($1, NULLIF($2, ''), $3, $4, NULLIF($5, ''), $6, $7, $8, $9)`,
			p.UserID, p.BusinessName, p.Phone, p.CityID, p.Bio, p.YearsExperience, p.WarrantyDays,
			p.AcceptsUrgent, p.Status); err != nil {
			return err
		}
		if err := replaceProviderDetails(ctx, tx, p); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO consents (user_id, purpose, version, granted_at, ip) VALUES ($1, $2, $3, $4, $5)`,
			p.UserID, consent.Purpose, consent.Version, consent.GrantedAt, parseIP(audit.IP)); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return mapProviderError(err)
}

func (r *ProviderRepository) UpdateProvider(ctx context.Context, p domain.ProviderProfile, expectedVersion int, audit domain.AuditEntry) (domain.ProviderProfile, error) {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE provider_profiles SET business_name = NULLIF($2, ''), phone = $3, city_id = $4, bio = NULLIF($5, ''),
			       years_experience = $6, work_warranty_days = $7, accepts_urgent = $8, status = $9,
			       rejection_reason = NULLIF($10, ''), verified_at = $11, first_published_at = $12, version = version + 1
			WHERE user_id = $1 AND version = $13`,
			p.UserID, p.BusinessName, p.Phone, p.CityID, p.Bio, p.YearsExperience, p.WarrantyDays, p.AcceptsUrgent,
			p.Status, p.RejectionReason, p.VerifiedAt, p.FirstPublishedAt, expectedVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM provider_profiles WHERE user_id = $1)`, p.UserID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return domain.ErrProviderVersion
			}
			return domain.ErrNotFound
		}
		if err := replaceProviderDetails(ctx, tx, p); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	if err != nil {
		return domain.ProviderProfile{}, mapProviderError(err)
	}
	return r.FindProvider(ctx, p.UserID)
}

// replaceProviderDetails deja oficios, paquetes, cobertura y horario como en p. Los paquetes que
// siguen conservan su fila (un trabajo de la 009 podrá apuntar a ellos); el resto se borra.
func replaceProviderDetails(ctx context.Context, tx pgx.Tx, p domain.ProviderProfile) error {
	categories := make([]string, len(p.Trades))
	for i, t := range p.Trades {
		categories[i] = t.CategoryID
	}
	steps := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM provider_trades WHERE provider_id = $1 AND NOT (category_id = ANY(COALESCE($2::uuid[], '{}')))`,
			[]any{p.UserID, categories}},
		{`DELETE FROM service_packages WHERE provider_id = $1 AND NOT (id = ANY(COALESCE($2::uuid[], '{}')))`,
			[]any{p.UserID, p.PackageIDs()}},
		{`DELETE FROM provider_coverage_zones WHERE provider_id = $1`, []any{p.UserID}},
		{`DELETE FROM weekly_availability WHERE provider_id = $1`, []any{p.UserID}},
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			return err
		}
	}

	batch := &pgx.Batch{}
	for i, t := range p.Trades {
		batch.Queue(`
			INSERT INTO provider_trades (provider_id, category_id, hourly_rate, min_hours, sort_order)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (provider_id, category_id) DO UPDATE SET hourly_rate = EXCLUDED.hourly_rate,
			       min_hours = EXCLUDED.min_hours, sort_order = EXCLUDED.sort_order`,
			p.UserID, t.CategoryID, nullable(t.HourlyRate), t.MinHours, i)
		for j, pkg := range t.Packages {
			batch.Queue(`
				INSERT INTO service_packages (id, provider_id, category_id, title, description, price, duration_minutes, sort_order)
				VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8)
				ON CONFLICT (id) DO UPDATE SET category_id = EXCLUDED.category_id, title = EXCLUDED.title,
				       description = EXCLUDED.description, price = EXCLUDED.price,
				       duration_minutes = EXCLUDED.duration_minutes, sort_order = EXCLUDED.sort_order
				WHERE service_packages.provider_id = EXCLUDED.provider_id`,
				pkg.ID, p.UserID, t.CategoryID, pkg.Title, pkg.Description, pkg.Price, pkg.DurationMinutes, j)
		}
	}
	if len(p.CoverageZoneIDs) > 0 {
		batch.Queue(`INSERT INTO provider_coverage_zones (provider_id, zone_id) SELECT $1, unnest($2::uuid[])`,
			p.UserID, p.CoverageZoneIDs)
	}
	for _, s := range p.Weekly {
		batch.Queue(`INSERT INTO weekly_availability (provider_id, weekday, start_minute, end_minute) VALUES ($1, $2, $3, $4)`,
			p.UserID, s.Weekday, s.Start, s.End)
	}
	return tx.SendBatch(ctx, batch).Close()
}

func (r *ProviderRepository) ListProviderBlocks(ctx context.Context, providerID string, from, to time.Time) ([]domain.AvailabilityBlock, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT id, lower(period), upper(period), reason, COALESCE(note, ''), COALESCE(created_by::text, ''), created_at
		FROM provider_blocks
		WHERE provider_id = $1 AND period && tstzrange($2, $3)
		ORDER BY lower(period)`, providerID, from, to)
	if err != nil {
		return nil, mapProviderError(err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.AvailabilityBlock, error) {
		var b domain.AvailabilityBlock
		err := row.Scan(&b.ID, &b.Start, &b.End, &b.Reason, &b.Note, &b.CreatedBy, &b.CreatedAt)
		return b, err
	})
}

func (r *ProviderRepository) AddProviderBlock(ctx context.Context, providerID string, b domain.AvailabilityBlock, audit domain.AuditEntry) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO provider_blocks (provider_id, period, reason, note, created_by)
			VALUES ($1, tstzrange($2, $3), $4, NULLIF($5, ''), NULLIF($6, '')::uuid)
			RETURNING id`, providerID, b.Start, b.End, b.Reason, b.Note, b.CreatedBy).Scan(&id); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return id, mapProviderError(err)
}

func (r *ProviderRepository) DeleteProviderManualBlock(ctx context.Context, providerID, blockID string, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			DELETE FROM provider_blocks WHERE id = $1 AND provider_id = $2 AND reason = 'manual'`, blockID, providerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, audit)
	})
	return mapProviderError(err)
}
