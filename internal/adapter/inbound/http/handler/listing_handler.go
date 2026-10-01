package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/service"
)

// Lenders es lo que el handler necesita de service.LenderService.
type Lenders interface {
	Get(ctx context.Context, userID string) (service.LenderView, bool, error)
	Save(ctx context.Context, userID string, in service.LenderInput, ip string) (service.LenderView, error)
}

// Listings es lo que el handler necesita de service.ListingService.
type Listings interface {
	Create(ctx context.Context, ownerID string, in service.ListingInput, ip string) (domain.ToolListing, error)
	Get(ctx context.Context, ownerID, id string) (domain.ToolListing, error)
	List(ctx context.Context, ownerID string) ([]domain.ToolListing, error)
	Update(ctx context.Context, ownerID, id string, version int, in service.ListingInput, ip string) (domain.ToolListing, error)
	Submit(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error)
	Pause(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error)
	Resume(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error)
	Archive(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error)
	Duplicate(ctx context.Context, ownerID, id, ip string) (domain.ToolListing, error)
	DepositSuggestion(ctx context.Context, ownerID, categoryID string, replacement domain.Cents) (domain.DepositRange, error)
	Calendar(ctx context.Context, ownerID, id string, from, to time.Time) ([]domain.AvailabilityBlock, error)
	BlockDates(ctx context.Context, ownerID, id string, b domain.AvailabilityBlock, ip string) (domain.AvailabilityBlock, error)
	UnblockDates(ctx context.Context, ownerID, id, blockID, ip string) error
}

// ListingHandler atiende /api/v1/me/lender y /me/listings (feature 003): todo lo del arrendador
// sobre sus propias publicaciones. El punto exacto de recojo solo sale aquí, al dueño.
type ListingHandler struct {
	lenders  Lenders
	listings Listings
}

func NewListingHandler(lenders Lenders, listings Listings) *ListingHandler {
	return &ListingHandler{lenders: lenders, listings: listings}
}

type placeRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type lenderResponse struct {
	Kind         string   `json:"kind"`
	BusinessName string   `json:"business_name,omitempty"`
	Phone        string   `json:"phone"`
	City         placeRef `json:"city"`
	Zone         placeRef `json:"zone"`
}

func toLenderResponse(v service.LenderView) lenderResponse {
	return lenderResponse{Kind: string(v.Profile.Kind), BusinessName: v.Profile.BusinessName, Phone: v.Profile.Phone,
		City: placeRef{ID: v.Location.City.ID, Slug: v.Location.City.Slug, Name: v.Location.City.Name},
		Zone: placeRef{ID: v.Location.Zone.ID, Slug: v.Location.Zone.Slug, Name: v.Location.Zone.Name}}
}

type lenderRequest struct {
	Kind         string `json:"kind"`
	BusinessName string `json:"business_name"`
	Phone        string `json:"phone"`
	City         string `json:"city"`
	Zone         string `json:"zone"`
	AcceptTerms  bool   `json:"accept_terms"`
}

// pricesJSON en céntimos; 0 = esa modalidad no se ofrece.
type pricesJSON struct {
	Hour    domain.Cents `json:"hour"`
	Day     domain.Cents `json:"day"`
	Weekend domain.Cents `json:"weekend"`
	Week    domain.Cents `json:"week"`
	Month   domain.Cents `json:"month"`
}

// listingFields es el formulario del asistente (entrada) y la mayor parte de la respuesta.
type listingFields struct {
	CategoryID        string          `json:"category_id"`
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	Attributes        json.RawMessage `json:"attributes"`
	ReplacementValue  domain.Cents    `json:"replacement_value"`
	Deposit           domain.Cents    `json:"deposit"`
	Prices            pricesJSON      `json:"prices"`
	Accessories       []string        `json:"accessories"`
	UsageInstructions string          `json:"usage_instructions"`
	PickupEnabled     bool            `json:"pickup_enabled"`
	PickupLocation    *geoPoint       `json:"pickup_location"`
	DeliveryEnabled   bool            `json:"delivery_enabled"`
	DeliveryFee       domain.Cents    `json:"delivery_fee"`
	DeliveryZoneIDs   []string        `json:"delivery_zone_ids"`
	BookingMode       string          `json:"booking_mode"`
	CancelPolicy      string          `json:"cancel_policy"`
	MinVerification   int             `json:"min_verification"`
	MinNoticeHours    int             `json:"min_notice_hours"`
	MinDurationHours  int             `json:"min_duration_hours"`
	MaxDurationHours  int             `json:"max_duration_hours"`
}

