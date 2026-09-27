package redisclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

func TestOAuthStateStore(t *testing.T) {
	sessions, rdb := newTestStore(t) // mismo Redis y prefijo de prueba
	store := NewOAuthStateStore(rdb, sessions.prefix)
	ctx := context.Background()

	want := port.OAuthState{Verifier: "verificador-pkce", AdultDeclared: true, AcceptLegal: true}
	if err := store.Save(ctx, "st-1", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	if ttl := rdb.TTL(ctx, store.key("st-1")).Val(); ttl <= 0 || ttl > time.Minute {
		t.Fatalf("el state debe vencer, TTL = %v", ttl)
	}

	got, err := store.Consume(ctx, "st-1")
	if err != nil || got != want {
		t.Fatalf("Consume = %+v, %v", got, err)
	}
	if _, err := store.Consume(ctx, "st-1"); !errors.Is(err, domain.ErrOAuthState) {
		t.Fatalf("el state sirve una sola vez, llegó %v", err)
	}
	if _, err := store.Consume(ctx, "no-existe"); !errors.Is(err, domain.ErrOAuthState) {
		t.Fatalf("un state desconocido es inválido, llegó %v", err)
	}
}
