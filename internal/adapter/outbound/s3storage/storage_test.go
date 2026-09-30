package s3storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// newTestStorage usa el SeaweedFS de docker compose (QATU_TEST_S3=localhost:8333) con los buckets
// de desarrollo; cada prueba escribe bajo una clave propia.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	endpoint := os.Getenv("QATU_TEST_S3")
	if endpoint == "" {
		t.Skip("QATU_TEST_S3 no definido: se omite la integración con S3")
	}
	s, err := New(Config{Endpoint: endpoint, AccessKey: "qatu-dev", SecretKey: "qatu-dev-secret", Region: "us-east-1",
		PublicBucket: "qatu-public", PrivateBucket: "qatu-private", PublicBaseURL: "http://" + endpoint + "/qatu-public"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBuckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func put(t *testing.T, url, contentType string, body []byte) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func get(t *testing.T, url string) int {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestPresignedUploadAndAccess(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	key := fmt.Sprintf("test/%d/original", time.Now().UnixNano())
	body := []byte("imagen de prueba")

	up, err := s.PresignPut(ctx, port.BucketPrivate, key, "image/jpeg", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if code := put(t, up.URL, "image/png", body); code != http.StatusForbidden {
		t.Fatalf("otro Content-Type no calza con la firma: %d", code)
	}
	if code := put(t, up.URL, up.Headers["Content-Type"], body); code != http.StatusOK {
		t.Fatalf("subida directa del navegador: %d", code)
	}
	if size, err := s.Stat(ctx, port.BucketPrivate, key); err != nil || size != int64(len(body)) {
		t.Fatalf("Stat = %d, %v", size, err)
	}
	r, err := s.Get(ctx, port.BucketPrivate, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, body) {
		t.Fatalf("Get = %q", got)
	}

	// Privado: sin firma no se lee; con URL firmada, sí.
	if code := get(t, "http://"+os.Getenv("QATU_TEST_S3")+"/qatu-private/"+key); code == http.StatusOK {
		t.Fatal("el bucket privado no se lee sin credenciales")
	}
	signed, err := s.PresignGet(ctx, port.BucketPrivate, key, time.Minute)
	if err != nil || get(t, signed) != http.StatusOK {
		t.Fatalf("URL firmada de lectura: %v", err)
	}

	// Público: se lee sin credenciales.
	must(t, s.Put(ctx, port.BucketPublic, key+".jpg", body, "image/jpeg"))
	if code := get(t, s.PublicURL(key+".jpg")); code != http.StatusOK {
		t.Fatalf("el bucket público se lee sin credenciales: %d", code)
	}

	must(t, s.Delete(ctx, port.BucketPrivate, key, key+"-no-existe"))
	must(t, s.Delete(ctx, port.BucketPublic, key+".jpg"))
	if _, err := s.Stat(ctx, port.BucketPrivate, key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("borrado: %v", err)
	}
	if _, err := s.Get(ctx, port.BucketPrivate, key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get de algo borrado: %v", err)
	}
}

// El navegador sube desde qatu-app: el almacenamiento debe aceptar la consulta previa de CORS.
func TestCORSPreflight(t *testing.T) {
	s := newTestStorage(t)
	up, err := s.PresignPut(context.Background(), port.BucketPrivate, "test/cors", "image/jpeg", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodOptions, up.URL, nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if origin := res.Header.Get("Access-Control-Allow-Origin"); origin != "http://localhost:3000" && origin != "*" {
		t.Fatalf("CORS: %d, Allow-Origin %q", res.StatusCode, origin)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
