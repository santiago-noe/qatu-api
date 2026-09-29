package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/adapter/inbound/http/middleware"
	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// CatalogAdmin es lo que el handler necesita de service.CatalogAdminService.
type CatalogAdmin interface {
	Categories(ctx context.Context, vertical domain.Vertical) ([]domain.Category, error)
	CreateCategory(ctx context.Context, actorID string, in domain.Category, ip string) (domain.Category, error)
	UpdateCategory(ctx context.Context, actorID, id string, p service.CategoryPatch, ip string) (domain.Category, error)
	Cities(ctx context.Context) ([]domain.City, error)
	SetCityEnabled(ctx context.Context, actorID, citySlug string, enabled bool, ip string) (domain.City, error)
	SetCategoryCityScope(ctx context.Context, actorID, categoryID, citySlug string, enabled *bool, ip string) error
	Settings(ctx context.Context) ([]domain.Setting, error)
	SetSetting(ctx context.Context, actorID string, in service.SettingInput, ip string) (domain.Setting, error)
	SettingHistory(ctx context.Context, key string, limit int) ([]domain.SettingChange, error)
}

// AdminCatalogHandler atiende /api/v1/admin/catalog, /admin/cities y /admin/settings (rol admin y segundo paso).
type AdminCatalogHandler struct {
	admin CatalogAdmin
}

func NewAdminCatalogHandler(admin CatalogAdmin) *AdminCatalogHandler {
	return &AdminCatalogHandler{admin: admin}
}

// adminCategoryResponse muestra también lo que el público no ve: apagada, prohibida y orden.
type adminCategoryResponse struct {
	ID               string                  `json:"id"`
	Vertical         string                  `json:"vertical"`
	ParentID         string                  `json:"parent_id,omitempty"`
	Slug             string                  `json:"slug"`
	Name             string                  `json:"name"`
	Description      string                  `json:"description,omitempty"`
	Icon             string                  `json:"icon,omitempty"`
	SortOrder        int                     `json:"sort_order"`
	AttributesSchema json.RawMessage         `json:"attributes_schema"`
	RiskLevel        string                  `json:"risk_level"`
	Prohibited       bool                    `json:"prohibited"`
	Enabled          bool                    `json:"enabled"`
	Children         []adminCategoryResponse `json:"children,omitempty"`
}

func toAdminCategory(c domain.Category) adminCategoryResponse {
	out := adminCategoryResponse{
		ID: c.ID, Vertical: string(c.Vertical), ParentID: c.ParentID, Slug: c.Slug, Name: c.Name,
		Description: c.Description, Icon: c.Icon, SortOrder: c.SortOrder, AttributesSchema: c.AttributesSchema,
		RiskLevel: string(c.RiskLevel), Prohibited: c.Prohibited, Enabled: c.Enabled,
	}
	for _, child := range c.Children {
		out.Children = append(out.Children, toAdminCategory(child))
	}
	return out
}

type createCategoryRequest struct {
	Vertical         string          `json:"vertical"`
	ParentID         string          `json:"parent_id"`
	Slug             string          `json:"slug"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Icon             string          `json:"icon"`
	SortOrder        int             `json:"sort_order"`
	AttributesSchema json.RawMessage `json:"attributes_schema"`
	RiskLevel        string          `json:"risk_level"`
	Prohibited       bool            `json:"prohibited"`
	Enabled          *bool           `json:"enabled"` // por defecto, activa
}

type updateCategoryRequest struct {
	ParentID         *string           `json:"parent_id"`
	Slug             *string           `json:"slug"`
	Name             *string           `json:"name"`
	Description      *string           `json:"description"`
	Icon             *string           `json:"icon"`
	SortOrder        *int              `json:"sort_order"`
	AttributesSchema json.RawMessage   `json:"attributes_schema"`
	RiskLevel        *domain.RiskLevel `json:"risk_level"`
	Prohibited       *bool             `json:"prohibited"`
	Enabled          *bool             `json:"enabled"`
}

type enabledRequest struct {
	Enabled *bool `json:"enabled"` // en el alcance por ciudad, null vuelve al valor global
}

type setSettingRequest struct {
	Key         string          `json:"key"`
	City        string          `json:"city"`
	CategoryID  string          `json:"category_id"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description"`
}

