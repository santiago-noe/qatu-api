package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type stubCatalog struct{}

func (stubCatalog) Categories(_ context.Context, v domain.Vertical, city string) ([]domain.Category, error) {
	if city == "cusco" {
		return nil, domain.ErrNotFound
	}
	return []domain.Category{{ID: "c1", Slug: "construccion", Name: "Construcción", RiskLevel: domain.RiskMedium,
		AttributesSchema: json.RawMessage(`{"type":"object"}`),
		Children:         []domain.Category{{ID: "t1", Slug: "motosierra", RiskLevel: domain.RiskHigh}}}}, nil
}

func (stubCatalog) Cities(context.Context) ([]domain.City, error) { return nil, nil }

func (stubCatalog) Zones(context.Context, string) ([]domain.Zone, error) { return nil, nil }

func (stubCatalog) LocationAt(_ context.Context, p domain.GeoPoint) (domain.Location, error) {
	if err := p.Validate(); err != nil {
		return domain.Location{}, err
	}
	if p.Lat > -13 {
		return domain.Location{}, domain.ErrOutOfCoverage
	}
	return domain.Location{City: domain.City{Slug: "ayacucho"}, Zone: domain.Zone{Slug: "ayacucho", HasBoundary: true}}, nil
}

func newCatalogApp() *fiber.App {
	h := NewCatalogHandler(stubCatalog{})
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Get("/catalog/categories", h.Categories)
	app.Get("/geo/zone", h.ZoneAt)
	return app
}

func TestCatalogHandlerErrors(t *testing.T) {
	app := newCatalogApp()
	for _, tt := range []struct {
		path, code string
		status     int
	}{
		{"/catalog/categories", "vertical_invalida", fiber.StatusBadRequest},
		{"/catalog/categories?vertical=vehicles", "vertical_invalida", fiber.StatusBadRequest},
		{"/catalog/categories?vertical=rental&city=cusco", "no_encontrado", fiber.StatusNotFound},
		{"/geo/zone?lat=abc&lng=-74.2", "ubicacion_invalida", fiber.StatusBadRequest},
		{"/geo/zone?lat=-95&lng=-74.2", "ubicacion_invalida", fiber.StatusBadRequest},
		{"/geo/zone?lat=-12.04&lng=-77.04", "fuera_de_cobertura", fiber.StatusNotFound},
	} {
		t.Run(tt.path, func(t *testing.T) {
			res, err := app.Test(httptest.NewRequest(fiber.MethodGet, tt.path, nil))
			if err != nil {
				t.Fatal(err)
			}
			var body APIError
			_ = json.NewDecoder(res.Body).Decode(&body)
			if res.StatusCode != tt.status || body.Code != tt.code {
				t.Fatalf("status %d código %q, se esperaba %d %q", res.StatusCode, body.Code, tt.status, tt.code)
			}
		})
	}
}

func TestCatalogHandlerCategories(t *testing.T) {
	res, err := newCatalogApp().Test(httptest.NewRequest(fiber.MethodGet, "/catalog/categories?vertical=rental&city=ayacucho", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != fiber.StatusOK || res.Header.Get(fiber.HeaderCacheControl) != publicMaxAge {
		t.Fatalf("status %d, Cache-Control %q", res.StatusCode, res.Header.Get(fiber.HeaderCacheControl))
	}
	raw, _ := io.ReadAll(res.Body)
	var body struct {
		Categories []struct {
			Slug             string          `json:"slug"`
			AttributesSchema json.RawMessage `json:"attributes_schema"`
			Children         []struct {
				Slug      string `json:"slug"`
				RiskLevel string `json:"risk_level"`
			} `json:"children"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	root := body.Categories[0]
	if root.Slug != "construccion" || string(root.AttributesSchema) != `{"type":"object"}` ||
		root.Children[0].Slug != "motosierra" || root.Children[0].RiskLevel != "high" {
		t.Fatalf("respuesta = %s", raw)
	}
}

func TestCatalogHandlerZoneAt(t *testing.T) {
	res, _ := newCatalogApp().Test(httptest.NewRequest(fiber.MethodGet, "/geo/zone?lat=-13.1631&lng=-74.2237", nil))
	var body struct {
		City struct{ Slug string } `json:"city"`
		Zone struct {
			Slug       string `json:"slug"`
			Detectable bool   `json:"detectable"`
		} `json:"zone"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != fiber.StatusOK || body.City.Slug != "ayacucho" || body.Zone.Slug != "ayacucho" || !body.Zone.Detectable {
		t.Fatalf("status %d, %+v", res.StatusCode, body)
	}
}
