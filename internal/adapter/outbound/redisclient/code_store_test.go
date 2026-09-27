package redisclient

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestCodeStore(t *testing.T) {
	_, rdb := newTestStore(t) // reutiliza la conexión y la limpieza por prefijo
	store := NewCodeStore(rdb, "test:codes:"+time.Now().Format("150405.000000")+":")
	t.Cleanup(func() {
		keys, _ := rdb.Keys(context.Background(), store.prefix+"*").Result()
		if len(keys) > 0 {
			rdb.Del(context.Background(), keys...)
		}
	})
	ctx := context.Background()
	p := domain.CodeEmailVerification

	t.Run("correcto se consume una sola vez", func(t *testing.T) {
		_ = store.Save(ctx, p, "u1", "hash-ok", time.Minute, 3)
		if err := store.Consume(ctx, p, "u1", "hash-ok"); err != nil {
			t.Fatal(err)
		}
		if err := store.Consume(ctx, p, "u1", "hash-ok"); !errors.Is(err, domain.ErrCodeInvalid) {
			t.Fatalf("un código ya usado es inválido, llegó %v", err)
		}
	})

	t.Run("intentos se agotan", func(t *testing.T) {
		_ = store.Save(ctx, p, "u2", "hash-ok", time.Minute, 2)
		if err := store.Consume(ctx, p, "u2", "mal"); !errors.Is(err, domain.ErrCodeInvalid) {
			t.Fatalf("primer fallo: %v", err)
		}
		if err := store.Consume(ctx, p, "u2", "mal"); !errors.Is(err, domain.ErrCodeExhausted) {
			t.Fatalf("segundo fallo agota: %v", err)
		}
		if err := store.Consume(ctx, p, "u2", "hash-ok"); !errors.Is(err, domain.ErrCodeInvalid) {
			t.Fatal("agotado, ni el correcto sirve")
		}
	})

	t.Run("vence con el TTL", func(t *testing.T) {
		_ = store.Save(ctx, p, "u3", "hash-ok", time.Second, 3)
		if ttl := rdb.TTL(ctx, store.codeKey(p, "u3")).Val(); ttl <= 0 || ttl > time.Second {
			t.Fatalf("TTL inesperado: %v", ttl)
		}
	})

	t.Run("un solo ganador con intentos simultáneos", func(t *testing.T) {
		_ = store.Save(ctx, p, "u4", "hash-ok", time.Minute, 3)
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := 0
		for range 10 {
			wg.Go(func() {
				if store.Consume(ctx, p, "u4", "hash-ok") == nil {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("el mismo código solo puede usarse una vez, se usó %d", wins)
		}
	})

	t.Run("espera entre envíos", func(t *testing.T) {
		first, _ := store.AcquireCooldown(ctx, p, "u5", time.Minute)
		second, _ := store.AcquireCooldown(ctx, p, "u5", time.Minute)
		if !first || second {
			t.Fatalf("primero sí, segundo no: %v %v", first, second)
		}
	})
}
