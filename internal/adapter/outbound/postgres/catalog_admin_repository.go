package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Implementa port.CatalogAdmin sobre el mismo CatalogRepository (comparte consultas de lectura).

const (
	checkViolation      = "23514"
	foreignKeyViolation = "23503"
	invalidTextInput    = "22P02" // por ejemplo, un ID que no es UUID
)

// mapCatalogError traduce los rechazos de la base (restricciones y el trigger del árbol) a errores de dominio.
func mapCatalogError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == "categories_slug_per_vertical":
		return domain.ErrSlugTaken
	case pgErr.Code == checkViolation && strings.HasPrefix(pgErr.Message, "categories:"):
		return domain.ErrCategoryTree
	case pgErr.Code == foreignKeyViolation, pgErr.Code == invalidTextInput:
		return domain.ErrNotFound
	}
	return err
}

func (r *CatalogRepository) ListAllCategories(ctx context.Context, vertical domain.Vertical) ([]domain.Category, error) {
	return r.queryCategories(ctx, categorySelect+` WHERE c.vertical = $1`+categoryOrder, vertical)
}

func (r *CatalogRepository) FindCategory(ctx context.Context, id string) (domain.Category, error) {
	c, err := scanCategory(r.db.Pool.QueryRow(ctx, categorySelect+` WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Category{}, domain.ErrNotFound
	}
	return c, mapCatalogError(err)
}

func (r *CatalogRepository) CreateCategory(ctx context.Context, c domain.Category, audit domain.AuditEntry) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (vertical, parent_id, slug, name, description, icon, sort_order,
			                        attributes_schema, risk_level, prohibited, enabled)
			VALUES ($1, NULLIF($2, '')::uuid, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, $11)
			RETURNING id`,
			c.Vertical, c.ParentID, c.Slug, c.Name, c.Description, c.Icon, c.SortOrder,
			c.AttributesSchema, c.RiskLevel, c.Prohibited, c.Enabled).Scan(&id); err != nil {
			return err
		}
		audit.EntityID = id
		return insertAudit(ctx, tx, audit)
	})
	return id, mapCatalogError(err)
}

// UpdateCategory reemplaza los campos editables; la vertical no cambia nunca.
func (r *CatalogRepository) UpdateCategory(ctx context.Context, c domain.Category, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE categories SET parent_id = NULLIF($2, '')::uuid, slug = $3, name = $4, description = NULLIF($5, ''),
			       icon = NULLIF($6, ''), sort_order = $7, attributes_schema = $8, risk_level = $9,
			       prohibited = $10, enabled = $11, version = version + 1
			WHERE id = $1`,
			c.ID, c.ParentID, c.Slug, c.Name, c.Description, c.Icon, c.SortOrder,
			c.AttributesSchema, c.RiskLevel, c.Prohibited, c.Enabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, audit)
	})
	return mapCatalogError(err)
}

func (r *CatalogRepository) FindCityBySlug(ctx context.Context, slug string) (domain.City, error) {
	c, err := scanCity(r.db.Pool.QueryRow(ctx, citySelect+` WHERE slug = $1`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.City{}, domain.ErrNotFound
	}
	return c, err
}

func (r *CatalogRepository) SetCityEnabled(ctx context.Context, cityID string, enabled bool, audit domain.AuditEntry) error {
	return pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE cities SET enabled = $2 WHERE id = $1`, cityID, enabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *CatalogRepository) SetCategoryCityScope(ctx context.Context, categoryID, cityID string, enabled *bool, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		var err error
		if enabled == nil {
			_, err = tx.Exec(ctx, `DELETE FROM category_city_overrides WHERE category_id = $1 AND city_id = $2`, categoryID, cityID)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO category_city_overrides (category_id, city_id, enabled) VALUES ($1, $2, $3)
				ON CONFLICT (category_id, city_id) DO UPDATE SET enabled = EXCLUDED.enabled`, categoryID, cityID, *enabled)
		}
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return mapCatalogError(err)
}

const settingSelect = `
	SELECT id, key, COALESCE(city_id::text, ''), COALESCE(category_id::text, ''), value,
	       COALESCE(description, ''), COALESCE(updated_by::text, ''), updated_at, version
	FROM platform_settings`

func scanSetting(row pgx.Row) (domain.Setting, error) {
	var s domain.Setting
	err := row.Scan(&s.ID, &s.Key, &s.CityID, &s.CategoryID, &s.Value, &s.Description, &s.UpdatedBy, &s.UpdatedAt, &s.Version)
	return s, err
}

func (r *CatalogRepository) ListSettings(ctx context.Context) ([]domain.Setting, error) {
	rows, err := r.db.Pool.Query(ctx, settingSelect+` ORDER BY key, city_id NULLS FIRST, category_id NULLS FIRST`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Setting, error) { return scanSetting(row) })
}

// settingScope: el alcance exacto (NULL = todas), igual que la restricción platform_settings_scope.
const settingScope = ` WHERE key = $1
	AND city_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
	AND category_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid`

func (r *CatalogRepository) FindSetting(ctx context.Context, key, cityID, categoryID string) (domain.Setting, error) {
	s, err := scanSetting(r.db.Pool.QueryRow(ctx, settingSelect+settingScope, key, cityID, categoryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Setting{}, domain.ErrNotFound
	}
	return s, mapCatalogError(err)
}

func (r *CatalogRepository) UpsertSetting(ctx context.Context, s domain.Setting, audit domain.AuditEntry) (domain.Setting, error) {
	var saved domain.Setting
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		var err error
		saved, err = scanSetting(tx.QueryRow(ctx, `
			INSERT INTO platform_settings (key, city_id, category_id, value, description, updated_by)
			VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, NULLIF($5, ''), NULLIF($6, '')::uuid)
			ON CONFLICT ON CONSTRAINT platform_settings_scope DO UPDATE
			SET value = EXCLUDED.value, description = COALESCE(EXCLUDED.description, platform_settings.description),
			    updated_by = EXCLUDED.updated_by, version = platform_settings.version + 1
			RETURNING id, key, COALESCE(city_id::text, ''), COALESCE(category_id::text, ''), value,
			          COALESCE(description, ''), COALESCE(updated_by::text, ''), updated_at, version`,
			s.Key, s.CityID, s.CategoryID, s.Value, s.Description, s.UpdatedBy))
		if err != nil {
			return err
		}
		audit.EntityID = saved.ID
		return insertAudit(ctx, tx, audit)
	})
	return saved, mapCatalogError(err)
}

// SettingHistory lee audit_log: el historial es la auditoría (inmutable), no una tabla aparte.
func (r *CatalogRepository) SettingHistory(ctx context.Context, key string, limit int) ([]domain.SettingChange, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT at, COALESCE(actor_id::text, ''), COALESCE(host(ip), ''), COALESCE(before, 'null'), COALESCE(after, 'null')
		FROM audit_log
		WHERE action = $1 AND after->>'key' = $2
		ORDER BY at DESC, id DESC
		LIMIT $3`, domain.AuditSettingChanged, key, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SettingChange, error) {
		var c domain.SettingChange
		err := row.Scan(&c.At, &c.ActorID, &c.IP, &c.Before, &c.After)
		return c, err
	})
}
