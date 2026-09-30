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
	// UpdateCategory guarda los cambios. Si queda prohibida, en la misma transacción rechaza las
	// publicaciones activas de ella y de sus tipos con domain.ProhibitedCategoryReason, y lo audita.
	UpdateCategory(ctx context.Context, c domain.Category, audit domain.AuditEntry) error

	// ListAllCities devuelve todas las ciudades por nombre, también las apagadas.
	ListAllCities(ctx context.Context) ([]domain.City, error)
	// FindCityBySlug incluye las ciudades apagadas (el admin las enciende).
	FindCityBySlug(ctx context.Context, slug string) (domain.City, error)
	// CreateCity devuelve el ID; domain.ErrCityTaken si el slug o el ubigeo ya existen. Nace apagada.
	CreateCity(ctx context.Context, c domain.City, audit domain.AuditEntry) (string, error)
	// UpdateCity guarda nombre, región, centro y si está encendida (el slug y el ubigeo no cambian).
	UpdateCity(ctx context.Context, c domain.City, audit domain.AuditEntry) error
	// SetCategoryCityScope fija si la categoría está activa en la ciudad; nil vuelve a su valor global.
	SetCategoryCityScope(ctx context.Context, categoryID, cityID string, enabled *bool, audit domain.AuditEntry) error
	// CategoryCityScopes devuelve cada ciudad (también las apagadas) con el ajuste de la categoría en ella.
	CategoryCityScopes(ctx context.Context, categoryID string) ([]domain.CategoryCityScope, error)

	// ListAllZones devuelve los distritos de la ciudad, también los apagados, con su límite simplificado.
	ListAllZones(ctx context.Context, cityID string) ([]domain.Zone, error)
	// FindZone busca por slug dentro de la ciudad, sin el límite; domain.ErrNotFound si no existe.
	FindZone(ctx context.Context, cityID, slug string) (domain.Zone, error)
	// CreateZone devuelve el ID; domain.ErrZoneTaken si el slug (en la ciudad) o el ubigeo ya existen.
	CreateZone(ctx context.Context, z domain.Zone, audit domain.AuditEntry) (string, error)
	// UpdateZone guarda nombre, orden y si está activo; con Boundary nil conserva el límite.
	UpdateZone(ctx context.Context, z domain.Zone, audit domain.AuditEntry) error
	// ZoneOverlap devuelve la mayor fracción del área del límite que comparte con otro distrito de
	// la ciudad (0 a 1), sin contar exceptZoneID (el mismo distrito al editarlo).
	ZoneOverlap(ctx context.Context, cityID, exceptZoneID string, boundary json.RawMessage) (float64, error)

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
