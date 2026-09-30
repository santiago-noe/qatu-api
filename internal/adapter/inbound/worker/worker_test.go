package worker

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/queue"
)

// countingPhotos cuenta cuántas veces se procesa cada foto.
type countingPhotos struct {
	mu    sync.Mutex
	calls map[string]int
	done  chan string
}

func (c *countingPhotos) Process(_ context.Context, id string) error {
	c.mu.Lock()
	c.calls[id]++
	c.mu.Unlock()
	c.done <- id
	return nil
}

// Integración con un Redis real (QATU_TEST_REDIS_ADDR). Usa la base 9 para no mezclarse con la
// cola de desarrollo.
func TestPhotoJobRoundTrip(t *testing.T) {
	addr := os.Getenv("QATU_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("QATU_TEST_REDIS_ADDR no definido: se omite la integración con Redis")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 9})
	ctx := context.Background()
	t.Cleanup(func() { rdb.FlushDB(ctx); rdb.Close() })

	photos := &countingPhotos{calls: map[string]int{}, done: make(chan string, 4)}
	w := New(rdb, zerolog.Nop(), 2, photos)
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Shutdown()

	jobs := queue.NewPhotoJobs(rdb)
	id := fmt.Sprintf("foto-%d", time.Now().UnixNano())
	for range 2 { // confirmar dos veces no encola dos trabajos
		if err := jobs.EnqueuePhoto(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case got := <-photos.done:
		if got != id {
			t.Fatalf("procesó %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("el worker no procesó la foto")
	}
	time.Sleep(500 * time.Millisecond)
	photos.mu.Lock()
	defer photos.mu.Unlock()
	if photos.calls[id] != 1 {
		t.Fatalf("una sola vez: %d", photos.calls[id])
	}
}
