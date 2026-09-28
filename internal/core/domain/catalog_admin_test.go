package domain

import (
	"errors"
	"strings"
	"testing"
)

func validCategory() Category {
	return Category{Vertical: VerticalRental, Slug: "rotomartillo", Name: "  Rotomartillo   SDS ", Icon: "Hammer", RiskLevel: RiskMedium}
}

func TestNormalizeCategory(t *testing.T) {
	c, err := NormalizeCategory(validCategory())
	if err != nil || c.Name != "Rotomartillo SDS" {
		t.Fatalf("NormalizeCategory = %+v, %v", c, err)
	}

	tests := []struct {
		name   string
		change func(*Category)
		want   error
	}{
		{"slug con mayúsculas", func(c *Category) { c.Slug = "Roto" }, ErrInvalidSlug},
		{"slug con espacios internos", func(c *Category) { c.Slug = "roto martillo" }, ErrInvalidSlug},
		{"nombre vacío", func(c *Category) { c.Name = "   " }, ErrInvalidName},
		{"nombre largo", func(c *Category) { c.Name = strings.Repeat("a", 81) }, ErrInvalidName},
		{"descripción larga", func(c *Category) { c.Description = strings.Repeat("a", 281) }, ErrInvalidDescription},
		{"ícono que no es de lucide", func(c *Category) { c.Icon = "<svg>" }, ErrInvalidIcon},
		{"riesgo desconocido", func(c *Category) { c.RiskLevel = "extreme" }, ErrInvalidRisk},
		{"orden negativo", func(c *Category) { c.SortOrder = -1 }, ErrInvalidSortOrder},
		{"vertical desconocida", func(c *Category) { c.Vertical = "vehicles" }, ErrInvalidVertical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validCategory()
			tt.change(&c)
			if _, err := NormalizeCategory(c); !errors.Is(err, tt.want) {
				t.Fatalf("se esperaba %v, llegó %v", tt.want, err)
			}
		})
	}
}

func TestNormalizeSettingValue(t *testing.T) {
	v, err := NormalizeSettingValue("rental.owner_commission_bps", []byte(" 1000 "))
	if err != nil || string(v) != "1000" {
		t.Fatalf("NormalizeSettingValue = %s, %v", v, err)
	}
	for _, raw := range []string{"10.5", "-1", "10001", `"1000"`, "1e3", "null", "{}"} {
		if _, err := NormalizeSettingValue("rental.owner_commission_bps", []byte(raw)); !errors.Is(err, ErrInvalidSettingValue) {
			t.Fatalf("%s debe rechazarse, llegó %v", raw, err)
		}
	}
	if _, err := NormalizeSettingValue("rental.owner_comision_bps", []byte("1000")); !errors.Is(err, ErrUnknownSetting) {
		t.Fatalf("una clave mal escrita se rechaza, llegó %v", err)
	}
	if !IsKnownSetting("service.client_service_fee_bps") || IsKnownSetting("otra.cosa") {
		t.Fatal("IsKnownSetting")
	}
}