type settingResponse struct {
	ID          string          `json:"id"`
	Key         string          `json:"key"`
	CityID      string          `json:"city_id,omitempty"`
	CategoryID  string          `json:"category_id,omitempty"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description,omitempty"`
	UpdatedBy   string          `json:"updated_by,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Version     int             `json:"version"`
}

func toSettingResponse(s domain.Setting) settingResponse {
	return settingResponse{ID: s.ID, Key: s.Key, CityID: s.CityID, CategoryID: s.CategoryID, Value: s.Value,
		Description: s.Description, UpdatedBy: s.UpdatedBy, UpdatedAt: s.UpdatedAt, Version: s.Version}
}

type settingChangeResponse struct {
	At      time.Time       `json:"at"`
	ActorID string          `json:"actor_id,omitempty"`
	IP      string          `json:"ip,omitempty"`
	Before  json.RawMessage `json:"before"`
	After   json.RawMessage `json:"after"`
}

func actorID(c fiber.Ctx) string {
	session, _ := middleware.SessionFrom(c)
	return session.UserID
}

// GET /api/v1/admin/catalog/categories?vertical=rental
func (h *AdminCatalogHandler) Categories(c fiber.Ctx) error {
	vertical, ok := domain.ParseVertical(c.Query("vertical"))
	if !ok {
		return domain.ErrInvalidVertical
	}
	tree, err := h.admin.Categories(c.Context(), vertical)
	if err != nil {
		return err
	}
	out := make([]adminCategoryResponse, len(tree))
	for i, cat := range tree {
		out[i] = toAdminCategory(cat)
	}
	return c.JSON(fiber.Map{"categories": out})
}

// POST /api/v1/admin/catalog/categories → 201
func (h *AdminCatalogHandler) CreateCategory(c fiber.Ctx) error {
	var req createCategoryRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	enabled := req.Enabled == nil || *req.Enabled
	cat, err := h.admin.CreateCategory(c.Context(), actorID(c), domain.Category{
		Vertical: domain.Vertical(req.Vertical), ParentID: req.ParentID, Slug: req.Slug, Name: req.Name,
		Description: req.Description, Icon: req.Icon, SortOrder: req.SortOrder, AttributesSchema: req.AttributesSchema,
		RiskLevel: domain.RiskLevel(req.RiskLevel), Prohibited: req.Prohibited, Enabled: enabled,
	}, c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toAdminCategory(cat))
}

// PATCH /api/v1/admin/catalog/categories/:id
func (h *AdminCatalogHandler) UpdateCategory(c fiber.Ctx) error {
	var req updateCategoryRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	cat, err := h.admin.UpdateCategory(c.Context(), actorID(c), c.Params("id"), service.CategoryPatch{
		ParentID: req.ParentID, Slug: req.Slug, Name: req.Name, Description: req.Description, Icon: req.Icon,
		SortOrder: req.SortOrder, AttributesSchema: req.AttributesSchema, RiskLevel: req.RiskLevel,
		Prohibited: req.Prohibited, Enabled: req.Enabled,
	}, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toAdminCategory(cat))
}

// PUT /api/v1/admin/catalog/categories/:id/cities/:city → 204. {"enabled": null} vuelve al valor global.
func (h *AdminCatalogHandler) SetCategoryCityScope(c fiber.Ctx) error {
	var req enabledRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if err := h.admin.SetCategoryCityScope(c.Context(), actorID(c), c.Params("id"), c.Params("city"), req.Enabled, c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// adminCityResponse: el admin ve también las ciudades apagadas.
type adminCityResponse struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Region  string `json:"region"`
	Enabled bool   `json:"enabled"`
}

func toAdminCity(c domain.City) adminCityResponse {
	return adminCityResponse{ID: c.ID, Slug: c.Slug, Name: c.Name, Region: c.Region, Enabled: c.Enabled}
}

// GET /api/v1/admin/cities
func (h *AdminCatalogHandler) Cities(c fiber.Ctx) error {
	cities, err := h.admin.Cities(c.Context())
	if err != nil {
		return err
	}
	out := make([]adminCityResponse, len(cities))
	for i, city := range cities {
		out[i] = toAdminCity(city)
	}
	return c.JSON(fiber.Map{"cities": out})
}

// PATCH /api/v1/admin/cities/:slug {"enabled": true|false}
func (h *AdminCatalogHandler) SetCityEnabled(c fiber.Ctx) error {
	var req enabledRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Enabled == nil {
		return badRequest("Indica enabled: true o false.")
	}
	city, err := h.admin.SetCityEnabled(c.Context(), actorID(c), c.Params("slug"), *req.Enabled, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toAdminCity(city))
}

// GET /api/v1/admin/settings
func (h *AdminCatalogHandler) Settings(c fiber.Ctx) error {
	list, err := h.admin.Settings(c.Context())
	if err != nil {
		return err
	}
	out := make([]settingResponse, len(list))
	for i, s := range list {
		out[i] = toSettingResponse(s)
	}
	return c.JSON(fiber.Map{"settings": out})
}

// PUT /api/v1/admin/settings — crea o cambia el valor de una clave en su alcance.
func (h *AdminCatalogHandler) SetSetting(c fiber.Ctx) error {
	var req setSettingRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	s, err := h.admin.SetSetting(c.Context(), actorID(c), service.SettingInput{
		Key: req.Key, CitySlug: req.City, CategoryID: req.CategoryID, Value: req.Value, Description: req.Description,
	}, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toSettingResponse(s))
}

// GET /api/v1/admin/settings/:key/history?limit=50
func (h *AdminCatalogHandler) SettingHistory(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit"))
	changes, err := h.admin.SettingHistory(c.Context(), c.Params("key"), limit)
	if err != nil {
		return err
	}
	out := make([]settingChangeResponse, len(changes))
	for i, ch := range changes {
		out[i] = settingChangeResponse{At: ch.At, ActorID: ch.ActorID, IP: ch.IP, Before: ch.Before, After: ch.After}
	}
	return c.JSON(fiber.Map{"history": out})
}
