package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestCheckPhotoUpload(t *testing.T) {
	for _, ct := range []string{"image/jpeg", "image/png", "image/webp"} {
		if err := CheckPhotoUpload(PhotoPublic, ct, 3<<20); err != nil {
			t.Errorf("%s: %v", ct, err)
		}
	}
	for name, tt := range map[string]struct {
		kind PhotoKind
		ct   string
		size int64
	}{
		"gif":          {PhotoPublic, "image/gif", 1000},
		"heic":         {PhotoPublic, "image/heic", 1000},
		"vacía":        {PhotoPublic, "image/jpeg", 0},
		"muy grande":   {PhotoPublic, "image/jpeg", MaxPhotoBytes + 1},
		"tipo de foto": {"otra", "image/jpeg", 1000},
	} {
		if err := CheckPhotoUpload(tt.kind, tt.ct, tt.size); !errors.Is(err, ErrPhotoType) {
			t.Errorf("%s: quiero ErrPhotoType, llegó %v", name, err)
		}
	}
}

func photos(n int, status PhotoStatus) []ListingPhoto {
	out := make([]ListingPhoto, n)
	for i := range out {
		out[i] = ListingPhoto{ID: fmt.Sprintf("p%d", i), Kind: PhotoPublic, Status: status}
	}
	return out
}

func TestCheckPhotoSlot(t *testing.T) {
	if err := CheckPhotoSlot(photos(11, PhotoReady), PhotoPublic); err != nil {
		t.Fatal("la número 12 entra")
	}
	if err := CheckPhotoSlot(photos(12, PhotoPending), PhotoPublic); !errors.Is(err, ErrPhotoLimit) {
		t.Fatal("la 13 no (las pendientes cuentan)")
	}
	if err := CheckPhotoSlot(append(photos(11, PhotoReady), photos(5, PhotoFailed)...), PhotoPublic); err != nil {
		t.Fatal("las fallidas no ocupan lugar")
	}
	if err := CheckPhotoSlot(photos(12, PhotoReady), PhotoSerial); err != nil {
		t.Fatal("la placa no cuenta entre las públicas")
	}
}

func TestReadyPublicPhotos(t *testing.T) {
	all := append(photos(2, PhotoReady), photos(1, PhotoPending)...)
	all = append(all, ListingPhoto{Kind: PhotoSerial, Status: PhotoReady})
	if n := ReadyPublicPhotos(all); n != 2 {
		t.Fatalf("solo públicas y listas: %d", n)
	}
}

func TestCheckPhotoOrder(t *testing.T) {
	existing := append(photos(3, PhotoReady), ListingPhoto{ID: "placa", Kind: PhotoSerial})
	if err := CheckPhotoOrder(existing, []string{"p2", "p0", "p1"}); err != nil {
		t.Fatal(err)
	}
	for name, ids := range map[string][]string{
		"falta una":    {"p2", "p0"},
		"repetida":     {"p2", "p0", "p0"},
		"ajena":        {"p2", "p0", "x"},
		"con la placa": {"p2", "p0", "p1", "placa"},
	} {
		if err := CheckPhotoOrder(existing, ids); !errors.Is(err, ErrPhotoOrder) {
			t.Errorf("%s: quiero ErrPhotoOrder, llegó %v", name, err)
		}
	}
}

func TestPhotoKeys(t *testing.T) {
	if k := OriginalPhotoKey("l1", "p1"); k != "originals/l1/p1" {
		t.Fatal(k)
	}
	if k := PhotoVariantKey("l1", "p1", 800); k != "listings/l1/p1/800.jpg" {
		t.Fatal(k)
	}
}
