package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

type memoryPhotos struct {
	photos map[string]domain.ListingPhoto
}

func (m *memoryPhotos) ListPhotos(_ context.Context, listingID string) ([]domain.ListingPhoto, error) {
	var out []domain.ListingPhoto
	for _, p := range m.photos {
		if p.ListingID == listingID {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b domain.ListingPhoto) int { return a.SortOrder - b.SortOrder })
	return out, nil
}

func (m *memoryPhotos) FindPhoto(_ context.Context, id string) (domain.ListingPhoto, error) {
	p, ok := m.photos[id]
	if !ok {
		return domain.ListingPhoto{}, domain.ErrNotFound
	}
	return p, nil
}

func (m *memoryPhotos) ListStalePending(_ context.Context, before time.Time, limit int) ([]domain.ListingPhoto, error) {
	var out []domain.ListingPhoto
	for _, p := range m.photos {
		if p.Status == domain.PhotoPending && p.CreatedAt.Before(before) && len(out) < limit {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memoryPhotos) CreatePhoto(_ context.Context, p domain.ListingPhoto) error {
	p.SortOrder = len(m.photos)
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	m.photos[p.ID] = p
	return nil
}

func (m *memoryPhotos) set(id string, change func(*domain.ListingPhoto)) error {
	p, ok := m.photos[id]
	if !ok {
		return domain.ErrNotFound
	}
	change(&p)
	m.photos[id] = p
	return nil
}

func (m *memoryPhotos) MarkPhotoReady(_ context.Context, id string, w, h int) error {
	return m.set(id, func(p *domain.ListingPhoto) { p.Status, p.Width, p.Height = domain.PhotoReady, w, h })
}

func (m *memoryPhotos) MarkPhotoFailed(_ context.Context, id string) error {
	return m.set(id, func(p *domain.ListingPhoto) { p.Status = domain.PhotoFailed })
}

func (m *memoryPhotos) DeletePhoto(_ context.Context, id string) error {
	delete(m.photos, id)
	return nil
}

func (m *memoryPhotos) ReorderPhotos(_ context.Context, _ string, ids []string) error {
	for i, id := range ids {
		_ = m.set(id, func(p *domain.ListingPhoto) { p.SortOrder = i })
	}
	return nil
}

// memoryStorage guarda objetos por bucket/clave; broken simula el almacenamiento caído.
type memoryStorage struct {
	objects map[string][]byte
	broken  bool
}

func objKey(b port.Bucket, key string) string { return string(b) + ":" + key }

func (m *memoryStorage) PresignPut(_ context.Context, b port.Bucket, key, ct string, ttl time.Duration) (port.PresignedUpload, error) {
	return port.PresignedUpload{URL: "https://s3.test/" + objKey(b, key), Headers: map[string]string{"Content-Type": ct}}, nil
}

func (m *memoryStorage) Stat(_ context.Context, b port.Bucket, key string) (int64, error) {
	data, ok := m.objects[objKey(b, key)]
	if !ok {
		return 0, domain.ErrNotFound
	}
	return int64(len(data)), nil
}

func (m *memoryStorage) Get(_ context.Context, b port.Bucket, key string) (io.ReadCloser, error) {
	if m.broken {
		return nil, errors.New("almacenamiento caído")
	}
	data, ok := m.objects[objKey(b, key)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memoryStorage) Put(_ context.Context, b port.Bucket, key string, data []byte, _ string) error {
	m.objects[objKey(b, key)] = data
	return nil
}

func (m *memoryStorage) Delete(_ context.Context, b port.Bucket, keys ...string) error {
	for _, k := range keys {
		delete(m.objects, objKey(b, k))
	}
	return nil
}

func (m *memoryStorage) PresignGet(_ context.Context, b port.Bucket, key string, _ time.Duration) (string, error) {
	return "https://s3.test/" + objKey(b, key) + "?firma", nil
}

func (m *memoryStorage) PublicURL(key string) string { return "https://fotos.test/" + key }

func (m *memoryStorage) keysWith(prefix string) []string {
	var out []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

type recordingJobs struct{ queued []string }

func (r *recordingJobs) EnqueuePhoto(_ context.Context, id string) error {
	r.queued = append(r.queued, id)
	return nil
}

// fakeImages acepta todo salvo el texto "roto" y devuelve un JPEG falso por ancho.
type fakeImages struct{}

func (fakeImages) Process(data []byte, widths []int, _ int) (port.ProcessedImage, error) {
	if string(data) == "roto" {
		return port.ProcessedImage{}, domain.ErrPhotoInvalid
	}
	out := port.ProcessedImage{Width: 1600, Height: 1200, Variants: map[int][]byte{}}
	for _, w := range widths {
		out.Variants[w] = []byte("jpeg")
	}
	return out, nil
}

type photoFixture struct {
	svc      *PhotoService
	listings *memoryListings
	photos   *memoryPhotos
	storage  *memoryStorage
	jobs     *recordingJobs
	listing  domain.ToolListing
}

func newPhotoFixture() photoFixture {
	listings := newMemoryListings()
	l := domain.ToolListing{ID: "l1", OwnerID: "ana", Status: domain.ListingDraft, Version: 1}
	listings.listings[l.ID] = l
	f := photoFixture{listings: listings, photos: &memoryPhotos{photos: map[string]domain.ListingPhoto{}},
		storage: &memoryStorage{objects: map[string][]byte{}}, jobs: &recordingJobs{}, listing: l}
	f.svc = NewPhotoService(PhotoDeps{Listings: listings, Photos: f.photos, Storage: f.storage, Jobs: f.jobs,
		Images: fakeImages{}, IDs: &seqIDs{}, Clock: &fakeClock{now: time.Now()}})
	return f
}

// upload hace el recorrido completo del navegador: pedir, subir y confirmar.
func (f photoFixture) upload(t *testing.T, kind domain.PhotoKind, content string) domain.ListingPhoto {
	t.Helper()
	ctx := context.Background()
	p, up, err := f.svc.RequestUpload(ctx, "ana", "l1", kind, "image/jpeg", int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if up.Headers["Content-Type"] != "image/jpeg" || !strings.Contains(up.URL, "private:originals/l1/") {
		t.Fatalf("se sube al bucket privado con el tipo firmado: %+v", up)
	}
	f.storage.objects[objKey(port.BucketPrivate, p.ObjectKey)] = []byte(content)
	if _, err := f.svc.CompleteUpload(ctx, "ana", "l1", p.ID); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPhotoUploadAndProcess(t *testing.T) {
	f := newPhotoFixture()
	ctx := context.Background()

	if _, _, err := f.svc.RequestUpload(ctx, "ana", "l1", domain.PhotoPublic, "image/gif", 100); !errors.Is(err, domain.ErrPhotoType) {
		t.Fatalf("tipo no admitido: %v", err)
	}
	if _, _, err := f.svc.RequestUpload(ctx, "beto", "l1", domain.PhotoPublic, "image/jpeg", 100); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("publicación ajena: %v", err)
	}

	p, _, err := f.svc.RequestUpload(ctx, "ana", "l1", domain.PhotoPublic, "image/jpeg", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CompleteUpload(ctx, "ana", "l1", p.ID); !errors.Is(err, domain.ErrPhotoNotUploaded) {
		t.Fatalf("confirmar sin haber subido: %v", err)
	}
	f.storage.objects[objKey(port.BucketPrivate, p.ObjectKey)] = []byte("foto")
	if _, err := f.svc.CompleteUpload(ctx, "ana", "l1", p.ID); err != nil || !slices.Equal(f.jobs.queued, []string{p.ID}) {
		t.Fatalf("encola el procesamiento: %v %v", err, f.jobs.queued)
	}

	if err := f.svc.Process(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	got := f.photos.photos[p.ID]
	if got.Status != domain.PhotoReady || got.Width != 1600 {
		t.Fatalf("lista con sus medidas: %+v", got)
	}
	if n := len(f.storage.keysWith("public:listings/l1/" + p.ID + "/")); n != 3 {
		t.Fatalf("3 tamaños públicos: %d", n)
	}
	if _, ok := f.storage.objects[objKey(port.BucketPrivate, p.ObjectKey)]; ok {
		t.Fatal("el original (con EXIF) se borra")
	}
	if err := f.svc.Process(ctx, p.ID); err != nil {
		t.Fatal("procesar de nuevo una lista no hace nada")
	}

	views, err := f.svc.List(ctx, "ana", "l1")
	if err != nil || views[0].URLs[800] != "https://fotos.test/listings/l1/"+p.ID+"/800.jpg" {
		t.Fatalf("URL pública permanente: %+v, %v", views, err)
	}
}

func TestPhotoProcessFailures(t *testing.T) {
	f := newPhotoFixture()
	ctx := context.Background()

	broken := f.upload(t, domain.PhotoPublic, "roto")
	if err := f.svc.Process(ctx, broken.ID); err != nil {
		t.Fatal("un archivo que no es imagen no se reintenta")
	}
	if f.photos.photos[broken.ID].Status != domain.PhotoFailed || len(f.storage.keysWith("private:originals")) != 0 {
		t.Fatalf("queda fallida y sin original: %+v", f.photos.photos[broken.ID])
	}

	p := f.upload(t, domain.PhotoPublic, "foto")
	f.storage.broken = true
	if err := f.svc.Process(ctx, p.ID); err == nil {
		t.Fatal("un error del almacenamiento se devuelve para reintentar")
	}
	if f.photos.photos[p.ID].Status != domain.PhotoPending {
		t.Fatal("sigue pendiente hasta que un reintento funcione")
	}
	f.storage.broken = false

	if err := f.svc.Process(ctx, "no-existe"); err != nil {
		t.Fatal("una foto borrada antes de procesarse se ignora")
	}

	huge, _, _ := f.svc.RequestUpload(ctx, "ana", "l1", domain.PhotoPublic, "image/jpeg", 100)
	f.storage.objects[objKey(port.BucketPrivate, huge.ObjectKey)] = make([]byte, domain.MaxPhotoBytes+1)
	if _, err := f.svc.CompleteUpload(ctx, "ana", "l1", huge.ID); !errors.Is(err, domain.ErrPhotoType) {
		t.Fatalf("subió más de lo declarado: %v", err)
	}
	if f.photos.photos[huge.ID].Status != domain.PhotoFailed {
		t.Fatal("queda fallida")
	}
}

func TestSerialPhotoIsPrivateAndSingle(t *testing.T) {
	f := newPhotoFixture()
	ctx := context.Background()
	first := f.upload(t, domain.PhotoSerial, "placa")
	must(t, f.svc.Process(ctx, first.ID))
	if n := len(f.storage.keysWith("public:")); n != 0 {
		t.Fatalf("la placa nunca va al bucket público: %d", n)
	}
	views, _ := f.svc.List(ctx, "ana", "l1")
	if !strings.HasSuffix(views[0].URLs[320], "?firma") {
		t.Fatalf("la placa se ve con URL firmada: %v", views[0].URLs)
	}

	second := f.upload(t, domain.PhotoSerial, "placa nueva")
	if _, ok := f.photos.photos[first.ID]; ok || len(f.storage.keysWith("private:listings/l1/"+first.ID+"/")) != 0 {
		t.Fatal("una placa nueva reemplaza la anterior y sus archivos")
	}
	if _, ok := f.photos.photos[second.ID]; !ok {
		t.Fatal("queda la nueva")
	}
}

func TestPhotoLimitsDeleteAndOrder(t *testing.T) {
	f := newPhotoFixture()
	ctx := context.Background()
	var ids []string
	for range domain.ListingPhotosMax {
		p := f.upload(t, domain.PhotoPublic, "foto")
		must(t, f.svc.Process(ctx, p.ID))
		ids = append(ids, p.ID)
	}
	if _, _, err := f.svc.RequestUpload(ctx, "ana", "l1", domain.PhotoPublic, "image/jpeg", 10); !errors.Is(err, domain.ErrPhotoLimit) {
		t.Fatalf("la 13: %v", err)
	}

	reversed := slices.Clone(ids)
	slices.Reverse(reversed)
	views, err := f.svc.Reorder(ctx, "ana", "l1", reversed)
	if err != nil || views[0].Photo.ID != ids[len(ids)-1] {
		t.Fatalf("la última pasa a ser la portada: %v", err)
	}
	if _, err := f.svc.Reorder(ctx, "ana", "l1", reversed[1:]); !errors.Is(err, domain.ErrPhotoOrder) {
		t.Fatalf("orden incompleto: %v", err)
	}

	must(t, f.svc.Delete(ctx, "ana", "l1", ids[0]))
	if len(f.storage.keysWith("public:listings/l1/"+ids[0]+"/")) != 0 {
		t.Fatal("borrar quita también los archivos")
	}

	// Publicada con 3 fotos: no puede quedar con 2.
	l := f.listings.listings["l1"]
	l.Status = domain.ListingPublished
	f.listings.listings["l1"] = l
	for _, id := range ids[1:9] {
		must(t, f.svc.Delete(ctx, "ana", "l1", id))
	}
	if err := f.svc.Delete(ctx, "ana", "l1", ids[9]); !errors.Is(err, domain.ErrListingPhotos) {
		t.Fatalf("una publicada no queda con menos de 3 fotos: %v", err)
	}

	l.Status = domain.ListingInReview
	f.listings.listings["l1"] = l
	if _, _, err := f.svc.RequestUpload(ctx, "ana", "l1", domain.PhotoPublic, "image/jpeg", 10); !errors.Is(err, domain.ErrListingNotEditable) {
		t.Fatalf("en revisión no se cambian las fotos: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
