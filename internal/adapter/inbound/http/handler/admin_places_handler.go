package handler

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// Ciudades y distritos del admin: /api/v1/admin/cities y /admin/cities/:slug/zones.

// adminCityResponse: el admin ve también las ciudades apagadas.
type adminCityResponse struct {
	ID      string   `json:"id"`
	Slug    string   `json:"slug"`
	Name    string   `json:"name"`
	Region  string   `json:"region"`
	Ubigeo  string   `json:"ubigeo,omitempty"`
	Center  geoPoint `json:"center"`
	Enabled bool     `json:"enabled"`
}

func toAdminCity(c domain.City) adminCityResponse {
	return adminCityResponse{ID: c.ID, Slug: c.Slug, Name: c.Name, Region: c.Region, Ubigeo: c.Ubigeo,
		Center: toGeoPoint(c.Center), Enabled: c.Enabled}
}

type createCityRequest struct {
	Slug   string   `json:"slug"`
	Name   string   `json:"name"`
	Region string   `json:"region"`
	Ubigeo string   `json:"ubigeo"`
	Center geoPoint `json:"center"`
}

type updateCityRequest struct {
	Name    *string   `json:"name"`
	Region  *string   `json:"region"`
	Center  *geoPoint `json:"center"`
	Enabled *bool     `json:"enabled"`
}

// adminZoneResponse lleva el límite simplificado (GeoJSON) solo en la lista.
type adminZoneResponse struct {
	ID          string          `json:"id"`
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Ubigeo      string          `json:"ubigeo,omitempty"`
	SortOrder   int             `json:"sort_order"`
	Enabled     bool            `json:"enabled"`
	HasBoundary bool            `json:"has_boundary"`
	Boundary    json.RawMessage `json:"boundary,omitempty"`
}

func toAdminZone(z domain.Zone) adminZoneResponse {
	return adminZoneResponse{ID: z.ID, Slug: z.Slug, Name: z.Name, Ubigeo: z.Ubigeo, SortOrder: z.SortOrder,
		Enabled: z.Enabled, HasBoundary: z.HasBoundary, Boundary: z.Boundary}
}

// createZoneRequest: boundary es GeoJSON (Polygon, MultiPolygon, Feature o FeatureCollection de uno).
type createZoneRequest struct {
	Slug      string          `json:"slug"`
	Name      string          `json:"name"`
	Ubigeo    string          `json:"ubigeo"`
	SortOrder int             `json:"sort_order"`
	Enabled   *bool           `json:"enabled"` // por defecto, activo
	Boundary  json.RawMessage `json:"boundary"`
}

type updateZoneRequest struct {
	Name      *string         `json:"name"`
	SortOrder *int            `json:"sort_order"`
	Enabled   *bool           `json:"enabled"`
	Boundary  json.RawMessage `json:"boundary"`
}

// nullJSON: un campo "boundary": null llega como el literal null; se trata como ausente.
func nullJSON(raw json.RawMessage) json.RawMessage {
	if string(raw) == "null" {
		return nil
	}
	return raw
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

// POST /api/v1/admin/cities → 201. La ciudad nace apagada.
func (h *AdminCatalogHandler) CreateCity(c fiber.Ctx) error {
	var req createCityRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	city, err := h.admin.CreateCity(c.Context(), actorID(c), domain.City{
		Slug: req.Slug, Name: req.Name, Region: req.Region, Ubigeo: req.Ubigeo, Center: req.Center.domain(),
	}, c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toAdminCity(city))
}

// PATCH /api/v1/admin/cities/:slug {"enabled": true} o nombre, región y centro.
func (h *AdminCatalogHandler) UpdateCity(c fiber.Ctx) error {
	var req updateCityRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	patch := service.CityPatch{Name: req.Name, Region: req.Region, Enabled: req.Enabled}
	if req.Center != nil {
		center := req.Center.domain()
		patch.Center = &center
	}
	city, err := h.admin.UpdateCity(c.Context(), actorID(c), c.Params("slug"), patch, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toAdminCity(city))
}

// GET /api/v1/admin/cities/:slug/zones
func (h *AdminCatalogHandler) Zones(c fiber.Ctx) error {
	city, zones, err := h.admin.Zones(c.Context(), c.Params("slug"))
	if err != nil {
		return err
	}
	out := make([]adminZoneResponse, len(zones))
	for i, z := range zones {
		out[i] = toAdminZone(z)
	}
	return c.JSON(fiber.Map{"city": toAdminCity(city), "zones": out})
}

// POST /api/v1/admin/cities/:slug/zones → 201
func (h *AdminCatalogHandler) CreateZone(c fiber.Ctx) error {
	var req createZoneRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	zone, err := h.admin.CreateZone(c.Context(), actorID(c), c.Params("slug"), domain.Zone{
		Slug: req.Slug, Name: req.Name, Ubigeo: req.Ubigeo, SortOrder: req.SortOrder,
		Enabled: req.Enabled == nil || *req.Enabled, Boundary: nullJSON(req.Boundary),
	}, c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toAdminZone(zone))
}

// PATCH /api/v1/admin/cities/:slug/zones/:zone
func (h *AdminCatalogHandler) UpdateZone(c fiber.Ctx) error {
	var req updateZoneRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	zone, err := h.admin.UpdateZone(c.Context(), actorID(c), c.Params("slug"), c.Params("zone"), service.ZonePatch{
		Name: req.Name, SortOrder: req.SortOrder, Enabled: req.Enabled, Boundary: nullJSON(req.Boundary),
	}, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toAdminZone(zone))
}
