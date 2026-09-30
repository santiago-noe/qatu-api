package handler

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// Moderation es lo que el handler necesita de service.ModerationService.
type Moderation interface {
	Queue(ctx context.Context) ([]service.ReviewView, error)
	Approve(ctx context.Context, moderatorID, listingID string, version int, ip string) (domain.ToolListing, error)
	Reject(ctx context.Context, moderatorID, listingID string, version int, reason, ip string) (domain.ToolListing, error)
}

// ModerationHandler atiende /api/v1/moderation (roles moderator y admin, con segundo paso).
type ModerationHandler struct {
	moderation Moderation
}

func NewModerationHandler(moderation Moderation) *ModerationHandler {
	return &ModerationHandler{moderation: moderation}
}

type reviewCategory struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	RiskLevel        string          `json:"risk_level"`
	AttributesSchema json.RawMessage `json:"attributes_schema"`
}

type reviewResponse struct {
	Listing  listingResponse `json:"listing"`
	Owner    reviewOwner     `json:"owner"`
	Category reviewCategory  `json:"category"`
	Photos   []photoResponse `json:"photos"`
}

type reviewOwner struct {
	Name         string `json:"name"`
	FirstListing bool   `json:"first_listing"`
}

type rejectRequest struct {
	Version int    `json:"version"`
	Reason  string `json:"reason"`
}

// GET /api/v1/moderation/listings — la cola, la que más espera primero.
func (h *ModerationHandler) Queue(c fiber.Ctx) error {
	queue, err := h.moderation.Queue(c.Context())
	if err != nil {
		return err
	}
	out := make([]reviewResponse, len(queue))
	for i, v := range queue {
		out[i] = reviewResponse{
			Listing: toListingResponse(v.Item.Listing),
			Owner:   reviewOwner{Name: v.Item.OwnerName, FirstListing: v.Item.FirstListing},
			Category: reviewCategory{ID: v.Category.ID, Name: v.Category.Name, RiskLevel: string(v.Category.RiskLevel),
				AttributesSchema: v.Category.AttributesSchema},
			Photos: toPhotoResponses(v.Photos),
		}
	}
	return c.JSON(fiber.Map{"listings": out})
}

// POST /api/v1/moderation/listings/:id/approve {"version": n}
func (h *ModerationHandler) Approve(c fiber.Ctx) error {
	var req versionRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	l, err := h.moderation.Approve(c.Context(), actorID(c), c.Params("id"), req.Version, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toListingResponse(l))
}

// POST /api/v1/moderation/listings/:id/reject {"version": n, "reason": "..."}
func (h *ModerationHandler) Reject(c fiber.Ctx) error {
	var req rejectRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	l, err := h.moderation.Reject(c.Context(), actorID(c), c.Params("id"), req.Version, req.Reason, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toListingResponse(l))
}
