// Package handler contiene los handlers HTTP, uno por feature.
package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// HealthChecker es lo que el handler necesita del caso de uso.
type HealthChecker interface {
	Check(ctx context.Context) domain.HealthReport
}

type HealthHandler struct {
	svc HealthChecker
}

func NewHealthHandler(svc HealthChecker) *HealthHandler {
	return &HealthHandler{svc: svc}
}

// Get responde 200 si todas las dependencias están arriba y 503 si alguna falla.
func (h *HealthHandler) Get(c fiber.Ctx) error {
	report := h.svc.Check(c.Context())
	status := fiber.StatusOK
	if !report.IsUp() {
		status = fiber.StatusServiceUnavailable
	}
	return c.Status(status).JSON(report)
}
