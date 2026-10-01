package port

import (
	"context"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// ProviderRepository guarda los perfiles de proveedor con sus oficios, paquetes, cobertura,
// horario y días bloqueados. Implementación: Postgres.
type ProviderRepository interface {
	// FindProvider devuelve el perfil completo, o domain.ErrNotFound si la persona aún no es proveedora.
	FindProvider(ctx context.Context, userID string) (domain.ProviderProfile, error)
	// CreateProvider guarda el perfil nuevo (los paquetes ya traen su ID), el consentimiento de las
	// condiciones y la auditoría, en una transacción.
	CreateProvider(ctx context.Context, p domain.ProviderProfile, consent domain.Consent, audit domain.AuditEntry) error
	// UpdateProvider guarda todo si la versión sigue siendo expectedVersion (bloqueo optimista) y
	// devuelve lo guardado; domain.ErrProviderVersion si alguien lo cambió antes.
	UpdateProvider(ctx context.Context, p domain.ProviderProfile, expectedVersion int, audit domain.AuditEntry) (domain.ProviderProfile, error)

	// ListProviderBlocks devuelve los bloqueos que tocan [from, to), en orden.
	ListProviderBlocks(ctx context.Context, providerID string, from, to time.Time) ([]domain.AvailabilityBlock, error)
	// AddProviderBlock devuelve el ID; domain.ErrAvailabilityOverlap si se cruza con otro bloqueo o trabajo.
	AddProviderBlock(ctx context.Context, providerID string, b domain.AvailabilityBlock, audit domain.AuditEntry) (string, error)
	// DeleteProviderManualBlock borra un bloqueo manual; domain.ErrNotFound si no existe o no es manual.
	DeleteProviderManualBlock(ctx context.Context, providerID, blockID string, audit domain.AuditEntry) error
}
