package service

import (
	"context"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// reviewQueueLimit: la cola del piloto es corta; si crece se pagina.
const reviewQueueLimit = 100

// ModerationDeps agrupa lo que necesita ModerationService.
type ModerationDeps struct {
	Listings   port.ListingRepository
	Categories port.CategoryFinder
	Accounts   port.AccountRepository
	Photos     *PhotoService
	Mailer     port.Mailer
	Clock      port.Clock
}

// ReviewView es una publicación en revisión con lo necesario para decidir: categoría (y su riesgo),
// arrendador y fotos públicas. Nunca el punto exacto ni la foto de la placa.
type ReviewView struct {
	Item     domain.ReviewItem
	Category domain.Category
	Photos   []PhotoView
}

// ModerationService: moderadores y admins aprueban o rechazan con motivo las publicaciones en
// revisión (primera de cada arrendador y categorías de riesgo alto, spec 003). Queda en audit_log.
type ModerationService struct {
	d ModerationDeps
}

func NewModerationService(d ModerationDeps) *ModerationService { return &ModerationService{d: d} }

// Queue devuelve la cola, la que más espera primero.
func (s *ModerationService) Queue(ctx context.Context) ([]ReviewView, error) {
	items, err := s.d.Listings.ReviewQueue(ctx, reviewQueueLimit)
	if err != nil {
		return nil, err
	}
	out := make([]ReviewView, len(items))
	for i, item := range items {
		if out[i], err = s.view(ctx, item); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *ModerationService) view(ctx context.Context, item domain.ReviewItem) (ReviewView, error) {
	category, err := s.d.Categories.FindCategory(ctx, item.Listing.CategoryID)
	if err != nil {
		return ReviewView{}, err
	}
	photos, err := s.d.Photos.PublicPhotos(ctx, item.Listing.ID)
	if err != nil {
		return ReviewView{}, err
	}
	// La respuesta de moderación no lleva el punto exacto de recojo (dato privado del dueño).
	item.Listing.PickupLocation = nil
	return ReviewView{Item: item, Category: category, Photos: photos}, nil
}

// Approve publica la publicación.
func (s *ModerationService) Approve(ctx context.Context, moderatorID, listingID string, version int, ip string) (domain.ToolListing, error) {
	return s.decide(ctx, moderatorID, listingID, version, domain.ListingApprove, "", ip)
}

// Reject la devuelve al arrendador con un motivo que él lee (la corrige y vuelve a enviarla).
func (s *ModerationService) Reject(ctx context.Context, moderatorID, listingID string, version int, reason, ip string) (domain.ToolListing, error) {
	reason, err := domain.NormalizeRejectionReason(reason)
	if err != nil {
		return domain.ToolListing{}, err
	}
	return s.decide(ctx, moderatorID, listingID, version, domain.ListingReject, reason, ip)
}

func (s *ModerationService) decide(ctx context.Context, moderatorID, listingID string, version int,
	action domain.ListingAction, reason, ip string) (domain.ToolListing, error) {
	current, err := s.d.Listings.FindListing(ctx, listingID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if current.OwnerID == moderatorID {
		return domain.ToolListing{}, domain.ErrSelfModeration
	}
	next, err := domain.ApplyListingAction(current, action, false, s.d.Clock.Now())
	if err != nil {
		return domain.ToolListing{}, err
	}
	auditAction := domain.AuditListingApproved
	if action == domain.ListingReject {
		next.RejectionReason, auditAction = reason, domain.AuditListingRejected
	}
	saved, err := s.d.Listings.UpdateListing(ctx, next, version, domain.AuditEntry{
		ActorID: moderatorID, Action: auditAction, Entity: "listing", EntityID: listingID,
		Before: map[string]any{"status": current.Status}, After: map[string]any{"status": next.Status, "reason": reason}, IP: ip,
	})
	if err != nil {
		return domain.ToolListing{}, err
	}
	s.notify(ctx, saved)
	return saved, nil
}

// notify avisa al arrendador por correo. Es un aviso: si el correo falla, la decisión ya quedó
// guardada y el arrendador la ve en «Mis publicaciones».
func (s *ModerationService) notify(ctx context.Context, l domain.ToolListing) {
	owner, err := s.d.Accounts.FindUser(ctx, l.OwnerID)
	if err != nil || owner.Email == "" {
		return
	}
	_ = s.d.Mailer.SendListingReview(ctx, port.ListingReviewEmail{
		To: owner.Email, Name: owner.Name, Title: l.Title,
		Approved: l.Status == domain.ListingPublished, Reason: l.RejectionReason,
	})
}
