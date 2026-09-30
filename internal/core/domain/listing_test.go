package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNextListingStatus(t *testing.T) {
	tests := []struct {
		from        ListingStatus
		action      ListingAction
		needsReview bool
		want        ListingStatus
		wantErr     bool
	}{
		{ListingDraft, ListingSubmit, true, ListingInReview, false},
		{ListingDraft, ListingSubmit, false, ListingPublished, false},
		{ListingRejected, ListingSubmit, true, ListingInReview, false}, // corregida, vuelve a revisión
		{ListingInReview, ListingApprove, false, ListingPublished, false},
		{ListingInReview, ListingReject, false, ListingRejected, false},
		{ListingPublished, ListingPause, false, ListingPaused, false},
		{ListingPaused, ListingResume, false, ListingPublished, false},
		{ListingPaused, ListingArchive, false, ListingArchived, false},
		{ListingDraft, ListingArchive, false, ListingArchived, false},

		{ListingPublished, ListingSubmit, false, "", true}, // ya publicada
		{ListingInReview, ListingPause, false, "", true},
		{ListingInReview, ListingArchive, false, "", true}, // moderación decide primero
		{ListingDraft, ListingApprove, false, "", true},    // nadie aprueba un borrador
		{ListingPublished, ListingResume, false, "", true},
		{ListingArchived, ListingResume, false, "", true}, // archivada no vuelve
		{ListingArchived, ListingArchive, false, "", true},
		{ListingRejected, ListingPause, false, "", true},
	}
	for _, tt := range tests {
		got, err := NextListingStatus(tt.from, tt.action, tt.needsReview)
		if tt.wantErr {
			if !errors.Is(err, ErrListingTransition) {
				t.Errorf("%s + %s: quiero ErrListingTransition, llegó %v, %v", tt.from, tt.action, got, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s + %s = %s, %v; quiero %s", tt.from, tt.action, got, err, tt.want)
		}
	}
}

func TestListingEditableAndReview(t *testing.T) {
	for status, want := range map[ListingStatus]bool{
		ListingDraft: true, ListingPublished: true, ListingPaused: true, ListingRejected: true,
		ListingInReview: false, ListingArchived: false,
	} {
		if status.Editable() != want {
			t.Errorf("%s.Editable() = %v", status, !want)
		}
	}
	if !ListingNeedsReview(true, RiskLow) || !ListingNeedsReview(false, RiskHigh) || ListingNeedsReview(false, RiskMedium) {
		t.Fatal("revisión: primera publicación o riesgo alto")
	}
	if MinVerificationFor(RiskLow) != 1 || MinVerificationFor(RiskMedium) != 1 || MinVerificationFor(RiskHigh) != 2 {
		t.Fatal("verificación mínima por riesgo (docs/05)")
	}
}

func validListing() ToolListing {
	here := GeoPoint{Lat: -13.1631, Lng: -74.2237}
	return ToolListing{
		Title: "  Rotomartillo   Bosch 800 W ", Attributes: []byte(`{}`),
		ReplacementValue: 450_00, Deposit: 140_00, Prices: Prices{Day: 35_00},
		Accessories:   []string{" Maletín ", "maletín", "", "Broca de 10 mm"},
		PickupEnabled: true, PickupLocation: &here, PublicLocation: &here, PublicRadiusM: 500, ZoneID: "z1",
		BookingMode: BookingOnRequest, CancelPolicy: CancelModerate, MinVerification: 1,
		MinNoticeHours: 12, MinDurationHours: 24, MaxDurationHours: 720,
	}
}

func TestNormalizeListing(t *testing.T) {
	l, err := NormalizeListing(validListing())
	if err != nil {
		t.Fatal(err)
	}
	if l.Title != "Rotomartillo Bosch 800 W" {
		t.Fatalf("título = %q", l.Title)
	}
	if strings.Join(l.Accessories, "|") != "Maletín|Broca de 10 mm" {
		t.Fatalf("accesorios sin vacíos ni repetidos: %q", l.Accessories)
	}

	tests := []struct {
		name   string
		change func(*ToolListing)
		want   error
	}{
		{"título corto", func(l *ToolListing) { l.Title = "Tal" }, ErrListingTitle},
		{"título largo", func(l *ToolListing) { l.Title = strings.Repeat("a", 81) }, ErrListingTitle},
		{"descripción larga", func(l *ToolListing) { l.Description = strings.Repeat("a", 2001) }, ErrListingText},
		{"precio negativo", func(l *ToolListing) { l.Prices.Week = -1 }, ErrListingAmount},
		{"monto absurdo", func(l *ToolListing) { l.ReplacementValue = 100_000_01 }, ErrListingAmount},
		{"modo de reserva", func(l *ToolListing) { l.BookingMode = "ya" }, ErrListingBookingMode},
		{"política", func(l *ToolListing) { l.CancelPolicy = "suave" }, ErrListingCancelPolicy},
		{"verificación", func(l *ToolListing) { l.MinVerification = 3 }, ErrListingVerification},
		{"máxima menor que mínima", func(l *ToolListing) { l.MaxDurationHours = 12 }, ErrListingDurations},
		{"antelación", func(l *ToolListing) { l.MinNoticeHours = 169 }, ErrListingDurations},
		{"accesorio largo", func(l *ToolListing) { l.Accessories = []string{strings.Repeat("a", 81)} }, ErrListingAccessories},
		{"punto fuera del mundo", func(l *ToolListing) { l.PickupLocation = &GeoPoint{Lat: 91} }, ErrInvalidLocation},
	}
	for _, tt := range tests {
		l := validListing()
		tt.change(&l)
		if _, err := NormalizeListing(l); !errors.Is(err, tt.want) {
			t.Errorf("%s: quiero %v, llegó %v", tt.name, tt.want, err)
		}
	}
}

func TestCheckListingComplete(t *testing.T) {
	if err := CheckListingComplete(validListing(), 3); err != nil {
		t.Fatalf("completa: %v", err)
	}
	tests := []struct {
		name   string
		change func(*ToolListing)
		photos int
		want   error
	}{
		{"sin precio por día", func(l *ToolListing) { l.Prices.Day = 0 }, 3, ErrListingDayPrice},
		{"sin valor", func(l *ToolListing) { l.ReplacementValue = 0 }, 3, ErrListingReplacementValue},
		{"sin entrega", func(l *ToolListing) { l.PickupEnabled = false }, 3, ErrListingFulfillment},
		{"recojo sin punto", func(l *ToolListing) { l.PickupLocation = nil }, 3, ErrListingPickupLocation},
		{"delivery sin distritos", func(l *ToolListing) { l.DeliveryEnabled = true }, 3, ErrListingDeliveryZones},
		{"pocas fotos", func(*ToolListing) {}, 2, ErrListingPhotos},
	}
	for _, tt := range tests {
		l := validListing()
		tt.change(&l)
		if err := CheckListingComplete(l, tt.photos); !errors.Is(err, tt.want) {
			t.Errorf("%s: quiero %v, llegó %v", tt.name, tt.want, err)
		}
	}
}

func TestSuggestDeposit(t *testing.T) {
	rule := DepositRule{PercentBps: 3000, MinFactorBps: 5000, MaxFactorBps: 15000}
	tests := []struct {
		name        string
		replacement Cents
		rule        DepositRule
		want        DepositRange
	}{
		// 30 % de S/ 450 = S/ 135 → S/ 140 (redondeo a S/ 10); rango 70 a 210.
		{"riesgo medio", 450_00, rule, DepositRange{Suggested: 140_00, Min: 70_00, Max: 210_00}},
		// 50 % de S/ 1 299,90 = S/ 649,95 → S/ 650.
		{"riesgo alto", 1_299_90, DepositRule{PercentBps: 5000, MinFactorBps: 5000, MaxFactorBps: 15000},
			DepositRange{Suggested: 650_00, Min: 330_00, Max: 980_00}},
		// 20 % de S/ 20 = S/ 4 → S/ 0: una herramienta barata puede ir sin garantía.
		{"valor bajo", 20_00, DepositRule{PercentBps: 2000, MinFactorBps: 5000, MaxFactorBps: 15000},
			DepositRange{Suggested: 0, Min: 0, Max: 0}},
	}
	for _, tt := range tests {
		if got := SuggestDeposit(tt.replacement, tt.rule); got != tt.want {
			t.Errorf("%s: %+v, quiero %+v", tt.name, got, tt.want)
		}
	}
	r := SuggestDeposit(450_00, rule)
	if !r.Contains(70_00) || !r.Contains(210_00) || r.Contains(69_99) || r.Contains(210_01) {
		t.Fatal("Contains incluye los extremos y nada más")
	}
}

func TestNormalizeRejectionReason(t *testing.T) {
	if r, err := NormalizeRejectionReason("  Fotos borrosas  "); err != nil || r != "Fotos borrosas" {
		t.Fatalf("%q, %v", r, err)
	}
	for _, raw := range []string{"", "   ", strings.Repeat("a", 501)} {
		if _, err := NormalizeRejectionReason(raw); !errors.Is(err, ErrRejectionReason) {
			t.Errorf("%q debe rechazarse", raw)
		}
	}
}

func TestCheckListingCategory(t *testing.T) {
	root := Category{ID: "construccion", Vertical: VerticalRental, Enabled: true}
	leaf := Category{ID: "rotomartillo", ParentID: "construccion", Vertical: VerticalRental, Enabled: true}
	if err := CheckListingCategory(leaf, root); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(leaf, root *Category)
		want   error
	}{
		{"tipo prohibido", func(l, _ *Category) { l.Prohibited = true }, ErrListingProhibited},
		{"raíz prohibida", func(_, r *Category) { r.Prohibited = true }, ErrListingProhibited},
		{"tipo apagado", func(l, _ *Category) { l.Enabled = false }, ErrListingCategory},
		{"raíz apagada", func(_, r *Category) { r.Enabled = false }, ErrListingCategory},
		{"una raíz no se publica", func(l, _ *Category) { l.ParentID = "" }, ErrListingCategory},
		{"un oficio no es herramienta", func(l, _ *Category) { l.Vertical = VerticalService }, ErrListingCategory},
		{"padre equivocado", func(l, _ *Category) { l.ParentID = "otra" }, ErrListingCategory},
	}
	for _, tt := range tests {
		l, r := leaf, root
		tt.change(&l, &r)
		if err := CheckListingCategory(l, r); !errors.Is(err, tt.want) {
			t.Errorf("%s: quiero %v, llegó %v", tt.name, tt.want, err)
		}
	}
}

func TestDepositRuleFromSnapshot(t *testing.T) {
	all := []Setting{
		{ID: "1", Key: "listings.deposit_high_bps", Value: []byte("5000")},
		{ID: "2", Key: SettingDepositMinFactor, Value: []byte("5000")},
		{ID: "3", Key: SettingDepositMaxFactor, Value: []byte("15000")},
		{ID: "4", Key: SettingPublicRadius, Value: []byte("500")},
	}
	snap, err := TakeSettingsSnapshot(all, ListingSettingKeys(RiskHigh), SettingScope{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rule, err := DepositRuleFrom(snap, RiskHigh)
	if err != nil || rule != (DepositRule{PercentBps: 5000, MinFactorBps: 5000, MaxFactorBps: 15000}) {
		t.Fatalf("DepositRuleFrom = %+v, %v", rule, err)
	}
	if _, err := DepositRuleFrom(snap, RiskLow); !errors.Is(err, ErrSettingMissing) {
		t.Fatal("otra categoría de riesgo necesita su propio %")
	}

	l := validListing() // valor S/ 450: con 50 % se sugieren S/ 230 (rango 120 a 350)
	for deposit, ok := range map[Cents]bool{230_00: true, 120_00: true, 350_00: true, 110_00: false, 360_00: false} {
		l.Deposit = deposit
		if err := CheckDeposit(l, rule); (err == nil) != ok {
			t.Errorf("garantía %d: %v", deposit, err)
		}
	}
	l.ReplacementValue = 0
	if CheckDeposit(l, rule) != nil {
		t.Fatal("sin valor de reposición aún no se revisa (borrador)")
	}
}

func TestDuplicateListing(t *testing.T) {
	now := time.Now()
	here := GeoPoint{Lat: -13.16, Lng: -74.22}
	l := validListing()
	l.ID, l.Status, l.Version, l.RejectionReason, l.PublicLocation, l.FirstPublishedAt = "l1", ListingPublished, 7, "x", &here, &now
	l.Title = strings.Repeat("a", ListingTitleMax)
	copy := DuplicateListing(l)
	if copy.ID != "" || copy.Status != ListingDraft || copy.Version != 0 || copy.PublicLocation != nil || copy.FirstPublishedAt != nil || copy.RejectionReason != "" {
		t.Fatalf("la copia es un borrador nuevo: %+v", copy)
	}
	if !strings.HasSuffix(copy.Title, " (copia)") || len([]rune(copy.Title)) != ListingTitleMax {
		t.Fatalf("título recortado para que quepa «(copia)»: %q", copy.Title)
	}
	copy.Accessories[0] = "cambiado"
	if l.Accessories[0] == "cambiado" {
		t.Fatal("la copia no comparte listas con el original")
	}
	if copy.PickupLocation != l.PickupLocation || copy.Prices != l.Prices {
		t.Fatal("conserva precios y punto de recojo")
	}
}

func TestApplyListingAction(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	rejected := validListing()
	rejected.Status, rejected.RejectionReason = ListingRejected, "Fotos borrosas"

	l, err := ApplyListingAction(rejected, ListingSubmit, true, now)
	if err != nil || l.Status != ListingInReview || l.RejectionReason != "" || l.FirstPublishedAt != nil {
		t.Fatalf("corregida vuelve a revisión sin el motivo viejo: %+v, %v", l, err)
	}
	l, err = ApplyListingAction(l, ListingApprove, false, now)
	if err != nil || l.Status != ListingPublished || l.FirstPublishedAt == nil || !l.FirstPublishedAt.Equal(now) {
		t.Fatalf("aprobada queda fechada: %+v, %v", l, err)
	}
	l.Status = ListingPaused
	again, _ := ApplyListingAction(l, ListingResume, false, now.Add(time.Hour))
	if !again.FirstPublishedAt.Equal(now) {
		t.Fatal("la fecha de la primera publicación no cambia")
	}
	if _, err := ApplyListingAction(l, ListingApprove, false, now); !errors.Is(err, ErrListingTransition) {
		t.Fatal("una pausada no se aprueba")
	}
	if rejected.RejectionReason != "Fotos borrosas" {
		t.Fatal("no modifica la publicación original")
	}
}
