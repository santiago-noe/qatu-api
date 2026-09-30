package port

import (
	"context"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// LenderActivation crea o actualiza el perfil de arrendador en una transacción: el perfil, el rol
// lender, el consentimiento de las condiciones (si es la primera vez) y la auditoría.
type LenderActivation struct {
	Profile domain.LenderProfile
	Consent *domain.Consent // nil al solo editar el perfil
	Audit   domain.AuditEntry
}

// LenderRepository guarda los perfiles de arrendador. Implementación: Postgres.
type LenderRepository interface {
	// FindLenderProfile devuelve domain.ErrNotFound si la persona aún no es arrendadora.
	FindLenderProfile(ctx context.Context, userID string) (domain.LenderProfile, error)
	SaveLenderProfile(ctx context.Context, a LenderActivation) error
}

// ListingRepository guarda las publicaciones de herramientas y su calendario. Implementación: Postgres.
type ListingRepository interface {
	// CreateListing inserta con el ID ya generado (el punto público depende de él).
	CreateListing(ctx context.Context, l domain.ToolListing, audit domain.AuditEntry) error
	// FindListing devuelve la publicación con sus distritos de delivery, o domain.ErrNotFound.
	FindListing(ctx context.Context, id string) (domain.ToolListing, error)
	// ListOwnerListings devuelve las del dueño, de la más reciente a la más antigua.
	ListOwnerListings(ctx context.Context, ownerID string) ([]domain.ToolListing, error)
	// UpdateListing guarda todo si la versión sigue siendo expectedVersion (bloqueo optimista) y
	// devuelve la guardada; domain.ErrListingVersion si alguien la cambió antes.
	UpdateListing(ctx context.Context, l domain.ToolListing, expectedVersion int, audit domain.AuditEntry) (domain.ToolListing, error)
	// OwnerHasPublished indica si el dueño ya tuvo alguna publicación aprobada (sin revisión obligatoria).
	OwnerHasPublished(ctx context.Context, ownerID string) (bool, error)
	// ReadyPhotos cuenta las fotos públicas ya procesadas (la placa privada no cuenta).
	ReadyPhotos(ctx context.Context, listingID string) (int, error)

	// ListBlocks devuelve los bloqueos que tocan [from, to), en orden.
	ListBlocks(ctx context.Context, listingID string, from, to time.Time) ([]domain.AvailabilityBlock, error)
	// AddBlock devuelve el ID; domain.ErrAvailabilityOverlap si se cruza con otro bloqueo o reserva.
	AddBlock(ctx context.Context, b domain.AvailabilityBlock, audit domain.AuditEntry) (string, error)
	// DeleteManualBlock borra un bloqueo manual de la publicación; domain.ErrNotFound si no existe
	// o no es manual (las reservas no se borran desde el calendario).
	DeleteManualBlock(ctx context.Context, listingID, blockID string, audit domain.AuditEntry) error
}

// CategoryFinder busca una categoría aunque esté apagada o prohibida (para explicar por qué no se
// puede publicar en ella). Implementación: Postgres.
type CategoryFinder interface {
	FindCategory(ctx context.Context, id string) (domain.Category, error)
}

// AttributesValidator valida los atributos de una publicación con el JSON Schema de su categoría.
type AttributesValidator interface {
	// Validate devuelve domain.ErrListingAttributes si el documento no cumple el esquema.
	Validate(schema, doc []byte) error
}
