package redisclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// OAuthStateStore implementa port.OAuthStateStore.
//
//	{prefix}oauth_state:{state}  JSON con el verificador PKCE y los consentimientos, TTL corto
type OAuthStateStore struct {
	rdb    *redis.Client
	prefix string
}

func NewOAuthStateStore(rdb *redis.Client, prefix string) *OAuthStateStore {
	return &OAuthStateStore{rdb: rdb, prefix: prefix}
}

func (s *OAuthStateStore) key(state string) string { return s.prefix + "oauth_state:" + state }

func (s *OAuthStateStore) Save(ctx context.Context, state string, data port.OAuthState, ttl time.Duration) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, s.key(state), raw, ttl).Err()
}

// Consume usa GETDEL: leer y borrar es una sola operación, así el state sirve una única vez
// aunque lleguen dos vueltas del proveedor al mismo tiempo.
func (s *OAuthStateStore) Consume(ctx context.Context, state string) (port.OAuthState, error) {
	raw, err := s.rdb.GetDel(ctx, s.key(state)).Bytes()
	if errors.Is(err, redis.Nil) {
		return port.OAuthState{}, domain.ErrOAuthState
	}
	if err != nil {
		return port.OAuthState{}, err
	}
	var data port.OAuthState
	if err := json.Unmarshal(raw, &data); err != nil {
		return port.OAuthState{}, fmt.Errorf("oauth state corrupto: %w", err)
	}
	return data, nil
}
