package service

import (
	"context"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// SettingsResolver da los ajustes que aplican a una ciudad y categoría. Lo usan la garantía
// sugerida (003) y la copia de precios de cada transacción (price_snapshot en 006 y 008): así un
// cambio de comisión solo afecta a lo que se cree después (spec 002).
type SettingsResolver struct {
	settings port.SettingsReader
	clock    port.Clock
}

func NewSettingsResolver(settings port.SettingsReader, clock port.Clock) *SettingsResolver {
	return &SettingsResolver{settings: settings, clock: clock}
}

// Snapshot copia los valores de las claves en el alcance de la ciudad y la categoría (con su padre).
func (r *SettingsResolver) Snapshot(ctx context.Context, cityID string, category domain.Category, keys ...string) (domain.SettingsSnapshot, error) {
	all, err := r.settings.ListSettings(ctx)
	if err != nil {
		return domain.SettingsSnapshot{}, err
	}
	scope := domain.SettingScope{CityID: cityID, CategoryID: category.ID, ParentCategoryID: category.ParentID}
	return domain.TakeSettingsSnapshot(all, keys, scope, r.clock.Now())
}
