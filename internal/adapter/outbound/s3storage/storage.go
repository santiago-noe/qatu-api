// Package s3storage implementa port.ObjectStorage con minio-go sobre cualquier almacenamiento
// compatible con S3: SeaweedFS en local, Cloudflare R2 en producción.
package s3storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

type Config struct {
	Endpoint  string // host:puerto, sin esquema
	AccessKey string
	SecretKey string
	UseSSL    bool
	Region    string // R2 usa "auto"
	// PublicBucket se sirve sin credenciales; PrivateBucket nunca.
	PublicBucket  string
	PrivateBucket string
	// PublicBaseURL antecede la clave en las URLs públicas (en local incluye el bucket; en
	// producción, el dominio propio del bucket de R2).
	PublicBaseURL string
}

type Storage struct {
	client *minio.Client
	cfg    Config
}

func New(cfg Config) (*Storage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3storage: %w", err)
	}
	return &Storage{client: client, cfg: cfg}, nil
}

// EnsureBuckets crea los buckets si faltan (solo en desarrollo: en producción los crea la infraestructura).
func (s *Storage) EnsureBuckets(ctx context.Context) error {
	for _, name := range []string{s.cfg.PublicBucket, s.cfg.PrivateBucket} {
		exists, err := s.client.BucketExists(ctx, name)
		if err != nil {
			return fmt.Errorf("s3storage: bucket %s: %w", name, err)
		}
		if !exists {
			if err := s.client.MakeBucket(ctx, name, minio.MakeBucketOptions{Region: s.cfg.Region}); err != nil {
				return fmt.Errorf("s3storage: crear bucket %s: %w", name, err)
			}
		}
	}
	return nil
}

func (s *Storage) Name() string { return "storage" }

// Ping comprueba el acceso al bucket privado (para /health).
func (s *Storage) Ping(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.cfg.PrivateBucket)
	return err
}

func (s *Storage) bucket(b port.Bucket) string {
	if b == port.BucketPublic {
		return s.cfg.PublicBucket
	}
	return s.cfg.PrivateBucket
}

// PresignPut firma también Content-Type: el navegador solo puede subir el tipo que declaró.
func (s *Storage) PresignPut(ctx context.Context, b port.Bucket, key, contentType string, ttl time.Duration) (port.PresignedUpload, error) {
	headers := http.Header{"Content-Type": []string{contentType}}
	u, err := s.client.PresignHeader(ctx, http.MethodPut, s.bucket(b), key, ttl, url.Values{}, headers)
	if err != nil {
		return port.PresignedUpload{}, err
	}
	return port.PresignedUpload{URL: u.String(), Headers: map[string]string{"Content-Type": contentType},
		ExpiresAt: time.Now().Add(ttl)}, nil
}

func (s *Storage) Stat(ctx context.Context, b port.Bucket, key string) (int64, error) {
	info, err := s.client.StatObject(ctx, s.bucket(b), key, minio.StatObjectOptions{})
	if err != nil {
		return 0, notFound(err)
	}
	return info.Size, nil
}

func (s *Storage) Get(ctx context.Context, b port.Bucket, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket(b), key, minio.GetObjectOptions{})
	if err != nil {
		return nil, notFound(err)
	}
	// GetObject es perezoso: Stat confirma que existe antes de devolverlo.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, notFound(err)
	}
	return obj, nil
}

// Put sube un archivo ya procesado. Las fotos no cambian nunca (clave nueva por foto): caché de un año.
func (s *Storage) Put(ctx context.Context, b port.Bucket, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket(b), key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType, CacheControl: "public, max-age=31536000, immutable",
	})
	return err
}

func (s *Storage) Delete(ctx context.Context, b port.Bucket, keys ...string) error {
	for _, key := range keys {
		if err := s.client.RemoveObject(ctx, s.bucket(b), key, minio.RemoveObjectOptions{}); err != nil && notFound(err) != domain.ErrNotFound {
			return err
		}
	}
	return nil
}

func (s *Storage) PresignGet(ctx context.Context, b port.Bucket, key string, ttl time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket(b), key, ttl, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *Storage) PublicURL(key string) string {
	return strings.TrimSuffix(s.cfg.PublicBaseURL, "/") + "/" + key
}

func notFound(err error) error {
	if code := minio.ToErrorResponse(err).Code; code == "NoSuchKey" || code == "NotFound" {
		return domain.ErrNotFound
	}
	return err
}
