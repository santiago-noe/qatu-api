package handler

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Catalog es lo que el handler necesita de service.CatalogService.
type Catalog interface {
	Categories(ctx context.Context, vertical domain.Vertical, citySlug string) ([]domain.Category, error)
	Cities(ctx context.Context) ([]domain.City, error)
	Zones(ctx context.Context, citySlug string) ([]domain.Zone, error)
	LocationAt(ctx context.Context, p domain.GeoPoint) (domain.Location, error)
}

// CatalogHandler atiende el catálogo público (sin sesión): categorías, oficios, ciudades y zonas.
type CatalogHandler struct {
	catalog Catalog
}

func NewCatalogHandler(catalog Catalog) *CatalogHandler { return &CatalogHandler{catalog: catalog} }

// publicMaxAge: el BFF y el navegador pueden reutilizar la respuesta unos minutos.
const publicMaxAge = "public, max-age=300"

type categoryResponse struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	RiskLevel   string `json:"risk_level"`
	// JSON Schema de los atributos: la web arma con él el formulario de la publicación.
	AttributesSchema json.RawMessage    `json:"attributes_schema,omitempty"`
	Children         []categoryResponse `json:"children,omitempty"`
}

func toCategoryResponses(list []domain.Category) []categoryResponse {
	out := make([]categoryResponse, len(list))
	for i, c := range list {
		out[i] = categoryResponse{
			ID: c.ID, Slug: c.Slug, Name: c.Name, Description: c.Description, Icon: c.Icon,
			RiskLevel: string(c.RiskLevel), AttributesSchema: c.AttributesSchema, Children: toCategoryResponses(c.Children),
		}
	}
	return out
}

// geoPoint es un punto en JSON, al responder y al recibir.
type geoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func toGeoPoint(p domain.GeoPoint) geoPoint { return geoPoint{Lat: p.Lat, Lng: p.Lng} }

func (p geoPoint) domain() domain.GeoPoint { return domain.GeoPoint{Lat: p.Lat, Lng: p.Lng} }

type cityResponse struct {
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	Region   string   `json:"region"`
	Timezone string   `json:"timezone"`
	Center   geoPoint `json:"center"`
}

func toCityResponse(c domain.City) cityResponse {
	return cityResponse{Slug: c.Slug, Name: c.Name, Region: c.Region, Timezone: c.Timezone,
		Center: toGeoPoint(c.Center)}
}

type zoneResponse struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Ubigeo string `json:"ubigeo,omitempty"`
	// Detectable: la zona tiene límite y se puede detectar por la ubicación del navegador.
	Detectable bool `json:"detectable"`
}

func toZoneResponse(z domain.Zone) zoneResponse {
	return zoneResponse{ID: z.ID, Slug: z.Slug, Name: z.Name, Ubigeo: z.Ubigeo, Detectable: z.HasBoundary}
}

// GET /api/v1/catalog/categories?vertical=rental|service[&city=ayacucho]
func (h *CatalogHandler) Categories(c fiber.Ctx) error {
	vertical, ok := domain.ParseVertical(c.Query("vertical"))
	if !ok {
		return domain.ErrInvalidVertical
	}
	tree, err := h.catalog.Categories(c.Context(), vertical, c.Query("city"))
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, publicMaxAge)
	return c.JSON(fiber.Map{"categories": toCategoryResponses(tree)})
}

// GET /api/v1/cities
func (h *CatalogHandler) Cities(c fiber.Ctx) error {
	cities, err := h.catalog.Cities(c.Context())
	if err != nil {
		return err
	}
	out := make([]cityResponse, len(cities))
	for i, city := range cities {
		out[i] = toCityResponse(city)
	}
	c.Set(fiber.HeaderCacheControl, publicMaxAge)
	return c.JSON(fiber.Map{"cities": out})
}

// GET /api/v1/cities/:slug/zones
func (h *CatalogHandler) Zones(c fiber.Ctx) error {
	zones, err := h.catalog.Zones(c.Context(), c.Params("slug"))
	if err != nil {
		return err
	}
	out := make([]zoneResponse, len(zones))
	for i, z := range zones {
		out[i] = toZoneResponse(z)
	}
	c.Set(fiber.HeaderCacheControl, publicMaxAge)
	return c.JSON(fiber.Map{"zones": out})
}

// GET /api/v1/geo/zone?lat=-13.16&lng=-74.22 → la ciudad y el distrito del punto.
// No se guarda ni se registra el punto: solo se usa para responder.
func (h *CatalogHandler) ZoneAt(c fiber.Ctx) error {
	lat, errLat := strconv.ParseFloat(c.Query("lat"), 64)
	lng, errLng := strconv.ParseFloat(c.Query("lng"), 64)
	if errLat != nil || errLng != nil {
		return domain.ErrInvalidLocation
	}
	loc, err := h.catalog.LocationAt(c.Context(), domain.GeoPoint{Lat: lat, Lng: lng})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"city": toCityResponse(loc.City), "zone": toZoneResponse(loc.Zone)})
}
