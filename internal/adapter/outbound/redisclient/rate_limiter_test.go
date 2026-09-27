package redisclient

import (
	"context"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestRateLimiter(t *testing.T) {
	_, rdb := newTestStore(t)
	prefix := "test:rl:" + time.Now().Format("150405.000000") + ":"
	l := NewRateLimiter(rdb, prefix)
	t.Cleanup(func() {
		keys, _ := rdb.Keys(context.Background(), prefix+"*").Result()
		if len(keys) > 0 {
			rdb.Del(context.Background(), keys...)
		}
	})
	ctx := context.Background()
	limit := domain.Limit{Max: 3, Window: 400 * time.Millisecond}

	for i := range 3 {
		if ok, _, err := l.Allow(ctx, "login:ana", limit); err != nil || !ok {
			t.Fatalf("intento %d dentro del límite debe pasar: %v", i+1, err)
		}
	}
	ok, retry, _ := l.Allow(ctx, "login:ana", limit)
	if ok || retry <= 0 || retry > limit.Window {
		t.Fatalf("el cuarto intento se bloquea con una espera razonable: ok=%v retry=%v", ok, retry)
	}
	if ok, _, _ := l.Allow(ctx, "login:beto", limit); !ok {
		t.Fatal("cada clave tiene su propio límite")
	}

	time.Sleep(limit.Window + 50*time.Millisecond)
	if ok, _, _ := l.Allow(ctx, "login:ana", limit); !ok {
		t.Fatal("pasada la ventana se permite de nuevo")
	}

	for range 3 {
		_, _, _ = l.Allow(ctx, "login:carla", limit)
	}
	if err := l.Reset(ctx, "login:carla"); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := l.Allow(ctx, "login:carla", limit); !ok {
		t.Fatal("Reset libera la clave")
	}
}
