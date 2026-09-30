package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// LenderRepository implementa port.LenderRepository.
type LenderRepository struct {
	db *Client
}

func NewLenderRepository(db *Client) *LenderRepository { return &LenderRepository{db: db} }

func (r *LenderRepository) FindLenderProfile(ctx context.Context, userID string) (domain.LenderProfile, error) {
	var p domain.LenderProfile
	err := r.db.Pool.QueryRow(ctx, `
		SELECT user_id, kind, COALESCE(business_name, ''), phone, city_id, zone_id, created_at, updated_at
		FROM lender_profiles WHERE user_id = $1`, userID).
		Scan(&p.UserID, &p.Kind, &p.BusinessName, &p.Phone, &p.CityID, &p.ZoneID, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LenderProfile{}, domain.ErrNotFound
	}
	return p, err
}

// SaveLenderProfile crea o actualiza el perfil y da el rol lender, en una transacción.
func (r *LenderRepository) SaveLenderProfile(ctx context.Context, a port.LenderActivation) error {
	p := a.Profile
	return pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO lender_profiles (user_id, kind, business_name, phone, city_id, zone_id)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6)
			ON CONFLICT (user_id) DO UPDATE SET kind = EXCLUDED.kind, business_name = EXCLUDED.business_name,
			       phone = EXCLUDED.phone, city_id = EXCLUDED.city_id, zone_id = EXCLUDED.zone_id`,
			p.UserID, p.Kind, p.BusinessName, p.Phone, p.CityID, p.ZoneID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_roles (user_id, role, granted_at) VALUES ($1, $2, now()) ON CONFLICT DO NOTHING`,
			p.UserID, domain.RoleLender); err != nil {
			return err
		}
		if err := bumpVersion(ctx, tx, p.UserID); err != nil {
			return err
		}
		if c := a.Consent; c != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO consents (user_id, purpose, version, granted_at, ip) VALUES ($1, $2, $3, $4, $5)`,
				p.UserID, c.Purpose, c.Version, c.GrantedAt, parseIP(a.Audit.IP)); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, a.Audit)
	})
}
