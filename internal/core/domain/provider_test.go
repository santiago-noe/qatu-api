package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validProvider() ProviderProfile {
	return ProviderProfile{
		UserID: "u1", Phone: "987 654 321", CityID: "c1", BusinessName: "  Gasfitería   Rápida ",
		Bio: "Gasfitero con experiencia en termas y fugas en casas y edificios.", YearsExperience: 8, WarrantyDays: 15,
		Trades: []ProviderTrade{
			{CategoryID: "gasfiteria", HourlyRate: 25_00, MinHours: 2, Packages: []ServicePackage{
				{Title: " Cambio de  grifería ", Price: 60_00, DurationMinutes: 60},
			}},
			{CategoryID: "pintura", HourlyRate: 0, MinHours: 4},
		},
		CoverageZoneIDs: []string{"z2", "z1", "z2"},
		Weekly: []WeeklySlot{{Weekday: 2, Start: 8 * 60, End: 12 * 60}, {Weekday: 1, Start: 14 * 60, End: 18 * 60},
			{Weekday: 1, Start: 8 * 60, End: 12 * 60}},
		Status: ListingDraft,
	}
}

func TestNormalizeProvider(t *testing.T) {
	p, err := NormalizeProvider(validProvider())
	if err != nil {
		t.Fatal(err)
	}
	if p.Phone != "+51987654321" || p.BusinessName != "Gasfitería Rápida" {
		t.Fatalf("contacto: %q %q", p.Phone, p.BusinessName)
	}
	if p.Trades[0].Packages[0].Title != "Cambio de grifería" || p.Trades[1].MinHours != 1 {
		t.Fatalf("oficios: %+v", p.Trades)
	}
	if strings.Join(p.CoverageZoneIDs, ",") != "z1,z2" {
		t.Fatalf("zonas sin repetir y ordenadas: %v", p.CoverageZoneIDs)
	}
	if p.Weekly[0] != (WeeklySlot{Weekday: 1, Start: 480, End: 720}) || p.Weekly[2].Weekday != 2 {
		t.Fatalf("horario ordenado: %+v", p.Weekly)
	}
	if !p.Trades[1].QuoteOnly() || p.Trades[0].QuoteOnly() {
		t.Fatal("un oficio sin tarifa ni paquetes es a cotizar")
	}
}

func TestNormalizeProviderRejects(t *testing.T) {
	cases := map[string]struct {
		edit func(*ProviderProfile)
		want error
	}{
		"celular":            {func(p *ProviderProfile) { p.Phone = "066312345" }, ErrLenderPhone},
		"nombre corto":       {func(p *ProviderProfile) { p.BusinessName = "A" }, ErrProviderBusinessName},
		"sin ciudad":         {func(p *ProviderProfile) { p.CityID = "" }, ErrInvalidLocation},
		"bio larga":          {func(p *ProviderProfile) { p.Bio = strings.Repeat("a", 2001) }, ErrProviderBio},
		"experiencia":        {func(p *ProviderProfile) { p.YearsExperience = 61 }, ErrProviderExperience},
		"garantía":           {func(p *ProviderProfile) { p.WarrantyDays = -1 }, ErrProviderWarranty},
		"oficio repetido":    {func(p *ProviderProfile) { p.Trades[1].CategoryID = "gasfiteria" }, ErrProviderTrade},
		"mínimo de horas":    {func(p *ProviderProfile) { p.Trades[0].MinHours = 9 }, ErrProviderRate},
		"tarifa negativa":    {func(p *ProviderProfile) { p.Trades[0].HourlyRate = -1 }, ErrProviderRate},
		"paquete sin precio": {func(p *ProviderProfile) { p.Trades[0].Packages[0].Price = 0 }, ErrProviderPackage},
		"duración suelta":    {func(p *ProviderProfile) { p.Trades[0].Packages[0].DurationMinutes = 45 }, ErrProviderPackage},
		"franjas cruzadas": {func(p *ProviderProfile) {
			p.Weekly = append(p.Weekly, WeeklySlot{Weekday: 1, Start: 11 * 60, End: 13 * 60})
		}, ErrProviderSchedule},
		"franja al revés": {func(p *ProviderProfile) { p.Weekly[0].End = p.Weekly[0].Start }, ErrProviderSchedule},
		"franja suelta":   {func(p *ProviderProfile) { p.Weekly[0].Start = 8*60 + 15 }, ErrProviderSchedule},
		"día inexistente": {func(p *ProviderProfile) { p.Weekly[0].Weekday = 8 }, ErrProviderSchedule},
	}
	for name, c := range cases {
		p := validProvider()
		c.edit(&p)
		if _, err := NormalizeProvider(p); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, se esperaba %v", name, err, c.want)
		}
	}

	p := validProvider()
	for d := 0; d < 4; d++ {
		p.Weekly = append(p.Weekly, WeeklySlot{Weekday: 5, Start: d * 120, End: d*120 + 60})
	}
	if _, err := NormalizeProvider(p); !errors.Is(err, ErrProviderSchedule) {
		t.Fatalf("hasta 3 franjas por día: %v", err)
	}
}

