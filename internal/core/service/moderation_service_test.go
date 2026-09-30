package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type moderationFixture struct {
	svc      *ModerationService
	listings *memoryListings
	photos   *memoryPhotos
	mailer   *capturingMailer
}

func newModerationFixture() moderationFixture {
	listings := newMemoryListings()
	here := plaza
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"nueva", "vieja"} {
		listings.listings[id] = domain.ToolListing{ID: id, OwnerID: "ana", CategoryID: "c1", Title: "Rotomartillo " + id,
			Status: domain.ListingInReview, Version: 3, PickupLocation: &here, UpdatedAt: now.Add(-time.Duration(i) * time.Hour)}
	}
	accounts := newMemoryAccounts()
	accounts.users["ana"] = domain.User{ID: "ana", Email: "ana@qatu.pe", Name: "Ana"}
	photos := &memoryPhotos{photos: map[string]domain.ListingPhoto{
		"p1": {ID: "p1", ListingID: "vieja", Kind: domain.PhotoPublic, Status: domain.PhotoReady},
		"p2": {ID: "p2", ListingID: "vieja", Kind: domain.PhotoSerial, Status: domain.PhotoReady},
		"p3": {ID: "p3", ListingID: "vieja", Kind: domain.PhotoPublic, Status: domain.PhotoPending},
	}}
	photoSvc := NewPhotoService(PhotoDeps{Listings: listings, Photos: photos, Storage: &memoryStorage{objects: map[string][]byte{}},
		Jobs: &recordingJobs{}, Images: fakeImages{}, IDs: &seqIDs{}, Clock: &fakeClock{now: now}})
	mailer := &capturingMailer{}
	svc := NewModerationService(ModerationDeps{Listings: listings, Categories: newMemoryCatalogAdmin(), Accounts: accounts,
		Photos: photoSvc, Mailer: mailer, Clock: &fakeClock{now: now}})
	return moderationFixture{svc: svc, listings: listings, photos: photos, mailer: mailer}
}

func TestModerationQueue(t *testing.T) {
	f := newModerationFixture()
	queue, err := f.svc.Queue(context.Background())
	if err != nil || len(queue) != 2 {
		t.Fatalf("Queue = %d, %v", len(queue), err)
	}
	first := queue[0]
	if first.Item.Listing.ID != "vieja" || !first.Item.FirstListing || first.Category.Name != "Construcción" {
		t.Fatalf("la que más espera primero, con su categoría: %+v", first.Item)
	}
	if first.Item.Listing.PickupLocation != nil {
		t.Fatal("moderación no ve el punto exacto")
	}
	if len(first.Photos) != 1 || first.Photos[0].Photo.ID != "p1" {
		t.Fatalf("solo fotos públicas listas (nunca la placa): %+v", first.Photos)
	}
}

func TestModerationDecisions(t *testing.T) {
	f := newModerationFixture()
	ctx := context.Background()

	if _, err := f.svc.Approve(ctx, "ana", "vieja", 3, ""); !errors.Is(err, domain.ErrSelfModeration) {
		t.Fatalf("nadie modera lo suyo: %v", err)
	}
	if _, err := f.svc.Approve(ctx, "mod-1", "vieja", 2, ""); !errors.Is(err, domain.ErrListingVersion) {
		t.Fatalf("otro moderador ya decidió: %v", err)
	}

	l, err := f.svc.Approve(ctx, "mod-1", "vieja", 3, "190.1.2.3")
	if err != nil || l.Status != domain.ListingPublished || l.FirstPublishedAt == nil {
		t.Fatalf("aprobada y fechada: %+v, %v", l, err)
	}
	audit := f.listings.audits[len(f.listings.audits)-1]
	if audit.Action != domain.AuditListingApproved || audit.ActorID != "mod-1" {
		t.Fatalf("la decisión se audita con el moderador: %+v", audit)
	}
	if len(f.mailer.reviews) != 1 || !f.mailer.reviews[0].Approved || f.mailer.reviews[0].To != "ana@qatu.pe" {
		t.Fatalf("aviso al arrendador: %+v", f.mailer.reviews)
	}
	if _, err := f.svc.Approve(ctx, "mod-1", "vieja", l.Version, ""); !errors.Is(err, domain.ErrListingTransition) {
		t.Fatal("una publicada no se vuelve a aprobar")
	}

	if _, err := f.svc.Reject(ctx, "mod-1", "nueva", 3, "   ", ""); !errors.Is(err, domain.ErrRejectionReason) {
		t.Fatalf("rechazar pide motivo: %v", err)
	}
	f.mailer.down = true // el correo falla: la decisión igual se guarda
	l, err = f.svc.Reject(ctx, "mod-1", "nueva", 3, "  Las fotos no muestran la herramienta. ", "")
	if err != nil || l.Status != domain.ListingRejected || l.RejectionReason != "Las fotos no muestran la herramienta." {
		t.Fatalf("rechazada con motivo: %+v, %v", l, err)
	}
	if f.listings.audits[len(f.listings.audits)-1].Action != domain.AuditListingRejected {
		t.Fatal("el rechazo se audita")
	}
	if _, err := f.svc.Approve(ctx, "mod-1", "no-existe", 1, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
}

func TestCleanupStalePhotos(t *testing.T) {
	f := newPhotoFixture()
	ctx := context.Background()
	old := time.Now().Add(-3 * time.Hour)
	f.photos.photos["vieja"] = domain.ListingPhoto{ID: "vieja", ListingID: "l1", Kind: domain.PhotoPublic,
		Status: domain.PhotoPending, ObjectKey: "originals/l1/vieja", CreatedAt: old}
	f.photos.photos["reciente"] = domain.ListingPhoto{ID: "reciente", ListingID: "l1", Kind: domain.PhotoPublic,
		Status: domain.PhotoPending, ObjectKey: "originals/l1/reciente", CreatedAt: time.Now()}
	f.photos.photos["lista"] = domain.ListingPhoto{ID: "lista", ListingID: "l1", Kind: domain.PhotoPublic,
		Status: domain.PhotoReady, CreatedAt: old}
	f.storage.objects["private:originals/l1/vieja"] = []byte("a medias")

	n, err := f.svc.CleanupStale(ctx)
	if err != nil || n != 1 {
		t.Fatalf("CleanupStale = %d, %v", n, err)
	}
	for id, want := range map[string]bool{"vieja": false, "reciente": true, "lista": true} {
		if _, ok := f.photos.photos[id]; ok != want {
			t.Errorf("%s: queda = %v", id, ok)
		}
	}
	for k := range f.storage.objects {
		if strings.Contains(k, "vieja") {
			t.Fatal("su original a medias también se borra")
		}
	}
}
