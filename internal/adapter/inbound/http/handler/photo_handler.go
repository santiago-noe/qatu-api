package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// Photos es lo que el handler necesita de service.PhotoService.
type Photos interface {
	RequestUpload(ctx context.Context, ownerID, listingID string, kind domain.PhotoKind, contentType string, size int64) (domain.ListingPhoto, port.PresignedUpload, error)
	CompleteUpload(ctx context.Context, ownerID, listingID, photoID string) (domain.ListingPhoto, error)
	List(ctx context.Context, ownerID, listingID string) ([]service.PhotoView, error)
	Delete(ctx context.Context, ownerID, listingID, photoID string) error
	Reorder(ctx context.Context, ownerID, listingID string, ids []string) ([]service.PhotoView, error)
}

// PhotoHandler atiende /api/v1/me/listings/:id/photos (feature 003).
type PhotoHandler struct {
	photos Photos
}

func NewPhotoHandler(photos Photos) *PhotoHandler { return &PhotoHandler{photos: photos} }

type photoResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	SortOrder int    `json:"sort_order"`
	// URLs por ancho ("320", "800", "1600"); vacío mientras se procesa. La de la placa vence en minutos.
	URLs map[string]string `json:"urls"`
}

func toPhotoResponse(p domain.ListingPhoto, urls map[int]string) photoResponse {
	out := photoResponse{ID: p.ID, Kind: string(p.Kind), Status: string(p.Status), Width: p.Width, Height: p.Height,
		SortOrder: p.SortOrder, URLs: make(map[string]string, len(urls))}
	for width, u := range urls {
		out.URLs[strconv.Itoa(width)] = u
	}
	return out
}

func toPhotoResponses(views []service.PhotoView) []photoResponse {
	out := make([]photoResponse, len(views))
	for i, v := range views {
		out[i] = toPhotoResponse(v.Photo, v.URLs)
	}
	return out
}

type uploadRequest struct {
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type uploadResponse struct {
	Photo  photoResponse `json:"photo"`
	Upload struct {
		Method    string            `json:"method"`
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		ExpiresAt time.Time         `json:"expires_at"`
	} `json:"upload"`
}

type reorderRequest struct {
	IDs []string `json:"ids"`
}

// GET /api/v1/me/listings/:id/photos
func (h *PhotoHandler) List(c fiber.Ctx) error {
	views, err := h.photos.List(c.Context(), actorID(c), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"photos": toPhotoResponses(views)})
}

// POST /api/v1/me/listings/:id/photos → 201 con la URL firmada: el navegador hace el PUT directo
// al almacenamiento con esas cabeceras y luego llama a .../complete.
func (h *PhotoHandler) RequestUpload(c fiber.Ctx) error {
	var req uploadRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	p, up, err := h.photos.RequestUpload(c.Context(), actorID(c), c.Params("id"), domain.PhotoKind(req.Kind), req.ContentType, req.Size)
	if err != nil {
		return err
	}
	var out uploadResponse
	out.Photo = toPhotoResponse(p, nil)
	out.Upload.Method, out.Upload.URL, out.Upload.Headers, out.Upload.ExpiresAt = fiber.MethodPut, up.URL, up.Headers, up.ExpiresAt
	return c.Status(fiber.StatusCreated).JSON(out)
}

// POST /api/v1/me/listings/:id/photos/:photo/complete → 202: se procesa en segundo plano.
func (h *PhotoHandler) CompleteUpload(c fiber.Ctx) error {
	p, err := h.photos.CompleteUpload(c.Context(), actorID(c), c.Params("id"), c.Params("photo"))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(toPhotoResponse(p, nil))
}

// DELETE /api/v1/me/listings/:id/photos/:photo → 204
func (h *PhotoHandler) Delete(c fiber.Ctx) error {
	if err := h.photos.Delete(c.Context(), actorID(c), c.Params("id"), c.Params("photo")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// PUT /api/v1/me/listings/:id/photos/order {"ids": [...]}: la primera es la portada.
func (h *PhotoHandler) Reorder(c fiber.Ctx) error {
	var req reorderRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	views, err := h.photos.Reorder(c.Context(), actorID(c), c.Params("id"), req.IDs)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"photos": toPhotoResponses(views)})
}
