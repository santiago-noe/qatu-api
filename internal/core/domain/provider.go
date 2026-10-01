package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Límites de un perfil de proveedor (los mismos CHECK de la migración 0009).
const (
	ProviderNameMin         = 2
	ProviderNameMax         = 80
	ProviderBioMin          = 30 // para publicar: el cliente necesita saber qué hace
	ProviderBioMax          = 2000
	ProviderYearsMax        = 60
	ProviderWarrantyMax     = 90
	ProviderTradesMax       = 5
	ProviderMinHoursMax     = 8
	ProviderPackagesMax     = 10 // por oficio
	ProviderPackageTitleMin = 5
	ProviderPackageTitleMax = 80
	ProviderPackageTextMax  = 500
	ProviderCoverageMax     = 30
	ProviderSlotsPerDay     = 3
	// ScheduleStep: el horario va en bloques de media hora.
	ScheduleStep        = 30
	PackageDurationMin  = 30
	PackageDurationMax  = 8 * 60
	minutesPerDay       = 24 * 60
	DefaultWarrantyDays = 15 // docs/02, flujo B: "garantía del trabajo (ej. 15 días)"
)

// ProviderProfile es el perfil de quien ofrece oficios a domicilio (spec 004). Usa el mismo ciclo
// de moderación que una publicación, sin archivar: una persona tiene un solo perfil y lo pausa.
// Aprobarlo por primera vez es la revisión manual del nivel P (docs/05): queda VerifiedAt.
type ProviderProfile struct {
	UserID           string
	BusinessName     string // nombre comercial opcional ("Gasfitería Rápida")
	Phone            string // +519XXXXXXXX; privado hasta confirmar un trabajo (docs/05)
	CityID           string
	Bio              string
	YearsExperience  int
	WarrantyDays     int // días de garantía del trabajo (flujo B, paso 10)
	AcceptsUrgent    bool
	Trades           []ProviderTrade
	CoverageZoneIDs  []string // distritos donde trabaja: se publica la cobertura, nunca su dirección
	Weekly           []WeeklySlot
	Status           ListingStatus
	RejectionReason  string
	VerifiedAt       *time.Time // nivel P: aprobado por moderación
	FirstPublishedAt *time.Time
	Version          int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ProviderTrade es un oficio del proveedor con su tarifa por hora y sus paquetes. Sin tarifa ni
// paquetes, el oficio es "a cotizar": solo recibe solicitudes de cotización (008).
type ProviderTrade struct {
	CategoryID string
	HourlyRate Cents // 0 = sin tarifa por hora
	MinHours   int   // mínimo de horas que se cobra con tarifa por hora
	Packages   []ServicePackage
}

// QuoteOnly: el oficio no tiene precio publicado y no admite reserva directa.
func (t ProviderTrade) QuoteOnly() bool { return t.HourlyRate == 0 && len(t.Packages) == 0 }

// ServicePackage es un trabajo a precio fijo ("Cambio de grifería", S/ 60, 1 h).
type ServicePackage struct {
	ID              string
	Title           string
	Description     string
	Price           Cents
	DurationMinutes int // duración estimada: ocupa esa franja del horario (009)
}

// WeeklySlot es una franja del horario semanal en la hora local de la ciudad: [Start, End) en
// minutos desde las 00:00. Weekday sigue ISO 8601: 1 = lunes … 7 = domingo.
type WeeklySlot struct {
	Weekday int
	Start   int
	End     int
}

// ParseClock lee "08:30" como minutos desde las 00:00; "24:00" vale como fin del día.
func ParseClock(raw string) (int, error) {
	if len(raw) != 5 || raw[2] != ':' {
		return 0, ErrProviderSchedule
	}
	digits := raw[:2] + raw[3:]
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, ErrProviderSchedule
		}
	}
	h := int(digits[0]-'0')*10 + int(digits[1]-'0')
	m := int(digits[2]-'0')*10 + int(digits[3]-'0')
	if m > 59 || h*60+m > minutesPerDay {
		return 0, ErrProviderSchedule
	}
	return h*60 + m, nil
}

// FormatClock escribe minutos desde las 00:00 como "08:30".
func FormatClock(minutes int) string { return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60) }

// NormalizeProvider limpia y valida lo que el proveedor puede guardar en cualquier momento
// (también en borrador). Lo obligatorio para publicar lo revisa CheckProviderComplete.
func NormalizeProvider(p ProviderProfile) (ProviderProfile, error) {
	phone, err := NormalizePeruMobile(p.Phone)
	if err != nil {
		return ProviderProfile{}, err
	}
	p.Phone = phone
	p.BusinessName = strings.Join(strings.Fields(p.BusinessName), " ")
	if n := utf8.RuneCountInString(p.BusinessName); n > 0 && (n < ProviderNameMin || n > ProviderNameMax) {
		return ProviderProfile{}, ErrProviderBusinessName
	}
	if p.CityID == "" {
		return ProviderProfile{}, ErrInvalidLocation
	}
	p.Bio = strings.TrimSpace(p.Bio)
	switch {
	case utf8.RuneCountInString(p.Bio) > ProviderBioMax:
		return ProviderProfile{}, ErrProviderBio
	case p.YearsExperience < 0 || p.YearsExperience > ProviderYearsMax:
		return ProviderProfile{}, ErrProviderExperience
	case p.WarrantyDays < 0 || p.WarrantyDays > ProviderWarrantyMax:
		return ProviderProfile{}, ErrProviderWarranty
	}
	if p.Trades, err = normalizeTrades(p.Trades); err != nil {
		return ProviderProfile{}, err
	}
	slices.Sort(p.CoverageZoneIDs)
	p.CoverageZoneIDs = slices.Compact(p.CoverageZoneIDs)
	if len(p.CoverageZoneIDs) > ProviderCoverageMax {
		return ProviderProfile{}, ErrProviderCoverage
	}
	if p.Weekly, err = normalizeWeekly(p.Weekly); err != nil {
		return ProviderProfile{}, err
	}
	return p, nil
}