func (f listingFields) input() service.ListingInput {
	in := service.ListingInput{
		CategoryID: f.CategoryID, Title: f.Title, Description: f.Description, Attributes: f.Attributes,
		ReplacementValue: f.ReplacementValue, Deposit: f.Deposit,
		Prices:      domain.Prices{Hour: f.Prices.Hour, Day: f.Prices.Day, Weekend: f.Prices.Weekend, Week: f.Prices.Week, Month: f.Prices.Month},
		Accessories: f.Accessories, UsageInstructions: f.UsageInstructions,
		PickupEnabled: f.PickupEnabled, DeliveryEnabled: f.DeliveryEnabled, DeliveryFee: f.DeliveryFee,
		DeliveryZones: f.DeliveryZoneIDs, BookingMode: domain.BookingMode(f.BookingMode),
		CancelPolicy: domain.CancelPolicy(f.CancelPolicy), MinVerification: f.MinVerification,
		MinNoticeHours: f.MinNoticeHours, MinDurationHours: f.MinDurationHours, MaxDurationHours: f.MaxDurationHours,
	}
	if f.PickupLocation != nil {
		p := f.PickupLocation.domain()
		in.PickupLocation = &p
	}
	return in
}

type updateListingRequest struct {
	Version int `json:"version"`
	listingFields
}

type versionRequest struct {
	Version int `json:"version"`
}

type listingResponse struct {
	ID string `json:"id"`
	listingFields
	CityID           string     `json:"city_id"`
	ZoneID           string     `json:"zone_id,omitempty"`
	PublicLocation   *geoPoint  `json:"public_location"`
	PublicRadiusM    int        `json:"public_radius_m,omitempty"`
	Status           string     `json:"status"`
	RejectionReason  string     `json:"rejection_reason,omitempty"`
	FirstPublishedAt *time.Time `json:"first_published_at,omitempty"`
	Version          int        `json:"version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func optionalPoint(p *domain.GeoPoint) *geoPoint {
	if p == nil {
		return nil
	}
	g := toGeoPoint(*p)
	return &g
}

func toListingResponse(l domain.ToolListing) listingResponse {
	return listingResponse{
		ID: l.ID,
		listingFields: listingFields{
			CategoryID: l.CategoryID, Title: l.Title, Description: l.Description, Attributes: l.Attributes,
			ReplacementValue: l.ReplacementValue, Deposit: l.Deposit,
			Prices:      pricesJSON{Hour: l.Prices.Hour, Day: l.Prices.Day, Weekend: l.Prices.Weekend, Week: l.Prices.Week, Month: l.Prices.Month},
			Accessories: nonNil(l.Accessories), UsageInstructions: l.UsageInstructions,
			PickupEnabled: l.PickupEnabled, PickupLocation: optionalPoint(l.PickupLocation),
			DeliveryEnabled: l.DeliveryEnabled, DeliveryFee: l.DeliveryFee, DeliveryZoneIDs: nonNil(l.DeliveryZoneIDs),
			BookingMode: string(l.BookingMode), CancelPolicy: string(l.CancelPolicy), MinVerification: l.MinVerification,
			MinNoticeHours: l.MinNoticeHours, MinDurationHours: l.MinDurationHours, MaxDurationHours: l.MaxDurationHours,
		},
		CityID: l.CityID, ZoneID: l.ZoneID, PublicLocation: optionalPoint(l.PublicLocation), PublicRadiusM: l.PublicRadiusM,
		Status: string(l.Status), RejectionReason: l.RejectionReason, FirstPublishedAt: l.FirstPublishedAt,
		Version: l.Version, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

// nonNil: una lista vacía sale como [] y no como null (más simple para la app).
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type blockResponse struct {
	ID     string    `json:"id"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Reason string    `json:"reason"`
	Note   string    `json:"note,omitempty"`
}

func toBlockResponse(b domain.AvailabilityBlock) blockResponse {
	return blockResponse{ID: b.ID, Start: b.Start, End: b.End, Reason: string(b.Reason), Note: b.Note}
}

type blockRequest struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Note  string    `json:"note"`
}

// GET /api/v1/me/lender → {"lender": {...}} o {"lender": null} si aún no es arrendador.
func (h *ListingHandler) GetLender(c fiber.Ctx) error {
	view, ok, err := h.lenders.Get(c.Context(), actorID(c))
	if err != nil {
		return err
	}
	if !ok {
		return c.JSON(fiber.Map{"lender": nil})
	}
	return c.JSON(fiber.Map{"lender": toLenderResponse(view)})
}

// PUT /api/v1/me/lender — activa el perfil (la primera vez, con accept_terms) o lo actualiza.
func (h *ListingHandler) SaveLender(c fiber.Ctx) error {
	var req lenderRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	view, err := h.lenders.Save(c.Context(), actorID(c), service.LenderInput{
		Kind: domain.LenderKind(req.Kind), BusinessName: req.BusinessName, Phone: req.Phone,
		CitySlug: req.City, ZoneSlug: req.Zone, AcceptTerms: req.AcceptTerms,
	}, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"lender": toLenderResponse(view)})
}

// GET /api/v1/me/lender/deposit-suggestion?category=<id>&value=<céntimos>
func (h *ListingHandler) DepositSuggestion(c fiber.Ctx) error {
	value, err := strconv.ParseInt(c.Query("value"), 10, 64)
	if err != nil {
		return domain.ErrListingAmount
	}
	r, err := h.listings.DepositSuggestion(c.Context(), actorID(c), c.Query("category"), value)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"suggested": r.Suggested, "min": r.Min, "max": r.Max})
}

