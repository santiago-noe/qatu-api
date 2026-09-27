package redisclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// RateLimiter implementa port.RateLimiter con una ventana deslizante sobre un sorted set:
// {prefix}ratelimit:{clave} guarda la marca de tiempo (ms) de cada intento.
type RateLimiter struct {
	rdb    *redis.Client
	prefix string
}

func NewRateLimiter(rdb *redis.Client, prefix string) *RateLimiter {
	return &RateLimiter{rdb: rdb, prefix: prefix}
}

// slidingWindow es atómico: limpia lo viejo, cuenta y registra el intento solo si cabe.
// Devuelve {1, 0} si se permite o {0, ms_a_esperar} si no.
var slidingWindow = redis.NewScript(`
local now, window, max = tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], 0, now - window)
if redis.call('ZCARD', KEYS[1]) >= max then
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  return {0, tonumber(oldest[2]) + window - now}
end
redis.call('ZADD', KEYS[1], now, ARGV[4])
redis.call('PEXPIRE', KEYS[1], window)
return {1, 0}
`)

func (l *RateLimiter) Allow(ctx context.Context, key string, limit domain.Limit) (bool, time.Duration, error) {
	now := time.Now().UnixMilli()
	res, err := slidingWindow.Run(ctx, l.rdb, []string{l.prefix + "ratelimit:" + key},
		now, limit.Window.Milliseconds(), limit.Max, memberID(now)).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	return res[0] == 1, time.Duration(res[1]) * time.Millisecond, nil
}

func (l *RateLimiter) Reset(ctx context.Context, key string) error {
	return l.rdb.Del(ctx, l.prefix+"ratelimit:"+key).Err()
}

// memberID distingue intentos en el mismo milisegundo.
func memberID(now int64) string {
	b := make([]byte, 6)
	rand.Read(b)
	return time.UnixMilli(now).Format("150405.000") + ":" + hex.EncodeToString(b)
}
