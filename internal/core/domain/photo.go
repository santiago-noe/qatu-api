package domain

import (
	"fmt"
	"slices"
	"time"
)

// PhotoKind separa las fotos que ve el público de la foto privada de la placa o número de serie,
// que solo ven el dueño y soporte (disputas).
type PhotoKind string

const (
	PhotoPublic PhotoKind = "public"
	PhotoSerial PhotoKind = "serial"
)

// PhotoStatus: pending mientras se sube o se procesa; ready con sus tamaños listos; failed si el
// archivo no era una imagen válida.
type PhotoStatus string

const (
	PhotoPending PhotoStatus = "pending"
	PhotoReady   PhotoStatus = "ready"
	PhotoFailed  PhotoStatus = "failed"
)

// Límites de las fotos (spec 003).
const (
	// MaxPhotoBytes: una foto de celular pesa de 2 a 6 MB.
	MaxPhotoBytes = 10 << 20
	// MaxPhotoPixels evita "bombas" de descompresión: una imagen chica en bytes pero enorme en píxeles.
	MaxPhotoPixels = 50_000_000
	// PhotoUploadTTL: tiempo para subir el archivo con la URL firmada.
	PhotoUploadTTL = 15 * time.Minute
	// SerialPhotoURLTTL: la foto de la placa se ve con una URL firmada de corta duración.
	SerialPhotoURLTTL = 5 * time.Minute
)

// PhotoSizes son los anchos que se generan (miniatura, ficha y pantalla completa). Nunca se amplía.
var PhotoSizes = []int{320, 800, 1600}

var photoContentTypes = []string{"image/jpeg", "image/png", "image/webp"}

// ListingPhoto es una foto de una publicación. ObjectKey es el original recién subido (privado,
// con sus metadatos EXIF); se borra al procesarlo.
type ListingPhoto struct {
	ID        string
	ListingID string
	Kind      PhotoKind
	ObjectKey string
	Status    PhotoStatus
	Width     int
	Height    int
	SortOrder int
	CreatedAt time.Time
}

// CheckPhotoUpload valida lo que el navegador dice que va a subir; al terminar se revisa el
// archivo real (tamaño en el almacenamiento y contenido al procesarlo).
func CheckPhotoUpload(kind PhotoKind, contentType string, size int64) error {
	if kind != PhotoPublic && kind != PhotoSerial {
		return ErrPhotoType
	}
	if !slices.Contains(photoContentTypes, contentType) || size <= 0 || size > MaxPhotoBytes {
		return ErrPhotoType
	}
	return nil
}

// CheckPhotoSlot: hasta 12 fotos públicas (las fallidas no cuentan). La de la placa es una sola:
// subir otra reemplaza la anterior (lo decide el servicio).
func CheckPhotoSlot(existing []ListingPhoto, kind PhotoKind) error {
	if kind != PhotoPublic {
		return nil
	}
	n := 0
	for _, p := range existing {
		if p.Kind == PhotoPublic && p.Status != PhotoFailed {
			n++
		}
	}
	if n >= ListingPhotosMax {
		return ErrPhotoLimit
	}
	return nil
}

// ReadyPublicPhotos cuenta las fotos públicas procesadas (las que pide CheckListingComplete).
func ReadyPublicPhotos(photos []ListingPhoto) int {
	n := 0
	for _, p := range photos {
		if p.Kind == PhotoPublic && p.Status == PhotoReady {
			n++
		}
	}
	return n
}

// CheckPhotoOrder: el nuevo orden incluye cada foto pública exactamente una vez (la primera es la portada).
func CheckPhotoOrder(existing []ListingPhoto, ids []string) error {
	var public []string
	for _, p := range existing {
		if p.Kind == PhotoPublic {
			public = append(public, p.ID)
		}
	}
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	slices.Sort(public)
	if !slices.Equal(sorted, public) {
		return ErrPhotoOrder
	}
	return nil
}

// OriginalPhotoKey es donde el navegador sube el archivo (bucket privado).
func OriginalPhotoKey(listingID, photoID string) string {
	return fmt.Sprintf("originals/%s/%s", listingID, photoID)
}

// PhotoVariantKey es cada tamaño procesado: en el bucket público si la foto es pública y en el
// privado si es la de la placa.
func PhotoVariantKey(listingID, photoID string, width int) string {
	return fmt.Sprintf("listings/%s/%s/%d.jpg", listingID, photoID, width)
}
