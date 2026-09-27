package redisclient

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// CodeStore implementa port.CodeStore.
//
//	{prefix}code:{finalidad}:{sujeto}      hash con campos h (hash del código) y a (intentos restantes)
//	{prefix}cooldown:{finalidad}:{sujeto}  marca de espera entre envíos
type CodeStore struct {
	rdb    *redis.Client
	prefix string
}

func NewCodeStore(rdb *redis.Client, prefix string) *CodeStore {
	return &CodeStore{rdb: rdb, prefix: prefix}
}

func (s *CodeStore) codeKey(p domain.CodePurpose, subject string) string {
	return s.prefix + "code:" + string(p) + ":" + subject
}

func (s *CodeStore) cooldownKey(p domain.CodePurpose, subject string) string {
	return s.prefix + "cooldown:" + string(p) + ":" + subject
}

func (s *CodeStore) Save(ctx context.Context, p domain.CodePurpose, subject, codeHash string, ttl time.Duration, attempts int) error {
	key := s.codeKey(p, subject)
	_, err := s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, key)
		pipe.HSet(ctx, key, "h", codeHash, "a", attempts)
		pipe.Expire(ctx, key, ttl)
		return nil
	})
	return err
}

// consumeScript valida y borra de forma atómica; así dos intentos simultáneos no pueden
// usar el mismo código ni saltarse el límite de intentos.
// Devuelve 1 = correcto, 0 = incorrecto, -1 = agotado, -2 = no existe o venció.
var consumeScript = redis.NewScript(`
local h = redis.call('HGET', KEYS[1], 'h')
if not h then return -2 end
if h == ARGV[1] then
  redis.call('DEL', KEYS[1])
  return 1
end
local left = redis.call('HINCRBY', KEYS[1], 'a', -1)
if left <= 0 then
  redis.call('DEL', KEYS[1])
  return -1
end
return 0
`)

func (s *CodeStore) Consume(ctx context.Context, p domain.CodePurpose, subject, codeHash string) error {
	result, err := consumeScript.Run(ctx, s.rdb, []string{s.codeKey(p, subject)}, codeHash).Int()
	if err != nil {
		return err
	}
	switch result {
	case 1:
		return nil
	case -1:
		return domain.ErrCodeExhausted
	default:
		return domain.ErrCodeInvalid
	}
}

func (s *CodeStore) AcquireCooldown(ctx context.Context, p domain.CodePurpose, subject string, d time.Duration) (bool, error) {
	return s.rdb.SetNX(ctx, s.cooldownKey(p, subject), 1, d).Result()
}
