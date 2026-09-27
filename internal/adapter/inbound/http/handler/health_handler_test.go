package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type stubHealth struct{ report domain.HealthReport }

func (s stubHealth) Check(context.Context) domain.HealthReport { return s.report }

func TestHealthHandlerGet(t *testing.T) {
	tests := []struct {
		name       string
		report     domain.HealthReport
		wantStatus int
	}{
		{
			name:       "todo arriba responde 200",
			report:     domain.HealthReport{Status: domain.HealthUp, Components: map[string]domain.HealthStatus{"postgres": domain.HealthUp}},
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "una dependencia caída responde 503",
			report:     domain.HealthReport{Status: domain.HealthDown, Components: map[string]domain.HealthStatus{"redis": domain.HealthDown}},
			wantStatus: fiber.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/health", NewHealthHandler(stubHealth{tt.report}).Get)

			res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/health", nil))
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, se esperaba %d", res.StatusCode, tt.wantStatus)
			}

			var body domain.HealthReport
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Status != tt.report.Status {
				t.Fatalf("body.status = %s, se esperaba %s", body.Status, tt.report.Status)
			}
		})
	}
}
