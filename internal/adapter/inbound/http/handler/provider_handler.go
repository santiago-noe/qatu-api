package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// Providers es lo que el handler necesita de service.ProviderService.
type Providers interface {
	Get(ctx context.Context, userID string) (service.ProviderView, bool, error)
	Activate(ctx context.Context, userID string, in service.ProviderInput, ip string) (service.ProviderView, error)
	Update(ctx context.Context, userID string, version int, in service.ProviderInput, ip string) (service.ProviderView, error)
	Submit(ctx context.Context, userID string, version int, ip string) (service.ProviderView, error)
	Pause(ctx context.Context, userID string, version int, ip string) (service.ProviderView, error)
	Resume(ctx context.Context, userID string, version int, ip string) (service.ProviderView, error)
	Calendar(ctx context.Context, userID string, from, to time.Time) ([]domain.AvailabilityBlock, error)
	BlockDates(ctx context.Context, userID string, b domain.AvailabilityBlock, ip string) (domain.AvailabilityBlock, error)
	UnblockDates(ctx context.Context, userID, blockID, ip string) error
}

// ProviderHandler atiende /api/v1/me/provider (feature 004): el perfil de proveedor de la persona.
// El celular solo sale aquí, a su dueño.
type ProviderHandler struct {
	providers Providers
}

func NewProviderHandler(providers Providers) *ProviderHandler {
	return &ProviderHandler{providers: providers}
}

