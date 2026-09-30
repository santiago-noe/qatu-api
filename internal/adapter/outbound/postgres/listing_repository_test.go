package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

func TestLenderRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewLenderRepository(db)
	ctx := context.Background()

	var userID, cityID, zoneID string
	must(t, db.Pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('ana@qatu.pe', 'Ana') RETURNING id`).Scan(&userID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id, city_id FROM zones WHERE slug = 'carmen-alto'`).Scan(&zoneID, &cityID))

	if _, err := repo.FindLenderProfile(ctx, userID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("aún no es arrendadora: %v", err)
	}
	profile := domain.LenderProfile{UserID: userID, Kind: domain.LenderPerson, Phone: "+51987654321", CityID: cityID, ZoneID: zoneID}
	consent := &domain.Consent{Purpose: domain.ConsentLenderTerms, Version: "v1", GrantedAt: time.Now()}
	must(t, repo.SaveLenderProfile(ctx, port.LenderActivation{Profile: profile, Consent: consent,
		Audit: domain.AuditEntry{ActorID: userID, Action: domain.AuditLenderActivated, Entity: "lender_profile", IP: "190.1.2.3"}}))

	got, err := repo.FindLenderProfile(ctx, userID)
	if err != nil || got.Phone != "+51987654321" || got.ZoneID != zoneID {
		t.Fatalf("FindLenderProfile = %+v, %v", got, err)
	}
	var roles, consents int
	must(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM user_roles WHERE user_id = $1 AND role = 'lender'`, userID).Scan(&roles))
	must(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM consents WHERE user_id = $1 AND purpose = 'lender_terms'`, userID).Scan(&consents))
	if roles != 1 || consents != 1 {
		t.Fatalf("rol lender y consentimiento: %d %d", roles, consents)
	}

	// Editar a negocio: sin consentimiento nuevo y sin duplicar el rol.
	profile.Kind, profile.BusinessName = domain.LenderBusiness, "Ferretería El Maestro"
	must(t, repo.SaveLenderProfile(ctx, port.LenderActivation{Profile: profile,
		Audit: domain.AuditEntry{ActorID: userID, Action: domain.AuditLenderUpdated, Entity: "lender_profile"}}))
	got, _ = repo.FindLenderProfile(ctx, userID)
	must(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM consents WHERE user_id = $1`, userID).Scan(&consents))
	if got.BusinessName != "Ferretería El Maestro" || consents != 1 {
		t.Fatalf("editar: %+v, %d consentimientos", got, consents)
	}
}

func TestListingRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewListingRepository(db)
	ctx := context.Background()

	var ownerID, categoryID, cityID, zoneID, otherZone string
	must(t, db.Pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('ana@qatu.pe', 'Ana') RETURNING id`).Scan(&ownerID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'rotomartillo'`).Scan(&categoryID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id, city_id FROM zones WHERE slug = 'ayacucho'`).Scan(&zoneID, &cityID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM zones WHERE slug = 'carmen-alto'`).Scan(&otherZone))

	exact := domain.GeoPoint{Lat: -13.1631, Lng: -74.2237}
	public := domain.GeoPoint{Lat: -13.1600, Lng: -74.2210}
	l := domain.ToolListing{
		ID: "11111111-1111-1111-1111-111111111111", OwnerID: ownerID, CategoryID: categoryID, CityID: cityID, ZoneID: zoneID,
		Title: "Rotomartillo Bosch", Attributes: json.RawMessage(`{"marca":"Bosch"}`), ReplacementValue: 450_00,
		Deposit: 140_00, Prices: domain.Prices{Day: 35_00, Week: 180_00}, Accessories: []string{"Maletín"},
		PickupEnabled: true, PickupLocation: &exact, PublicLocation: &public, PublicRadiusM: 500,
		DeliveryEnabled: true, DeliveryFee: 10_00, DeliveryZoneIDs: []string{otherZone, zoneID},
		BookingMode: domain.BookingOnRequest, CancelPolicy: domain.CancelModerate, MinVerification: 1,
		MinNoticeHours: 12, MinDurationHours: 24, MaxDurationHours: 720, Status: domain.ListingDraft,
	}
	audit := domain.AuditEntry{ActorID: ownerID, Action: domain.AuditListingCreated, Entity: "listing", EntityID: l.ID}
	must(t, repo.CreateListing(ctx, l, audit))

	got, err := repo.FindListing(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Prices != l.Prices || got.Deposit != 140_00 || got.PublicRadiusM != 500 ||
		*got.PickupLocation != exact || *got.PublicLocation != public || len(got.DeliveryZoneIDs) != 2 ||
		!slices.Equal(got.Accessories, []string{"Maletín"}) || got.Prices.Hour != 0 {
		t.Fatalf("ida y vuelta: %+v", got)
	}

	t.Run("versión optimista y distritos de delivery", func(t *testing.T) {
		next := got
		next.Title, next.DeliveryZoneIDs = "Rotomartillo Bosch 800 W", []string{zoneID}
		if _, err := repo.UpdateListing(ctx, next, got.Version+1, audit); !errors.Is(err, domain.ErrListingVersion) {
			t.Fatalf("versión vieja: %v", err)
		}
		saved, err := repo.UpdateListing(ctx, next, got.Version, audit)
		if err != nil || saved.Version != 2 || saved.Title != "Rotomartillo Bosch 800 W" || len(saved.DeliveryZoneIDs) != 1 {
			t.Fatalf("UpdateListing = %+v, %v", saved, err)
		}
		missing := next
		missing.ID = "22222222-2222-2222-2222-222222222222"
		if _, err := repo.UpdateListing(ctx, missing, 1, audit); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("inexistente: %v", err)
		}
		// Sin recojo ni delivery: la columna vuelve a NULL y la lista se vacía.
		next = saved
		next.PickupEnabled, next.PickupLocation, next.PublicLocation, next.PublicRadiusM = false, nil, nil, 0
		next.DeliveryZoneIDs = nil
		if saved, err = repo.UpdateListing(ctx, next, saved.Version, audit); err != nil || saved.PickupLocation != nil || len(saved.DeliveryZoneIDs) != 0 {
			t.Fatalf("quitar recojo y distritos: %+v, %v", saved, err)
		}
	})

	t.Run("fotos listas y primera publicación", func(t *testing.T) {
		must(t, execErr(db, ctx, `INSERT INTO listing_photos (listing_id, kind, object_key, status) VALUES
			($1, 'public', 'a', 'ready'), ($1, 'public', 'b', 'pending'), ($1, 'serial', 'c', 'ready')`, l.ID))
		if n, err := repo.ReadyPhotos(ctx, l.ID); err != nil || n != 1 {
			t.Fatalf("solo cuentan las públicas procesadas: %d, %v", n, err)
		}
		if published, _ := repo.OwnerHasPublished(ctx, ownerID); published {
			t.Fatal("un borrador no cuenta como publicada")
		}
		must(t, execErr(db, ctx, `UPDATE tool_listings SET first_published_at = now() WHERE id = $1`, l.ID))
		if published, _ := repo.OwnerHasPublished(ctx, ownerID); !published {
			t.Fatal("ya tuvo una publicada")
		}
		if list, err := repo.ListOwnerListings(ctx, ownerID); err != nil || len(list) != 1 {
			t.Fatalf("ListOwnerListings = %d, %v", len(list), err)
		}
	})

	t.Run("calendario", func(t *testing.T) {
		start := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
		block := domain.AvailabilityBlock{ListingID: l.ID, Start: start, End: start.Add(48 * time.Hour), Reason: domain.BlockManual,
			Note: "taller", CreatedBy: ownerID}
		id, err := repo.AddBlock(ctx, block, audit)
		if err != nil {
			t.Fatal(err)
		}
		overlap := block
		overlap.Start = start.Add(24 * time.Hour)
		overlap.End = start.Add(72 * time.Hour)
		if _, err := repo.AddBlock(ctx, overlap, audit); !errors.Is(err, domain.ErrAvailabilityOverlap) {
			t.Fatalf("fechas que se cruzan: %v", err)
		}
		booking := block
		booking.Start, booking.End, booking.Reason = start.Add(48*time.Hour), start.Add(96*time.Hour), domain.BlockBooking
		bookingID, err := repo.AddBlock(ctx, booking, audit)
		if err != nil {
			t.Fatalf("contiguo sí: %v", err)
		}
		blocks, err := repo.ListBlocks(ctx, l.ID, start.Add(-time.Hour), start.Add(30*24*time.Hour))
		if err != nil || len(blocks) != 2 || blocks[0].ID != id || !blocks[0].Start.Equal(start) || blocks[0].Note != "taller" {
			t.Fatalf("ListBlocks = %+v, %v", blocks, err)
		}
		if err := repo.DeleteManualBlock(ctx, l.ID, bookingID, audit); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("una reserva no se borra desde el calendario: %v", err)
		}
		must(t, repo.DeleteManualBlock(ctx, l.ID, id, audit))
		if blocks, _ := repo.ListBlocks(ctx, l.ID, start, start.Add(30*24*time.Hour)); len(blocks) != 1 {
			t.Fatalf("queda la reserva: %+v", blocks)
		}
	})

	t.Run("la base traduce sus rechazos", func(t *testing.T) {
		var trade string
		must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'gasfiteria'`).Scan(&trade))
		bad := l
		bad.ID, bad.CategoryID = "33333333-3333-3333-3333-333333333333", trade
		if err := repo.CreateListing(ctx, bad, audit); !errors.Is(err, domain.ErrListingCategory) {
			t.Fatalf("un oficio no es una herramienta: %v", err)
		}
		if _, err := repo.FindListing(ctx, "no-es-uuid"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("un ID que no es UUID: %v", err)
		}
	})
}
