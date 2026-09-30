package service

import (
	"context"
	"errors"
	"io"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// PhotoDeps agrupa lo que necesita PhotoService.
type PhotoDeps struct {
	Listings port.ListingRepository
	Photos   port.PhotoRepository
	Storage  port.ObjectStorage
	Jobs     port.PhotoJobs
	Images   port.ImageProcessor
	IDs      port.IDGenerator
}

// PhotoView es una foto con la dirección de cada tamaño (vacío mientras se procesa).
type PhotoView struct {
	Photo domain.ListingPhoto
	URLs  map[int]string
}

// PhotoService: fotos de una publicación (spec 003). El navegador sube directo al almacenamiento
// con una URL firmada; un trabajo en segundo plano genera los tamaños sin EXIF y borra el original.
// La foto de la placa nunca es pública: se ve con una URL firmada de pocos minutos.
type PhotoService struct {
	d PhotoDeps
}

func NewPhotoService(d PhotoDeps) *PhotoService { return &PhotoService{d: d} }

// ownedListing devuelve la publicación si es del dueño (para cualquier otro no existe).
func (s *PhotoService) ownedListing(ctx context.Context, ownerID, listingID string) (domain.ToolListing, error) {
	l, err := s.d.Listings.FindListing(ctx, listingID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if l.OwnerID != ownerID {
		return domain.ToolListing{}, domain.ErrNotFound
	}
	return l, nil
}

func (s *PhotoService) editableListing(ctx context.Context, ownerID, listingID string) (domain.ToolListing, error) {
	l, err := s.ownedListing(ctx, ownerID, listingID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if !l.Status.Editable() {
		return domain.ToolListing{}, domain.ErrListingNotEditable
	}
	return l, nil
}

// ownedPhoto busca una foto de esa publicación.
func (s *PhotoService) ownedPhoto(ctx context.Context, listingID, photoID string) (domain.ListingPhoto, error) {
	p, err := s.d.Photos.FindPhoto(ctx, photoID)
	if err != nil {
		return domain.ListingPhoto{}, err
	}
	if p.ListingID != listingID {
		return domain.ListingPhoto{}, domain.ErrNotFound
	}
	return p, nil
}

// RequestUpload registra la foto (pendiente) y devuelve la URL firmada para subirla. Una foto de
// placa nueva reemplaza la anterior.
func (s *PhotoService) RequestUpload(ctx context.Context, ownerID, listingID string, kind domain.PhotoKind,
	contentType string, size int64) (domain.ListingPhoto, port.PresignedUpload, error) {
	if err := domain.CheckPhotoUpload(kind, contentType, size); err != nil {
		return domain.ListingPhoto{}, port.PresignedUpload{}, err
	}
	l, err := s.editableListing(ctx, ownerID, listingID)
	if err != nil {
		return domain.ListingPhoto{}, port.PresignedUpload{}, err
	}
	existing, err := s.d.Photos.ListPhotos(ctx, l.ID)
	if err != nil {
		return domain.ListingPhoto{}, port.PresignedUpload{}, err
	}
	if err := domain.CheckPhotoSlot(existing, kind); err != nil {
		return domain.ListingPhoto{}, port.PresignedUpload{}, err
	}
	if kind == domain.PhotoSerial {
		for _, old := range existing {
			if old.Kind == domain.PhotoSerial {
				if err := s.remove(ctx, old); err != nil {
					return domain.ListingPhoto{}, port.PresignedUpload{}, err
				}
			}
		}
	}
	id := s.d.IDs.NewID()
	photo := domain.ListingPhoto{ID: id, ListingID: l.ID, Kind: kind, ObjectKey: domain.OriginalPhotoKey(l.ID, id),
		Status: domain.PhotoPending}
	upload, err := s.d.Storage.PresignPut(ctx, port.BucketPrivate, photo.ObjectKey, contentType, domain.PhotoUploadTTL)
	if err != nil {
		return domain.ListingPhoto{}, port.PresignedUpload{}, err
	}
	if err := s.d.Photos.CreatePhoto(ctx, photo); err != nil {
		return domain.ListingPhoto{}, port.PresignedUpload{}, err
	}
	return photo, upload, nil
}

// CompleteUpload confirma que el archivo llegó y encola su procesamiento. Repetirlo no encola dos veces.
func (s *PhotoService) CompleteUpload(ctx context.Context, ownerID, listingID, photoID string) (domain.ListingPhoto, error) {
	if _, err := s.ownedListing(ctx, ownerID, listingID); err != nil {
		return domain.ListingPhoto{}, err
	}
	p, err := s.ownedPhoto(ctx, listingID, photoID)
	if err != nil || p.Status != domain.PhotoPending {
		return p, err
	}
	size, err := s.d.Storage.Stat(ctx, port.BucketPrivate, p.ObjectKey)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ListingPhoto{}, domain.ErrPhotoNotUploaded
	}
	if err != nil {
		return domain.ListingPhoto{}, err
	}
	// La URL firmada no limita el tamaño: se revisa aquí.
	if size > domain.MaxPhotoBytes {
		if err := s.fail(ctx, p); err != nil {
			return domain.ListingPhoto{}, err
		}
		return domain.ListingPhoto{}, domain.ErrPhotoType
	}
	return p, s.d.Jobs.EnqueuePhoto(ctx, p.ID)
}

// Process es el trabajo en segundo plano. Un archivo que no es imagen deja la foto fallida (sin
// reintentar); un error del almacenamiento se devuelve para que la cola reintente.
func (s *PhotoService) Process(ctx context.Context, photoID string) error {
	p, err := s.d.Photos.FindPhoto(ctx, photoID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil // la borraron antes de procesarla
	}
	if err != nil || p.Status != domain.PhotoPending {
		return err
	}
	data, err := s.readOriginal(ctx, p)
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrPhotoInvalid) {
		return s.fail(ctx, p)
	}
	if err != nil {
		return err
	}
	img, err := s.d.Images.Process(data, domain.PhotoSizes, domain.MaxPhotoPixels)
	if errors.Is(err, domain.ErrPhotoInvalid) {
		return s.fail(ctx, p)
	}
	if err != nil {
		return err
	}
	bucket := variantBucket(p.Kind)
	for width, jpg := range img.Variants {
		if err := s.d.Storage.Put(ctx, bucket, domain.PhotoVariantKey(p.ListingID, p.ID, width), jpg, "image/jpeg"); err != nil {
			return err
		}
	}
	if err := s.d.Photos.MarkPhotoReady(ctx, p.ID, img.Width, img.Height); errors.Is(err, domain.ErrNotFound) {
		// La borraron mientras se procesaba: no quedan archivos huérfanos.
		return s.d.Storage.Delete(ctx, bucket, variantKeys(p)...)
	} else if err != nil {
		return err
	}
	// El original tiene el EXIF (con la ubicación GPS): no se guarda.
	return s.d.Storage.Delete(ctx, port.BucketPrivate, p.ObjectKey)
}

func (s *PhotoService) readOriginal(ctx context.Context, p domain.ListingPhoto) ([]byte, error) {
	r, err := s.d.Storage.Get(ctx, port.BucketPrivate, p.ObjectKey)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, domain.MaxPhotoBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > domain.MaxPhotoBytes {
		return nil, domain.ErrPhotoInvalid
	}
	return data, nil
}

// fail marca la foto como fallida y borra el original.
func (s *PhotoService) fail(ctx context.Context, p domain.ListingPhoto) error {
	if err := s.d.Photos.MarkPhotoFailed(ctx, p.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return s.d.Storage.Delete(ctx, port.BucketPrivate, p.ObjectKey)
}

// List devuelve las fotos con sus direcciones: públicas permanentes; la placa, firmada y corta.
func (s *PhotoService) List(ctx context.Context, ownerID, listingID string) ([]PhotoView, error) {
	if _, err := s.ownedListing(ctx, ownerID, listingID); err != nil {
		return nil, err
	}
	photos, err := s.d.Photos.ListPhotos(ctx, listingID)
	if err != nil {
		return nil, err
	}
	out := make([]PhotoView, len(photos))
	for i, p := range photos {
		out[i] = PhotoView{Photo: p, URLs: map[int]string{}}
		if p.Status != domain.PhotoReady {
			continue
		}
		for _, width := range domain.PhotoSizes {
			key := domain.PhotoVariantKey(p.ListingID, p.ID, width)
			if p.Kind == domain.PhotoPublic {
				out[i].URLs[width] = s.d.Storage.PublicURL(key)
				continue
			}
			u, err := s.d.Storage.PresignGet(ctx, port.BucketPrivate, key, domain.SerialPhotoURLTTL)
			if err != nil {
				return nil, err
			}
			out[i].URLs[width] = u
		}
	}
	return out, nil
}

// Delete quita una foto. Una publicación visible no puede quedar con menos de 3 fotos públicas.
func (s *PhotoService) Delete(ctx context.Context, ownerID, listingID, photoID string) error {
	l, err := s.editableListing(ctx, ownerID, listingID)
	if err != nil {
		return err
	}
	p, err := s.ownedPhoto(ctx, listingID, photoID)
	if err != nil {
		return err
	}
	live := l.Status == domain.ListingPublished || l.Status == domain.ListingPaused
	if live && p.Kind == domain.PhotoPublic && p.Status == domain.PhotoReady {
		photos, err := s.d.Photos.ListPhotos(ctx, listingID)
		if err != nil {
			return err
		}
		if domain.ReadyPublicPhotos(photos)-1 < domain.ListingPhotosMin {
			return domain.ErrListingPhotos
		}
	}
	return s.remove(ctx, p)
}

// Reorder cambia el orden de las fotos públicas; la primera es la portada.
func (s *PhotoService) Reorder(ctx context.Context, ownerID, listingID string, ids []string) ([]PhotoView, error) {
	if _, err := s.editableListing(ctx, ownerID, listingID); err != nil {
		return nil, err
	}
	photos, err := s.d.Photos.ListPhotos(ctx, listingID)
	if err != nil {
		return nil, err
	}
	if err := domain.CheckPhotoOrder(photos, ids); err != nil {
		return nil, err
	}
	if err := s.d.Photos.ReorderPhotos(ctx, listingID, ids); err != nil {
		return nil, err
	}
	return s.List(ctx, ownerID, listingID)
}

// remove borra la fila y sus archivos (original y tamaños).
func (s *PhotoService) remove(ctx context.Context, p domain.ListingPhoto) error {
	if err := s.d.Photos.DeletePhoto(ctx, p.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if err := s.d.Storage.Delete(ctx, port.BucketPrivate, p.ObjectKey); err != nil {
		return err
	}
	return s.d.Storage.Delete(ctx, variantBucket(p.Kind), variantKeys(p)...)
}

func variantBucket(kind domain.PhotoKind) port.Bucket {
	if kind == domain.PhotoPublic {
		return port.BucketPublic
	}
	return port.BucketPrivate
}

func variantKeys(p domain.ListingPhoto) []string {
	keys := make([]string, len(domain.PhotoSizes))
	for i, width := range domain.PhotoSizes {
		keys[i] = domain.PhotoVariantKey(p.ListingID, p.ID, width)
	}
	return keys
}
