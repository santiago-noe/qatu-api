package port

import (
	"context"
	"io"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Bucket separa lo que se sirve sin credenciales (fotos públicas procesadas) de lo privado
// (originales con EXIF y la foto de la placa).
type Bucket string

const (
	BucketPublic  Bucket = "public"
	BucketPrivate Bucket = "private"
)

// PresignedUpload es la subida directa del navegador al almacenamiento: un PUT a URL con esas
// cabeceras exactas, antes de ExpiresAt.
type PresignedUpload struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// ObjectStorage guarda archivos en un almacenamiento S3 (SeaweedFS en local, Cloudflare R2 en
// producción). Implementación: minio-go.
type ObjectStorage interface {
	PresignPut(ctx context.Context, bucket Bucket, key, contentType string, ttl time.Duration) (PresignedUpload, error)
	// Stat devuelve el tamaño del objeto, o domain.ErrNotFound si no existe.
	Stat(ctx context.Context, bucket Bucket, key string) (int64, error)
	Get(ctx context.Context, bucket Bucket, key string) (io.ReadCloser, error)
	Put(ctx context.Context, bucket Bucket, key string, data []byte, contentType string) error
	// Delete borra las claves; las que no existen se ignoran.
	Delete(ctx context.Context, bucket Bucket, keys ...string) error
	PresignGet(ctx context.Context, bucket Bucket, key string, ttl time.Duration) (string, error)
	// PublicURL es la dirección permanente de un objeto del bucket público.
	PublicURL(key string) string
}

// PhotoRepository guarda las filas de listing_photos. Implementación: Postgres.
type PhotoRepository interface {
	// ListPhotos devuelve las fotos de la publicación: públicas en su orden y luego la placa.
	ListPhotos(ctx context.Context, listingID string) ([]domain.ListingPhoto, error)
	// FindPhoto devuelve domain.ErrNotFound si no existe.
	FindPhoto(ctx context.Context, photoID string) (domain.ListingPhoto, error)
	// CreatePhoto la agrega al final de las públicas.
	CreatePhoto(ctx context.Context, p domain.ListingPhoto) error
	MarkPhotoReady(ctx context.Context, photoID string, width, height int) error
	MarkPhotoFailed(ctx context.Context, photoID string) error
	DeletePhoto(ctx context.Context, photoID string) error
	// ReorderPhotos pone sort_order según la posición de cada ID.
	ReorderPhotos(ctx context.Context, listingID string, ids []string) error
}

// PhotoJobs encola el procesamiento de una foto recién subida. Implementación: asynq (Redis).
type PhotoJobs interface {
	EnqueuePhoto(ctx context.Context, photoID string) error
}

// ProcessedImage es una foto ya lista: sus dimensiones reales y un JPEG por cada ancho pedido.
type ProcessedImage struct {
	Width    int
	Height   int
	Variants map[int][]byte
}

// ImageProcessor lee una imagen (JPEG, PNG o WebP), la endereza según su EXIF y genera los tamaños
// sin metadatos. Devuelve domain.ErrPhotoInvalid si no es una imagen válida o es demasiado grande.
type ImageProcessor interface {
	Process(data []byte, widths []int, maxPixels int) (ProcessedImage, error)
}
