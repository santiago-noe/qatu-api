package domain

import (
	"errors"
	"testing"
)

func TestNormalizePeruMobile(t *testing.T) {
	for _, raw := range []string{"987654321", "987 654 321", "987-654-321", "+51 987 654 321", "51987654321", "(+51) 987.654.321", "0987654321"} {
		if got, err := NormalizePeruMobile(raw); err != nil || got != "+51987654321" {
			t.Errorf("%q = %q, %v", raw, got, err)
		}
	}
	// Fijos, cortos, con letras o de otro país.
	for _, raw := range []string{"", "066312345", "98765432", "9876543210", "98765432a", "+1 987654321", "+56 987654321"} {
		if _, err := NormalizePeruMobile(raw); !errors.Is(err, ErrLenderPhone) {
			t.Errorf("%q debe rechazarse", raw)
		}
	}
}

func TestNormalizeLenderProfile(t *testing.T) {
	base := LenderProfile{Kind: LenderPerson, Phone: "987 654 321", CityID: "c", ZoneID: "z", BusinessName: "ignorado"}
	p, err := NormalizeLenderProfile(base)
	if err != nil || p.Phone != "+51987654321" || p.BusinessName != "" {
		t.Fatalf("persona = %+v, %v", p, err)
	}

	biz := base
	biz.Kind, biz.BusinessName = LenderBusiness, "  Ferretería   El Maestro "
	if p, err := NormalizeLenderProfile(biz); err != nil || p.BusinessName != "Ferretería El Maestro" {
		t.Fatalf("negocio = %+v, %v", p, err)
	}
	biz.BusinessName = "X"
	if _, err := NormalizeLenderProfile(biz); !errors.Is(err, ErrLenderBusinessName) {
		t.Fatal("un negocio necesita nombre")
	}
	noZone := base
	noZone.ZoneID = ""
	if _, err := NormalizeLenderProfile(noZone); !errors.Is(err, ErrInvalidLocation) {
		t.Fatal("el distrito es obligatorio")
	}
}
