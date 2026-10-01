package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Cents es un monto en céntimos enteros de soles (PEN). Nunca decimales para dinero (constitución IV).
type Cents = int64

// Límites de una publicación (los mismos CHECK de la migración 0006).
const (
	ListingTitleMin             = 5
	ListingTitleMax             = 80
	ListingTextMax              = 2000
	ListingAccessoriesMax       = 30
	ListingAccessoryMax         = 80
	ListingPhotosMin            = 3
	ListingPhotosMax            = 12
	ListingMaxNoticeHours       = 168  // una semana
	ListingMaxMinDuration       = 720  // 30 días
	ListingMaxMaxDuration       = 2160 // 90 días
	RejectionReasonMax          = 500
	MaxAmount             Cents = 100_000_00 // S/ 100 000: tope de cordura para precios y valores
)

// ListingStatus sigue la máquina de estados de la spec 003.
type ListingStatus string

const (
	ListingDraft     ListingStatus = "draft"
	ListingInReview  ListingStatus = "in_review"
	ListingPublished ListingStatus = "published"
	ListingPaused    ListingStatus = "paused"
	ListingRejected  ListingStatus = "rejected"
	ListingArchived  ListingStatus = "archived"
)

// ListingAction es lo que alguien pide hacer con una publicación.
type ListingAction string

const (
	ListingSubmit  ListingAction = "submit"  // dueño: enviar el borrador (o la corrección de una rechazada)
	ListingApprove ListingAction = "approve" // moderación
	ListingReject  ListingAction = "reject"  // moderación, con motivo
	ListingPause   ListingAction = "pause"   // dueño
	ListingResume  ListingAction = "resume"  // dueño
	ListingArchive ListingAction = "archive" // dueño: sale del catálogo para siempre
)

// NextListingStatus aplica una acción. needsReview: la publicación debe pasar por moderación
// (primera del arrendador o categoría de riesgo alto); solo importa al enviar.
func NextListingStatus(current ListingStatus, action ListingAction, needsReview bool) (ListingStatus, error) {
	switch action {
	case ListingSubmit:
		if current == ListingDraft || current == ListingRejected {
			if needsReview {
				return ListingInReview, nil
			}
			return ListingPublished, nil
		}
	case ListingApprove:
		if current == ListingInReview {
			return ListingPublished, nil
		}
	case ListingReject:
		if current == ListingInReview {
			return ListingRejected, nil
		}
	case ListingPause:
		if current == ListingPublished {
			return ListingPaused, nil
		}
	case ListingResume:
		if current == ListingPaused {
			return ListingPublished, nil
		}
	case ListingArchive:
		if current != ListingArchived && current != ListingInReview {
			return ListingArchived, nil
		}
	}
	return "", ErrListingTransition
}

// ApplyListingAction cambia el estado y lo que depende de él: al volver a revisión o publicarse se
// borra el motivo del rechazo anterior, y la primera publicación queda fechada. El motivo de un
// rechazo nuevo lo pone quien rechaza (moderación).
func ApplyListingAction(l ToolListing, action ListingAction, needsReview bool, now time.Time) (ToolListing, error) {
	if err := applyModeratedAction(&l.Status, &l.RejectionReason, &l.FirstPublishedAt, action, needsReview, now); err != nil {
		return ToolListing{}, err
	}
	return l, nil
}

// applyModeratedAction es el ciclo de moderación que comparten las publicaciones y los perfiles de
// proveedor (004): el nuevo estado, el motivo que se borra y la fecha de la primera publicación.
func applyModeratedAction(status *ListingStatus, reason *string, firstPublished **time.Time,
	action ListingAction, needsReview bool, now time.Time) error {
	next, err := NextListingStatus(*status, action, needsReview)
	if err != nil {
		return err
	}
	*status = next
	if next == ListingInReview || next == ListingPublished {
		*reason = ""
	}
	if next == ListingPublished && *firstPublished == nil {
		*firstPublished = &now
	}
	return nil
}