type packageJSON struct {
	ID              string       `json:"id,omitempty"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Price           domain.Cents `json:"price"`
	DurationMinutes int          `json:"duration_minutes"`
}

type tradeJSON struct {
	CategoryID string        `json:"category_id"`
	HourlyRate domain.Cents  `json:"hourly_rate"`
	MinHours   int           `json:"min_hours"`
	Packages   []packageJSON `json:"packages"`
	// QuoteOnly solo sale en la respuesta: sin tarifa ni paquetes, el oficio es a cotizar.
	QuoteOnly bool `json:"quote_only"`
}

// slotJSON es una franja del horario en la hora local de la ciudad: "08:00" a "17:30".
type slotJSON struct {
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// providerFields es el formulario del editor (entrada); la ciudad va por slug.
type providerFields struct {
	BusinessName    string      `json:"business_name"`
	Phone           string      `json:"phone"`
	City            string      `json:"city"`
	Bio             string      `json:"bio"`
	YearsExperience int         `json:"years_experience"`
	WarrantyDays    *int        `json:"warranty_days"`
	AcceptsUrgent   bool        `json:"accepts_urgent"`
	Trades          []tradeJSON `json:"trades"`
	CoverageZoneIDs []string    `json:"coverage_zone_ids"`
	Weekly          []slotJSON  `json:"weekly"`
}

func (f providerFields) input() (service.ProviderInput, error) {
	in := service.ProviderInput{
		BusinessName: f.BusinessName, Phone: f.Phone, CitySlug: f.City, Bio: f.Bio,
		YearsExperience: f.YearsExperience, WarrantyDays: f.WarrantyDays, AcceptsUrgent: f.AcceptsUrgent,
		CoverageZoneIDs: f.CoverageZoneIDs, Trades: make([]domain.ProviderTrade, len(f.Trades)),
		Weekly: make([]domain.WeeklySlot, len(f.Weekly)),
	}
	for i, t := range f.Trades {
		trade := domain.ProviderTrade{CategoryID: t.CategoryID, HourlyRate: t.HourlyRate, MinHours: t.MinHours,
			Packages: make([]domain.ServicePackage, len(t.Packages))}
		for j, p := range t.Packages {
			trade.Packages[j] = domain.ServicePackage{ID: p.ID, Title: p.Title, Description: p.Description,
				Price: p.Price, DurationMinutes: p.DurationMinutes}
		}
		in.Trades[i] = trade
	}
	for i, s := range f.Weekly {
		start, err := domain.ParseClock(s.Start)
		if err != nil {
			return service.ProviderInput{}, err
		}
		end, err := domain.ParseClock(s.End)
		if err != nil {
			return service.ProviderInput{}, err
		}
		in.Weekly[i] = domain.WeeklySlot{Weekday: s.Weekday, Start: start, End: end}
	}
	return in, nil
}

type activateProviderRequest struct {
	providerFields
	AcceptTerms bool `json:"accept_terms"`
}

type updateProviderRequest struct {
	providerFields
	Version int `json:"version"`
}

type providerResponse struct {
	BusinessName     string      `json:"business_name,omitempty"`
	Phone            string      `json:"phone"`
	City             placeRef    `json:"city"`
	Bio              string      `json:"bio"`
	YearsExperience  int         `json:"years_experience"`
	WarrantyDays     int         `json:"warranty_days"`
	AcceptsUrgent    bool        `json:"accepts_urgent"`
	Trades           []tradeJSON `json:"trades"`
	CoverageZoneIDs  []string    `json:"coverage_zone_ids"`
	Weekly           []slotJSON  `json:"weekly"`
	Status           string      `json:"status"`
	RejectionReason  string      `json:"rejection_reason,omitempty"`
	VerifiedAt       *time.Time  `json:"verified_at,omitempty"`
	FirstPublishedAt *time.Time  `json:"first_published_at,omitempty"`
	Version          int         `json:"version"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

func toProviderResponse(v service.ProviderView) providerResponse {
	p := v.Profile
	out := providerResponse{
		BusinessName: p.BusinessName, Phone: p.Phone, City: placeRef{ID: v.City.ID, Slug: v.City.Slug, Name: v.City.Name},
		Bio: p.Bio, YearsExperience: p.YearsExperience, WarrantyDays: p.WarrantyDays, AcceptsUrgent: p.AcceptsUrgent,
		Trades: make([]tradeJSON, len(p.Trades)), CoverageZoneIDs: nonNil(p.CoverageZoneIDs),
		Weekly: make([]slotJSON, len(p.Weekly)), Status: string(p.Status), RejectionReason: p.RejectionReason,
		VerifiedAt: p.VerifiedAt, FirstPublishedAt: p.FirstPublishedAt, Version: p.Version,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	for i, t := range p.Trades {
		trade := tradeJSON{CategoryID: t.CategoryID, HourlyRate: t.HourlyRate, MinHours: t.MinHours,
			Packages: make([]packageJSON, len(t.Packages)), QuoteOnly: t.QuoteOnly()}
		for j, pkg := range t.Packages {
			trade.Packages[j] = packageJSON{ID: pkg.ID, Title: pkg.Title, Description: pkg.Description,
				Price: pkg.Price, DurationMinutes: pkg.DurationMinutes}
		}
		out.Trades[i] = trade
	}
	for i, s := range p.Weekly {
		out.Weekly[i] = slotJSON{Weekday: s.Weekday, Start: domain.FormatClock(s.Start), End: domain.FormatClock(s.End)}
	}
	return out
}

func sendProvider(c fiber.Ctx, status int, v service.ProviderView) error {
	return c.Status(status).JSON(fiber.Map{"provider": toProviderResponse(v)})
}

// GET /api/v1/me/provider → {"provider": {...}} o {"provider": null} si aún no es proveedor.
func (h *ProviderHandler) Get(c fiber.Ctx) error {
	view, ok, err := h.providers.Get(c.Context(), actorID(c))
	if err != nil {
		return err
	}
	if !ok {
		return c.JSON(fiber.Map{"provider": nil})
	}
	return sendProvider(c, fiber.StatusOK, view)
}

// POST /api/v1/me/provider → 201: activa el perfil en borrador (con accept_terms).
func (h *ProviderHandler) Activate(c fiber.Ctx) error {
	var req activateProviderRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	in, err := req.input()
	if err != nil {
		return err
	}
	in.AcceptTerms = req.AcceptTerms
	view, err := h.providers.Activate(c.Context(), actorID(c), in, c.IP())
	if err != nil {
		return err
	}
	return sendProvider(c, fiber.StatusCreated, view)
}

// PUT /api/v1/me/provider — el formulario completo con la versión leída (bloqueo optimista).
func (h *ProviderHandler) Update(c fiber.Ctx) error {
	var req updateProviderRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	in, err := req.input()
	if err != nil {
		return err
	}
	view, err := h.providers.Update(c.Context(), actorID(c), req.Version, in, c.IP())
	if err != nil {
		return err
	}
	return sendProvider(c, fiber.StatusOK, view)
}

// runAction atiende POST /me/provider/{submit,pause,resume} {"version": n}.
func (h *ProviderHandler) runAction(c fiber.Ctx,
	do func(ctx context.Context, userID string, version int, ip string) (service.ProviderView, error)) error {
	var req versionRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	view, err := do(c.Context(), actorID(c), req.Version, c.IP())
	if err != nil {
		return err
	}
	return sendProvider(c, fiber.StatusOK, view)
}

func (h *ProviderHandler) Submit(c fiber.Ctx) error { return h.runAction(c, h.providers.Submit) }
func (h *ProviderHandler) Pause(c fiber.Ctx) error  { return h.runAction(c, h.providers.Pause) }
func (h *ProviderHandler) Resume(c fiber.Ctx) error { return h.runAction(c, h.providers.Resume) }

// GET /api/v1/me/provider/availability?from=…&to=…
func (h *ProviderHandler) Calendar(c fiber.Ctx) error {
	from, to, err := calendarWindow(c)
	if err != nil {
		return err
	}
	blocks, err := h.providers.Calendar(c.Context(), actorID(c), from, to)
	if err != nil {
		return err
	}
	return sendBlocks(c, blocks)
}

// POST /api/v1/me/provider/availability → 201 {"start", "end", "note"}; el fin no se incluye.
func (h *ProviderHandler) BlockDates(c fiber.Ctx) error {
	block, err := bindBlock(c)
	if err != nil {
		return err
	}
	b, err := h.providers.BlockDates(c.Context(), actorID(c), block, c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toBlockResponse(b))
}

// DELETE /api/v1/me/provider/availability/:block → 204
func (h *ProviderHandler) UnblockDates(c fiber.Ctx) error {
	if err := h.providers.UnblockDates(c.Context(), actorID(c), c.Params("block"), c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
