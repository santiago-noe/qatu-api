package redisclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Prueba de integración contra un Redis real. Se ejecuta si existe QATU_TEST_REDIS_ADDR
// (por ejemplo localhost:6379 con docker compose). Usa un prefijo único y lo limpia al final.
func newTestStore(t *testing.T) (*SessionStore, *redis.Client) {
	t.Helper()
	addr := os.Getenv("QATU_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("QATU_TEST_REDIS_ADDR no definido: se omite la integración con Redis")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis no disponible en %s: %v", addr, err)
	}
	prefix := fmt.Sprintf("test:%d:", time.Now().UnixNano())
	t.Cleanup(func() {
		keys, _ := rdb.Keys(ctx, prefix+"*").Result()
		if len(keys) > 0 {
			rdb.Del(ctx, keys...)
		}
		rdb.Close()
	})
	return NewSessionStore(rdb, prefix), rdb
}

func session(id, user string, ttl time.Duration) domain.Session {
	now := time.Now().UTC().Truncate(time.Second)
	return domain.Session{ID: id, UserID: user, Roles: []domain.Role{domain.RoleClient}, Provider: domain.ProviderPassword,
		CreatedAt: now, RenewedAt: now, ExpiresAt: now.Add(ttl)}
}

func TestSessionStoreRoundTrip(t *testing.T) {
	store, rdb := newTestStore(t)
	ctx := context.Background()

	s := session("s1", "u1", time.Hour)
	if err := store.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "s1")
	if err != nil || got.UserID != "u1" || !got.ExpiresAt.Equal(s.ExpiresAt) || got.Roles[0] != domain.RoleClient {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if ttl := rdb.TTL(ctx, store.sessionKey("s1")).Val(); ttl <= 0 || ttl > time.Hour {
		t.Fatalf("la clave debe vencer con la sesión, TTL = %v", ttl)
	}

	later := s.ExpiresAt.Add(2 * time.Hour)
	renew := func(x *domain.Session) { x.ExpiresAt = later }
	if _, err := store.Update(ctx, "s1", renew); err != nil {
		t.Fatal(err)
	}
	if ttl := rdb.TTL(ctx, store.sessionKey("s1")).Val(); ttl <= time.Hour {
		t.Fatalf("renovar debe alargar el TTL, quedó %v", ttl)
	}

	// Un cambio posterior conserva lo anterior y no acorta el vencimiento.
	now := time.Now().UTC().Truncate(time.Second)
	got, err = store.Update(ctx, "s1", func(x *domain.Session) { x.TwoFactorAt = &now })
	if err != nil || got.TwoFactorAt == nil || !got.ExpiresAt.Equal(later) {
		t.Fatalf("Update = %+v, %v", got, err)
	}
	if again, _ := store.Get(ctx, "s1"); again.TwoFactorAt == nil {
		t.Fatal("la marca del segundo paso debe quedar guardada")
	}

	if _, err := store.Get(ctx, "no-existe"); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("una sesión inexistente es inválida, llegó %v", err)
	}
	if _, err := store.Update(ctx, "no-existe", renew); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("no se modifica una sesión cerrada, llegó %v", err)
	}
	if n := rdb.Exists(ctx, store.sessionKey("no-existe")).Val(); n != 0 {
		t.Fatal("Update no debe crear la sesión")
	}
}

func TestSessionStoreDeleteAndList(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for _, s := range []domain.Session{session("a", "u1", time.Hour), session("b", "u1", time.Hour), session("c", "u1", time.Hour), session("z", "u2", time.Hour)} {
		if err := store.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Delete(ctx, "u2", "a"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no se borra una sesión ajena, llegó %v", err)
	}
	if err := store.Delete(ctx, "u1", "a"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAllForUser(ctx, "u1", "c"); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListForUser(ctx, "u1")
	if err != nil || len(list) != 1 || list[0].ID != "c" {
		t.Fatalf("debe quedar solo la sesión conservada: %+v %v", list, err)
	}
	if other, _ := store.ListForUser(ctx, "u2"); len(other) != 1 {
		t.Fatal("las sesiones de otro usuario no se tocan")
	}
}