func TestParseClock(t *testing.T) {
	for raw, want := range map[string]int{"00:00": 0, "08:30": 510, "24:00": 1440} {
		if got, err := ParseClock(raw); err != nil || got != want {
			t.Errorf("ParseClock(%q) = %d, %v", raw, got, err)
		}
		if FormatClock(want) != raw {
			t.Errorf("FormatClock(%d) = %q", want, FormatClock(want))
		}
	}
	for _, raw := range []string{"8:30", "24:30", "12:60", "-1:00", "ab:cd", ""} {
		if _, err := ParseClock(raw); !errors.Is(err, ErrProviderSchedule) {
			t.Errorf("ParseClock(%q) debía fallar", raw)
		}
	}
}

func TestCheckProviderComplete(t *testing.T) {
	p, _ := NormalizeProvider(validProvider())
	if err := CheckProviderComplete(p); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		edit func(*ProviderProfile)
		want error
	}{
		"bio":     {func(p *ProviderProfile) { p.Bio = "Gasfitero." }, ErrProviderBioRequired},
		"oficios": {func(p *ProviderProfile) { p.Trades = nil }, ErrProviderTradesRequired},
		"zonas":   {func(p *ProviderProfile) { p.CoverageZoneIDs = nil }, ErrProviderCoverage},
		"horario": {func(p *ProviderProfile) { p.Weekly = nil }, ErrProviderScheduleRequired},
	}
	for name, c := range cases {
		q := p
		c.edit(&q)
		if err := CheckProviderComplete(q); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestProviderActions(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	p := ProviderProfile{Status: ListingDraft}

	// Sin nivel P, enviar siempre pasa por revisión.
	sent, err := ApplyProviderAction(p, ListingSubmit, now)
	if err != nil || sent.Status != ListingInReview {
		t.Fatalf("enviar: %v %v", sent.Status, err)
	}
	if _, err := ApplyProviderAction(sent, ListingPause, now); !errors.Is(err, ErrProviderTransition) {
		t.Fatalf("en revisión no se pausa: %v", err)
	}
	if _, err := ApplyProviderAction(p, ListingArchive, now); !errors.Is(err, ErrProviderTransition) {
		t.Fatal("un perfil no se archiva")
	}

	// Aprobado y verificado: publicado, se pausa y se reanuda.
	approved, _ := ApplyProviderAction(sent, ListingApprove, now)
	approved.VerifiedAt = &now
	if approved.Status != ListingPublished || approved.FirstPublishedAt == nil {
		t.Fatalf("aprobar: %+v", approved)
	}
	paused, _ := ApplyProviderAction(approved, ListingPause, now)
	resumed, err := ApplyProviderAction(paused, ListingResume, now)
	if err != nil || resumed.Status != ListingPublished {
		t.Fatalf("reanudar: %v %v", resumed.Status, err)
	}

	// Un rechazado vuelve a revisión aunque ya estuviera verificado.
	rejected := ProviderProfile{Status: ListingRejected, VerifiedAt: &now, RejectionReason: "Falta foto"}
	again, _ := ApplyProviderAction(rejected, ListingSubmit, now)
	if again.Status != ListingInReview || again.RejectionReason != "" {
		t.Fatalf("reenviar: %+v", again)
	}
}