func normalizeTrades(in []ProviderTrade) ([]ProviderTrade, error) {
	if len(in) > ProviderTradesMax {
		return nil, ErrProviderTrade
	}
	out := make([]ProviderTrade, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		if t.CategoryID == "" || seen[t.CategoryID] {
			return nil, ErrProviderTrade
		}
		seen[t.CategoryID] = true
		if t.HourlyRate < 0 || t.HourlyRate > MaxAmount {
			return nil, ErrProviderRate
		}
		if t.HourlyRate == 0 {
			t.MinHours = 1 // sin tarifa por hora el mínimo no se usa
		}
		if t.MinHours < 1 || t.MinHours > ProviderMinHoursMax {
			return nil, ErrProviderRate
		}
		if len(t.Packages) > ProviderPackagesMax {
			return nil, ErrProviderPackage
		}
		packages := make([]ServicePackage, len(t.Packages))
		for i, pkg := range t.Packages {
			pkg.Title = strings.Join(strings.Fields(pkg.Title), " ")
			pkg.Description = strings.TrimSpace(pkg.Description)
			switch n := utf8.RuneCountInString(pkg.Title); {
			case n < ProviderPackageTitleMin || n > ProviderPackageTitleMax,
				utf8.RuneCountInString(pkg.Description) > ProviderPackageTextMax,
				pkg.Price <= 0 || pkg.Price > MaxAmount,
				pkg.DurationMinutes < PackageDurationMin || pkg.DurationMinutes > PackageDurationMax,
				pkg.DurationMinutes%ScheduleStep != 0:
				return nil, ErrProviderPackage
			}
			packages[i] = pkg
		}
		t.Packages = packages
		out = append(out, t)
	}
	return out, nil
}

// normalizeWeekly ordena las franjas y revisa que vayan en medias horas, sin cruzarse y con un
// máximo por día.
func normalizeWeekly(in []WeeklySlot) ([]WeeklySlot, error) {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b WeeklySlot) int {
		if a.Weekday != b.Weekday {
			return a.Weekday - b.Weekday
		}
		return a.Start - b.Start
	})
	perDay := map[int]int{}
	for i, s := range out {
		switch {
		case s.Weekday < 1 || s.Weekday > 7,
			s.Start < 0 || s.End > minutesPerDay || s.End <= s.Start,
			s.Start%ScheduleStep != 0 || s.End%ScheduleStep != 0,
			i > 0 && out[i-1].Weekday == s.Weekday && out[i-1].End > s.Start:
			return nil, ErrProviderSchedule
		}
		if perDay[s.Weekday]++; perDay[s.Weekday] > ProviderSlotsPerDay {
			return nil, ErrProviderSchedule
		}
	}
	return out, nil
}

// CheckProviderComplete revisa lo necesario para aparecer ante los clientes (spec 004: un perfil
// incompleto no aparece en búsquedas): descripción, al menos un oficio, una zona y una franja.
func CheckProviderComplete(p ProviderProfile) error {
	switch {
	case utf8.RuneCountInString(p.Bio) < ProviderBioMin:
		return ErrProviderBioRequired
	case len(p.Trades) == 0:
		return ErrProviderTradesRequired
	case len(p.CoverageZoneIDs) == 0:
		return ErrProviderCoverage
	case len(p.Weekly) == 0:
		return ErrProviderScheduleRequired
	}
	return nil
}

// ProviderNeedsReview: hasta que moderación lo apruebe una vez (nivel P), todo envío pasa por
// revisión; también el de un perfil rechazado.
func ProviderNeedsReview(p ProviderProfile) bool {
	return p.VerifiedAt == nil || p.Status == ListingRejected
}

// ApplyProviderAction cambia el estado del perfil. Archivar no existe para un perfil.
func ApplyProviderAction(p ProviderProfile, action ListingAction, now time.Time) (ProviderProfile, error) {
	if action == ListingArchive {
		return ProviderProfile{}, ErrProviderTransition
	}
	if err := applyModeratedAction(&p.Status, &p.RejectionReason, &p.FirstPublishedAt, action, ProviderNeedsReview(p), now); err != nil {
		return ProviderProfile{}, ErrProviderTransition
	}
	return p, nil
}

// PackageIDs devuelve los IDs de todos los paquetes del perfil.
func (p ProviderProfile) PackageIDs() []string {
	var ids []string
	for _, t := range p.Trades {
		for _, pkg := range t.Packages {
			if pkg.ID != "" {
				ids = append(ids, pkg.ID)
			}
		}
	}
	return ids
}
