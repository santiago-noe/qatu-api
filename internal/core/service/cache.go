package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// cached aplica cache-aside (docs/04): devuelve key de la caché o llama a load y guarda el
// resultado. La caché nunca es la fuente de verdad: si Redis falla o el valor está corrupto,
// se lee de la fuente y la respuesta sigue siendo correcta.
func cached[T any](ctx context.Context, cache port.Cache, key string, ttl time.Duration, load func() (T, error)) (T, error) {
	if raw, ok, err := cache.Get(ctx, key); err == nil && ok {
		var v T
		if json.Unmarshal(raw, &v) == nil {
			return v, nil
		}
	}
	v, err := load()
	if err != nil {
		return v, err
	}
	if raw, err := json.Marshal(v); err == nil {
		_ = cache.Set(ctx, key, raw, ttl)
	}
	return v, nil
}