// ReviewItem es una publicación en la cola de moderación, con lo que el moderador necesita saber
// del arrendador (sin datos de contacto).
type ReviewItem struct {
	Listing      ToolListing
	OwnerName    string
	FirstListing bool // el arrendador aún no tiene ninguna aprobada
}

// Editable indica si el dueño puede cambiar los datos. En revisión se congela (lo que aprueba
// moderación es lo que se publica) y archivada ya no vuelve.
func (s ListingStatus) Editable() bool {
	return s == ListingDraft || s == ListingPublished || s == ListingPaused || s == ListingRejected
}

// ListingNeedsReview: la primera publicación de un arrendador y las de riesgo alto pasan por moderación.
func ListingNeedsReview(firstOfOwner bool, risk RiskLevel) bool {
	return firstOfOwner || risk == RiskHigh
}

type BookingMode string

const (
	BookingOnRequest BookingMode = "request"
	BookingInstant   BookingMode = "instant"
)

type CancelPolicy string

const (
	CancelFlexible CancelPolicy = "flexible"
	CancelModerate CancelPolicy = "moderate"
	CancelStrict   CancelPolicy = "strict"
)

// ProhibitedCategoryReason es el motivo que ve el arrendador cuando el admin prohíbe la categoría
// de una publicación activa: sale del catálogo y no puede volver (spec 002).
const ProhibitedCategoryReason = "La categoría de esta herramienta ya no se permite en Qatu, por eso salió del catálogo."

// CheckListingCategory: se publica en un tipo de herramienta (segundo nivel), activo y no
// prohibido, y con su categoría padre igual. Una raíz prohibida prohíbe todos sus tipos.
// El alcance por ciudad lo revisa quien llama, con CategoryCityScope.ActiveFor.
func CheckListingCategory(c, parent Category) error {
	if c.Prohibited || parent.Prohibited {
		return ErrListingProhibited
	}
	if c.Vertical != VerticalRental || c.ParentID == "" || c.ParentID != parent.ID || !c.Enabled || !parent.Enabled {
		return ErrListingCategory
	}
	return nil
}

// MinVerificationFor es el nivel mínimo del arrendatario según el riesgo (docs/05): 1 para riesgo
// bajo y medio, 2 para alto. El arrendador puede pedir más, nunca menos.
func MinVerificationFor(risk RiskLevel) int {
	if risk == RiskHigh {
		return 2
	}
	return 1
}

// Prices en céntimos; 0 = esa modalidad no se ofrece. El precio por día es obligatorio.
type Prices struct {
	Hour    Cents
	Day     Cents
	Weekend Cents
	Week    Cents
	Month   Cents
}

func (p Prices) all() []Cents { return []Cents{p.Hour, p.Day, p.Weekend, p.Week, p.Month} }

