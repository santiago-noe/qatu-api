package port

import (
	"context"
	"encoding/json"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// CatalogAdmin lee y escribe todo el catálogo (incluido lo apagado o prohibido). Cada escritura
// guarda su auditoría en la misma transacción. Implementación: Postgres.
type CatalogAdmin interface {
	ListAllCategories(ctx context.Context, vertical domain.Vertical) ([]domain.Category, error)
	// FindCategory devuelve domain.ErrNotFound si no existe.
	FindCategory(ctx context.Context, id string) (domain.Category, error)
	// CreateCategory devuelve el ID creado; domain.ErrSlugTaken o domain.ErrCategoryTree si la base lo rechaza.
	CreateCategory(ctx context.Context, c domain.Category, audit domain.AuditEntry) (string, error)
	UpdateCategory(ctx context.Context, c domain.Category, audit domain.AuditEntry) error

	// FindCityBySlug incluye las ciudades apagadas (el admin las enciende).
	FindCityBySlug(ctx context.Context, slug string) (domain.City, error)
	SetCityEnabled(ctx context.Context, cityID string, enabled bool, audit domain.AuditEntry) error
	// SetCategoryCityScope fija si la categoría está activa en la ciudad; nil vuelve a su valor global.
	SetCategoryCityScope(ctx context.Context, categoryID, cityID string, enabled *bool, audit domain.AuditEntry) error

	ListSettings(ctx context.Context) ([]domain.Setting, error)
	// FindSetting devuelve el ajuste de ese alcance exacto, o domain.ErrNotFound.
	FindSetting(ctx context.Context, key, cityID, categoryID string) (domain.Setting, error)
	// UpsertSetting crea o reemplaza el valor del alcance y sube la versión.
	UpsertSetting(ctx context.Context, s domain.Setting, audit domain.AuditEntry) (domain.Setting, error)
	// SettingHistory devuelve los cambios de la clave (de audit_log), del más reciente al más antiguo.
	SettingHistory(ctx context.Context, key string, limit int) ([]domain.SettingChange, error)
}

// SchemaValidator valida JSON Schema (2020-12). Implementación: santhosh-tekuri/jsonschema.
type SchemaValidator interface {
	// CheckSchema devuelve domain.ErrInvalidSchema si el documento no es un esquema de objeto válido.
	CheckSchema(schema json.RawMessage) error
}
