package port

import (
	"context"
	"time"
)

// Cache guarda lecturas frecuentes (cache-aside, docs/04). Nunca es la fuente de verdad:
// si falla, el servicio lee de Postgres. Implementación: Redis.
type Cache interface {
	// Get devuelve el valor y si existía.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// DeletePrefix invalida todas las claves que empiezan con prefix (al escribir en el admin).
	DeletePrefix(ctx context.Context, prefix string) error
}