// ToolListing es una herramienta en alquiler: 1 publicación = 1 unidad (decisión de clarify).
type ToolListing struct {
	ID                string
	OwnerID           string
	CategoryID        string
	CityID            string
	ZoneID            string // sale del punto de recojo (zone_at) o del perfil si solo hay delivery
	Title             string
	Description       string
	Attributes        json.RawMessage
	ReplacementValue  Cents
	Deposit           Cents
	Prices            Prices
	Accessories       []string
	UsageInstructions string
	PickupEnabled     bool
	PickupLocation    *GeoPoint // privado
	PublicLocation    *GeoPoint // desplazado: lo que ve el público
	PublicRadiusM     int
	DeliveryEnabled   bool
	DeliveryFee       Cents
	DeliveryZoneIDs   []string
	BookingMode       BookingMode
	CancelPolicy      CancelPolicy
	MinVerification   int
	MinNoticeHours    int
	MinDurationHours  int
	MaxDurationHours  int
	Status            ListingStatus
	RejectionReason   string
	FirstPublishedAt  *time.Time
	Version           int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NormalizeListing limpia y valida lo que el dueño puede guardar en cualquier momento (también
// en borrador). Lo obligatorio para publicar lo revisa CheckListingComplete.
func NormalizeListing(l ToolListing) (ToolListing, error) {
	l.Title = strings.Join(strings.Fields(l.Title), " ")
	l.Description = strings.TrimSpace(l.Description)
	l.UsageInstructions = strings.TrimSpace(l.UsageInstructions)
	if n := utf8.RuneCountInString(l.Title); n < ListingTitleMin || n > ListingTitleMax {
		return ToolListing{}, ErrListingTitle
	}
	if utf8.RuneCountInString(l.Description) > ListingTextMax || utf8.RuneCountInString(l.UsageInstructions) > ListingTextMax {
		return ToolListing{}, ErrListingText
	}
	accessories, err := normalizeAccessories(l.Accessories)
	if err != nil {
		return ToolListing{}, err
	}
	l.Accessories = accessories
	for _, amount := range append(l.Prices.all(), l.ReplacementValue, l.Deposit, l.DeliveryFee) {
		if amount < 0 || amount > MaxAmount {
			return ToolListing{}, ErrListingAmount
		}
	}
	if l.BookingMode != BookingOnRequest && l.BookingMode != BookingInstant {
		return ToolListing{}, ErrListingBookingMode
	}
	if l.CancelPolicy != CancelFlexible && l.CancelPolicy != CancelModerate && l.CancelPolicy != CancelStrict {
		return ToolListing{}, ErrListingCancelPolicy
	}
	if l.MinVerification < 0 || l.MinVerification > 2 {
		return ToolListing{}, ErrListingVerification
	}
	switch {
	case l.MinNoticeHours < 0 || l.MinNoticeHours > ListingMaxNoticeHours,
		l.MinDurationHours < 1 || l.MinDurationHours > ListingMaxMinDuration,
		l.MaxDurationHours < l.MinDurationHours || l.MaxDurationHours > ListingMaxMaxDuration:
		return ToolListing{}, ErrListingDurations
	}
	if l.PickupLocation != nil {
		if err := l.PickupLocation.Validate(); err != nil {
			return ToolListing{}, err
		}
	}
	slices.Sort(l.DeliveryZoneIDs)
	l.DeliveryZoneIDs = slices.Compact(l.DeliveryZoneIDs)
	return l, nil
}

// normalizeAccessories quita vacíos y repetidos (sin distinguir mayúsculas) y respeta los límites.
func normalizeAccessories(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		item := strings.Join(strings.Fields(raw), " ")
		key := strings.ToLower(item)
		if item == "" || seen[key] {
			continue
		}
		if utf8.RuneCountInString(item) > ListingAccessoryMax {
			return nil, ErrListingAccessories
		}
		seen[key] = true
		out = append(out, item)
	}
	if len(out) > ListingAccessoriesMax {
		return nil, ErrListingAccessories
	}
	return out, nil
}

// CheckListingComplete revisa lo necesario para alquilar sin ambigüedad (flujo A1 en docs/02):
// precio por día, valor de reposición, garantía, al menos una forma de entrega y mínimo de fotos.
func CheckListingComplete(l ToolListing, readyPhotos int) error {
	switch {
	case l.Prices.Day <= 0:
		return ErrListingDayPrice
	case l.ReplacementValue <= 0:
		return ErrListingReplacementValue
	case !l.PickupEnabled && !l.DeliveryEnabled:
		return ErrListingFulfillment
	case l.PickupEnabled && (l.PickupLocation == nil || l.PublicLocation == nil):
		return ErrListingPickupLocation
	case l.DeliveryEnabled && len(l.DeliveryZoneIDs) == 0:
		return ErrListingDeliveryZones
	case l.ZoneID == "":
		return ErrListingPickupLocation
	case readyPhotos < ListingPhotosMin:
		return ErrListingPhotos
	}
	return nil
}

// DepositRule son los ajustes de garantía de platform_settings (decisión de clarify).
type DepositRule struct {
	PercentBps   int64 // % del valor de reposición según el riesgo de la categoría
	MinFactorBps int64 // la garantía puede bajar hasta este % de la sugerida
	MaxFactorBps int64 // y subir hasta este
}

// depositStep: la garantía se redondea a S/ 10 (montos fáciles de entregar en efectivo).
const depositStep Cents = 10_00

func roundDeposit(c Cents) Cents { return (c + depositStep/2) / depositStep * depositStep }

// DepositRange es la garantía sugerida y los límites entre los que el arrendador puede ajustarla.
type DepositRange struct {
	Suggested Cents
	Min       Cents
	Max       Cents
}

// SuggestDeposit calcula la garantía para un valor de reposición (enteros: sin coma flotante).
func SuggestDeposit(replacement Cents, rule DepositRule) DepositRange {
	suggested := roundDeposit(replacement * rule.PercentBps / 10_000)
	return DepositRange{
		Suggested: suggested,
		Min:       roundDeposit(suggested * rule.MinFactorBps / 10_000),
		Max:       roundDeposit(suggested * rule.MaxFactorBps / 10_000),
	}
}

// Contains indica si una garantía está dentro del rango permitido.
func (r DepositRange) Contains(deposit Cents) bool { return deposit >= r.Min && deposit <= r.Max }

// NormalizeRejectionReason valida el motivo de un rechazo de moderación (lo lee el arrendador).
func NormalizeRejectionReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > RejectionReasonMax {
		return "", ErrRejectionReason
	}
	return reason, nil
}

