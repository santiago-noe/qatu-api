package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func commission(id, city, category, value string) Setting {
	return Setting{ID: id, Key: "rental.owner_commission_bps", CityID: city, CategoryID: category, Value: json.RawMessage(value), Version: 1}
}

func TestResolveSetting(t *testing.T) {
	all := []Setting{
		commission("global", "", "", "1000"),
		commission("cusco", "cusco", "", "900"),
		commission("construccion", "", "construccion", "800"),
		commission("ayacucho-construccion", "ayacucho", "construccion", "700"),
		commission("rotomartillo", "", "rotomartillo", "600"),
		{ID: "otra-clave", Key: "rental.client_service_fee_bps", Value: json.RawMessage("500")},
	}
	tests := []struct {
		name  string
		scope SettingScope
		want  string
	}{
		{"sin alcance: global", SettingScope{}, "global"},
		{"otra ciudad sin ajuste: global", SettingScope{CityID: "lima"}, "global"},
		{"ciudad", SettingScope{CityID: "cusco", CategoryID: "taladro", ParentCategoryID: "electricas"}, "cusco"},
		{"la categoría padre gana a la ciudad", SettingScope{CityID: "cusco", CategoryID: "andamio", ParentCategoryID: "construccion"}, "construccion"},
		{"ciudad y padre", SettingScope{CityID: "ayacucho", CategoryID: "andamio", ParentCategoryID: "construccion"}, "ayacucho-construccion"},
		{"el tipo gana a todo lo demás", SettingScope{CityID: "ayacucho", CategoryID: "rotomartillo", ParentCategoryID: "construccion"}, "rotomartillo"},
	}
	for _, tt := range tests {
		got, ok := ResolveSetting(all, "rental.owner_commission_bps", tt.scope)
		if !ok || got.ID != tt.want {
			t.Errorf("%s: %s, quiero %s", tt.name, got.ID, tt.want)
		}
	}
	if _, ok := ResolveSetting(all[1:2], "rental.owner_commission_bps", SettingScope{CityID: "lima"}); ok {
		t.Fatal("un ajuste de otra ciudad no aplica")
	}
}

func TestTakeSettingsSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	all := []Setting{commission("global", "", "", "1000"), commission("ayacucho", "ayacucho", "", "900")}
	snap, err := TakeSettingsSnapshot(all, []string{"rental.owner_commission_bps"}, SettingScope{CityID: "ayacucho"}, now)
	if err != nil || !snap.TakenAt.Equal(now) || snap.Values["rental.owner_commission_bps"].SettingID != "ayacucho" {
		t.Fatalf("TakeSettingsSnapshot = %+v, %v", snap, err)
	}
	if v, err := snap.Int("rental.owner_commission_bps"); err != nil || v != 900 {
		t.Fatalf("Int = %d, %v", v, err)
	}

	// La copia no cambia aunque cambie el ajuste después (spec 002).
	all[1].Value = json.RawMessage("500")
	if v, _ := snap.Int("rental.owner_commission_bps"); v != 900 {
		t.Fatal("la copia es independiente del ajuste vivo")
	}

	if _, err := snap.Int("rental.client_service_fee_bps"); !errors.Is(err, ErrSettingMissing) {
		t.Fatal("una clave que no se copió")
	}
	if _, err := TakeSettingsSnapshot(all, []string{"rental.client_service_fee_bps"}, SettingScope{}, now); !errors.Is(err, ErrSettingMissing) {
		t.Fatal("sin valor global es un error de configuración, no un 0")
	}
	if _, err := TakeSettingsSnapshot(all, []string{"rental.comision"}, SettingScope{}, now); !errors.Is(err, ErrUnknownSetting) {
		t.Fatal("clave desconocida")
	}
}
