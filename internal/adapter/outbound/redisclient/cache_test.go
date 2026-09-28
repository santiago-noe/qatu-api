package redisclient

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCache(t *testing.T) {
	sessions, rdb := newTestStore(t) // mismo Redis y prefijo de prueba
	cache := NewCache(rdb, sessions.prefix)
	ctx := context.Background()

	if _, ok, err := cache.Get(ctx, "catalog:cities"); ok || err != nil {
		t.Fatalf("una clave que no existe no es error: ok=%v err=%v", ok, err)
	}
	if err := cache.Set(ctx, "catalog:cities", []byte(`["ayacucho"]`), time.Minute); err != nil {
		t.Fatal(err)
	}
	v, ok, err := cache.Get(ctx, "catalog:cities")
	if err != nil || !ok || string(v) != `["ayacucho"]` {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
	if ttl := rdb.TTL(ctx, sessions.prefix+"cache:catalog:cities").Val(); ttl <= 0 || ttl > time.Minute {
		t.Fatalf("la clave vence, TTL = %v", ttl)
	}

	// Más claves que un lote de SCAN, más una de otro prefijo que no debe tocarse.
	for i := range 250 {
		_ = cache.Set(ctx, fmt.Sprintf("catalog:zones:%d", i), []byte("x"), time.Minute)
	}
	_ = cache.Set(ctx, "search:taladro", []byte("x"), time.Minute)
	if err := cache.DeletePrefix(ctx, "catalog:"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.Get(ctx, "catalog:zones:249"); ok {
		t.Fatal("DeletePrefix borra todas las claves del prefijo")
	}
	if _, ok, _ := cache.Get(ctx, "search:taladro"); !ok {
		t.Fatal("DeletePrefix no toca otros prefijos")
	}
}