// Claves de platform_settings que usa una publicación (migración 0006).
const (
	SettingPublicRadius     = "listings.public_radius_m"
	SettingDepositMinFactor = "listings.deposit_min_factor_bps"
	SettingDepositMaxFactor = "listings.deposit_max_factor_bps"
)

// depositPercentKey: el % de garantía depende del riesgo de la categoría (decisión de clarify).
var depositPercentKey = map[RiskLevel]string{
	RiskLow:    "listings.deposit_low_bps",
	RiskMedium: "listings.deposit_medium_bps",
	RiskHigh:   "listings.deposit_high_bps",
}

// ListingSettingKeys son las claves que hay que copiar para una categoría de ese riesgo.
func ListingSettingKeys(risk RiskLevel) []string {
	return []string{depositPercentKey[risk], SettingDepositMinFactor, SettingDepositMaxFactor, SettingPublicRadius}
}

// DepositRuleFrom arma la regla de garantía desde la copia de ajustes de la categoría y ciudad.
func DepositRuleFrom(snap SettingsSnapshot, risk RiskLevel) (DepositRule, error) {
	percent, err := snap.Int(depositPercentKey[risk])
	if err != nil {
		return DepositRule{}, err
	}
	minFactor, err := snap.Int(SettingDepositMinFactor)
	if err != nil {
		return DepositRule{}, err
	}
	maxFactor, err := snap.Int(SettingDepositMaxFactor)
	if err != nil {
		return DepositRule{}, err
	}
	return DepositRule{PercentBps: percent, MinFactorBps: minFactor, MaxFactorBps: maxFactor}, nil
}

// CheckDeposit: con valor de reposición, la garantía debe estar en el rango permitido.
func CheckDeposit(l ToolListing, rule DepositRule) error {
	if l.ReplacementValue <= 0 {
		return nil
	}
	if !SuggestDeposit(l.ReplacementValue, rule).Contains(l.Deposit) {
		return ErrListingDeposit
	}
	return nil
}

// copySuffix marca la copia de una publicación duplicada.
const copySuffix = " (copia)"

// DuplicateListing prepara un borrador igual a otra publicación: un negocio con varias unidades
// del mismo modelo publica cada una así (1 publicación = 1 unidad). No copia fotos, calendario,
// estado ni punto público (sale del ID nuevo).
func DuplicateListing(l ToolListing) ToolListing {
	title := []rune(l.Title)
	if max := ListingTitleMax - utf8.RuneCountInString(copySuffix); len(title) > max {
		title = title[:max]
	}
	l.Title = strings.TrimSpace(string(title)) + copySuffix
	l.ID, l.Status, l.RejectionReason, l.Version = "", ListingDraft, "", 0
	l.PublicLocation, l.FirstPublishedAt = nil, nil
	l.Accessories = slices.Clone(l.Accessories)
	l.DeliveryZoneIDs = slices.Clone(l.DeliveryZoneIDs)
	l.Attributes = slices.Clone(l.Attributes)
	return l
}
