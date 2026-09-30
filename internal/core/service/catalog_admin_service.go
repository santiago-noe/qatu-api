package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// defaultAttributesSchema: una categoría sin atributos propios.
var defaultAttributesSchema = json.RawMessage(`{"type": "object", "properties": {}}`)

// CategoryPatch son los cambios de una categoría; nil = no cambia. La vertical no se edita.
type CategoryPatch struct {
	ParentID         *string
	Slug             *string
	Name             *string
	Description      *string
	Icon             *string
	SortOrder        *int
	AttributesSchema json.RawMessage // nil = no cambia
	RiskLevel        *domain.RiskLevel
	Prohibited       *bool
	Enabled          *bool
}

// SettingInput es un cambio de platform_settings en un alcance (ciudad y categoría opcionales).
type SettingInput struct {
	Key         string
	CitySlug    string
	CategoryID  string
	Value       json.RawMessage
	Description string
}

// CatalogAdminService: el admin edita categorías, ciudades y ajustes. Cada cambio se audita en la
// misma transacción y limpia la caché del catálogo público.
type CatalogAdminService struct {
	repo    port.CatalogAdmin
	schemas port.SchemaValidator
	cache   port.Cache
}

func NewCatalogAdminService(repo port.CatalogAdmin, schemas port.SchemaValidator, cache port.Cache) *CatalogAdminService {
	return &CatalogAdminService{repo: repo, schemas: schemas, cache: cache}
}

// Categories devuelve todo el árbol de la vertical, incluido lo apagado y lo prohibido.
func (s *CatalogAdminService) Categories(ctx context.Context, vertical domain.Vertical) ([]domain.Category, error) {
	flat, err := s.repo.ListAllCategories(ctx, vertical)
	return domain.BuildCategoryTree(flat), err
}

func (s *CatalogAdminService) CreateCategory(ctx context.Context, actorID string, in domain.Category, ip string) (domain.Category, error) {
	if len(in.AttributesSchema) == 0 {
		in.AttributesSchema = defaultAttributesSchema
	}
	c, err := s.validCategory(in)
	if err != nil {
		return domain.Category{}, err
	}
	id, err := s.repo.CreateCategory(ctx, c, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditCategoryCreated, Entity: "category", After: categorySnapshot(c), IP: ip,
	})
	if err != nil {
		return domain.Category{}, err
	}
	s.invalidate(ctx)
	return s.repo.FindCategory(ctx, id)
}

func (s *CatalogAdminService) UpdateCategory(ctx context.Context, actorID, id string, p CategoryPatch, ip string) (domain.Category, error) {
	current, err := s.repo.FindCategory(ctx, id)
	if err != nil {
		return domain.Category{}, err
	}
	next := applyCategoryPatch(current, p)
	if next, err = s.validCategory(next); err != nil {
		return domain.Category{}, err
	}
	before, after := categorySnapshot(current), categorySnapshot(next)
	if reflect.DeepEqual(before, after) {
		return current, nil // nada cambia: ni escritura ni auditoría
	}
	err = s.repo.UpdateCategory(ctx, next, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditCategoryUpdated, Entity: "category", EntityID: id,
		Before: before, After: after, IP: ip,
	})
	if err != nil {
		return domain.Category{}, err
	}
	s.invalidate(ctx)
	return next, nil
}

// SetCategoryCityScope activa o desactiva una categoría solo en una ciudad; nil vuelve a su valor global.
func (s *CatalogAdminService) SetCategoryCityScope(ctx context.Context, actorID, categoryID, citySlug string, enabled *bool, ip string) error {
	if _, err := s.repo.FindCategory(ctx, categoryID); err != nil {
		return err
	}
	city, err := s.repo.FindCityBySlug(ctx, citySlug)
	if err != nil {
		return err
	}
	err = s.repo.SetCategoryCityScope(ctx, categoryID, city.ID, enabled, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditCategoryCityScope, Entity: "category", EntityID: categoryID,
		After: map[string]any{"city": city.Slug, "enabled": enabled}, IP: ip,
	})
	if err != nil {
		return err
	}
	s.invalidate(ctx)
	return nil
}

func (s *CatalogAdminService) Settings(ctx context.Context) ([]domain.Setting, error) {
	return s.repo.ListSettings(ctx)
}

