package redisclient

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache implementa port.Cache.
//
//	{prefix}cache:{clave}  valor serializado con su TTL
type Cache struct {
	rdb    *redis.Client
	prefix string
}

func NewCache(rdb *redis.Client, prefix string) *Cache {
	return &Cache{rdb: rdb, prefix: prefix + "cache:"}
}

func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := c.rdb.Get(ctx, c.prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	return v, err == nil, err
}

func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, c.prefix+key, value, ttl).Err()
}

// DeletePrefix recorre con SCAN (no KEYS, que bloquea Redis) y borra con UNLINK en segundo plano.
func (c *Cache) DeletePrefix(ctx context.Context, prefix string) error {
	iter := c.rdb.Scan(ctx, 0, c.prefix+prefix+"*", 200).Iterator()
	var batch []string
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) == 200 {
			if err := c.rdb.Unlink(ctx, batch...).Err(); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(batch) > 0 {
		return c.rdb.Unlink(ctx, batch...).Err()
	}
	return nil
}