// GET /api/v1/me/listings
func (h *ListingHandler) List(c fiber.Ctx) error {
	list, err := h.listings.List(c.Context(), actorID(c))
	if err != nil {
		return err
	}
	out := make([]listingResponse, len(list))
	for i, l := range list {
		out[i] = toListingResponse(l)
	}
	return c.JSON(fiber.Map{"listings": out})
}

// POST /api/v1/me/listings → 201 (borrador)
func (h *ListingHandler) Create(c fiber.Ctx) error {
	var req listingFields
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	l, err := h.listings.Create(c.Context(), actorID(c), req.input(), c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toListingResponse(l))
}

// GET /api/v1/me/listings/:id
func (h *ListingHandler) Get(c fiber.Ctx) error {
	l, err := h.listings.Get(c.Context(), actorID(c), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(toListingResponse(l))
}

// PUT /api/v1/me/listings/:id — el formulario completo con la versión leída (bloqueo optimista).
func (h *ListingHandler) Update(c fiber.Ctx) error {
	var req updateListingRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	l, err := h.listings.Update(c.Context(), actorID(c), c.Params("id"), req.Version, req.input(), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toListingResponse(l))
}

// runAction atiende POST /me/listings/:id/{submit,pause,resume,archive} {"version": n}.
func (h *ListingHandler) runAction(c fiber.Ctx,
	do func(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error)) error {
	var req versionRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	l, err := do(c.Context(), actorID(c), c.Params("id"), req.Version, c.IP())
	if err != nil {
		return err
	}
	return c.JSON(toListingResponse(l))
}

func (h *ListingHandler) Submit(c fiber.Ctx) error  { return h.runAction(c, h.listings.Submit) }
func (h *ListingHandler) Pause(c fiber.Ctx) error   { return h.runAction(c, h.listings.Pause) }
func (h *ListingHandler) Resume(c fiber.Ctx) error  { return h.runAction(c, h.listings.Resume) }
func (h *ListingHandler) Archive(c fiber.Ctx) error { return h.runAction(c, h.listings.Archive) }

// POST /api/v1/me/listings/:id/duplicate → 201 (borrador nuevo)
func (h *ListingHandler) Duplicate(c fiber.Ctx) error {
	l, err := h.listings.Duplicate(c.Context(), actorID(c), c.Params("id"), c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toListingResponse(l))
}

// calendarWindow lee ?from=…&to=… en RFC 3339 (el calendario de publicaciones y de proveedores).
func calendarWindow(c fiber.Ctx) (time.Time, time.Time, error) {
	from, errFrom := time.Parse(time.RFC3339, c.Query("from"))
	to, errTo := time.Parse(time.RFC3339, c.Query("to"))
	if errFrom != nil || errTo != nil {
		return time.Time{}, time.Time{}, badRequest("Indica from y to en formato RFC 3339.")
	}
	return from, to, nil
}

// sendBlocks responde {"blocks": [...]}.
func sendBlocks(c fiber.Ctx, blocks []domain.AvailabilityBlock) error {
	out := make([]blockResponse, len(blocks))
	for i, b := range blocks {
		out[i] = toBlockResponse(b)
	}
	return c.JSON(fiber.Map{"blocks": out})
}

// bindBlock lee {"start", "end", "note"}; el fin no se incluye.
func bindBlock(c fiber.Ctx) (domain.AvailabilityBlock, error) {
	var req blockRequest
	if err := bindJSON(c, &req); err != nil {
		return domain.AvailabilityBlock{}, err
	}
	return domain.AvailabilityBlock{Start: req.Start, End: req.End, Note: req.Note}, nil
}

// GET /api/v1/me/listings/:id/availability?from=2026-10-01T00:00:00-05:00&to=...
func (h *ListingHandler) Calendar(c fiber.Ctx) error {
	from, to, err := calendarWindow(c)
	if err != nil {
		return err
	}
	blocks, err := h.listings.Calendar(c.Context(), actorID(c), c.Params("id"), from, to)
	if err != nil {
		return err
	}
	return sendBlocks(c, blocks)
}

// POST /api/v1/me/listings/:id/availability → 201 {"start", "end", "note"}; el fin no se incluye.
func (h *ListingHandler) BlockDates(c fiber.Ctx) error {
	block, err := bindBlock(c)
	if err != nil {
		return err
	}
	b, err := h.listings.BlockDates(c.Context(), actorID(c), c.Params("id"), block, c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toBlockResponse(b))
}

// DELETE /api/v1/me/listings/:id/availability/:block → 204
func (h *ListingHandler) UnblockDates(c fiber.Ctx) error {
	if err := h.listings.UnblockDates(c.Context(), actorID(c), c.Params("id"), c.Params("block"), c.IP()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