// SetSetting cambia un ajuste en su alcance. Las transacciones ya creadas guardaron su copia de
// los valores (price_snapshot), así que el cambio solo afecta a las nuevas (spec 002).
func (s *CatalogAdminService) SetSetting(ctx context.Context, actorID string, in SettingInput, ip string) (domain.Setting, error) {
	value, err := domain.NormalizeSettingValue(in.Key, in.Value)
	if err != nil {
		return domain.Setting{}, err
	}
	cityID := ""
	if in.CitySlug != "" {
		city, err := s.repo.FindCityBySlug(ctx, in.CitySlug)
		if err != nil {
			return domain.Setting{}, err
		}
		cityID = city.ID
	}
	if in.CategoryID != "" {
		if _, err := s.repo.FindCategory(ctx, in.CategoryID); err != nil {
			return domain.Setting{}, err
		}
	}

	current, err := s.repo.FindSetting(ctx, in.Key, cityID, in.CategoryID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Setting{}, err
	}
	exists := err == nil
	if exists && bytes.Equal(current.Value, value) && (in.Description == "" || in.Description == current.Description) {
		return current, nil
	}

	scope := map[string]any{"key": in.Key, "city": in.CitySlug, "category_id": in.CategoryID}
	after := map[string]any{"value": value}
	var before any
	if exists {
		before = with(scope, map[string]any{"value": current.Value})
	}
	saved, err := s.repo.UpsertSetting(ctx, domain.Setting{
		Key: in.Key, CityID: cityID, CategoryID: in.CategoryID, Value: value, Description: in.Description, UpdatedBy: actorID,
	}, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditSettingChanged, Entity: "platform_setting",
		Before: before, After: with(scope, after), IP: ip,
	})
	return saved, err
}

// SettingHistory devuelve los últimos cambios de una clave admitida.
func (s *CatalogAdminService) SettingHistory(ctx context.Context, key string, limit int) ([]domain.SettingChange, error) {
	if !domain.IsKnownSetting(key) {
		return nil, domain.ErrUnknownSetting
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return s.repo.SettingHistory(ctx, key, limit)
}

// validCategory normaliza los campos y valida el JSON Schema de atributos.
func (s *CatalogAdminService) validCategory(c domain.Category) (domain.Category, error) {
	c, err := domain.NormalizeCategory(c)
	if err != nil {
		return domain.Category{}, err
	}
	if err := s.schemas.CheckSchema(c.AttributesSchema); err != nil {
		return domain.Category{}, err
	}
	return c, nil
}

// invalidate limpia la caché del catálogo público. Si Redis falla, el TTL (1 h) acota lo viejo.
func (s *CatalogAdminService) invalidate(ctx context.Context) {
	_ = s.cache.DeletePrefix(ctx, CatalogCachePrefix)
}

func applyCategoryPatch(c domain.Category, p CategoryPatch) domain.Category {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&c.ParentID, p.ParentID)
	set(&c.Slug, p.Slug)
	set(&c.Name, p.Name)
	set(&c.Description, p.Description)
	set(&c.Icon, p.Icon)
	if p.SortOrder != nil {
		c.SortOrder = *p.SortOrder
	}
	if p.AttributesSchema != nil {
		c.AttributesSchema = p.AttributesSchema
	}
	if p.RiskLevel != nil {
		c.RiskLevel = *p.RiskLevel
	}
	if p.Prohibited != nil {
		c.Prohibited = *p.Prohibited
	}
	if p.Enabled != nil {
		c.Enabled = *p.Enabled
	}
	return c
}

// categorySnapshot: los campos editables, para auditar y para detectar si algo cambió.
func categorySnapshot(c domain.Category) map[string]any {
	return map[string]any{
		"vertical": c.Vertical, "parent_id": c.ParentID, "slug": c.Slug, "name": c.Name,
		"description": c.Description, "icon": c.Icon, "sort_order": c.SortOrder,
		"attributes_schema": compactJSON(c.AttributesSchema), "risk_level": c.RiskLevel,
		"prohibited": c.Prohibited, "enabled": c.Enabled,
	}
}

// compactJSON quita espacios para comparar esquemas por contenido, no por formato.
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}

// with devuelve una copia de base con los campos extra (el alcance va en el antes y el después).
func with(base map[string]any, extra map[string]any) map[string]any {
	out := maps.Clone(base)
	maps.Copy(out, extra)
	return out
}
