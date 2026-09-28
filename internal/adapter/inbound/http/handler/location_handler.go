package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// UserLocation es lo que el handler necesita de service.UserLocationService.
type UserLocation interface {
	Get(ctx context.Context, userID string) (domain.Location, bool, error)
	Set(ctx context.Context, userID, citySlug, zoneSlug, ip string) (domain.Location, error)
}

// LocationHandler atiende /api/v1/me/location: la ciudad y el distrito del usuario conectado.
type LocationHandler struct {
	location UserLocation
}

func NewLocationHandler(location UserLocation) *LocationHandler {
	return &LocationHandler{location: location}
}

type setLocationRequest struct {
	City string `json:"city"`
	Zone string `json:"zone"`
}

type locationResponse struct {
	City cityResponse `json:"city"`
	Zone zoneResponse `json:"zone"`
}

func toLocationResponse(l domain.Location) locationResponse {
	return locationResponse{City: toCityResponse(l.City), Zone: toZoneResponse(l.Zone)}
}

// GET /api/v1/me/location → {"location": {...}} o {"location": null} si aún no eligió.
func (h *LocationHandler) Get(c fiber.Ctx) error {
	loc, ok, err := h.location.Get(c.Context(), actorID(c))
	if err != nil {
		return err
	}
	if !ok {
		return c.JSON(fiber.Map{"location": nil})
	}
	return c.JSON(fiber.Map{"location": toLocationResponse(loc)})
}

// PUT /api/v1/me/location {"city": "ayacucho", "zone": "carmen-alto"}
func (h *LocationHandler) Set(c fiber.Ctx) error {
	var req setLocationRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.City == "" || req.Zone == "" {
		return badRequest("Indica la ciudad y el distrito.")
	}
	loc, err := h.location.Set(c.Context(), actorID(c), req.City, req.Zone, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"location": toLocationResponse(loc)})
}
