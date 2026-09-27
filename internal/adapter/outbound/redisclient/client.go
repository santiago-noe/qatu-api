// Package redisclient implementa sesiones, códigos, caché y límites sobre Redis.
package redisclient

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Client envuelve el cliente de Redis. Implementa port.HealthChecker.
type Client struct {
	RDB *redis.Client
}

func New(addr, password string, db int) *Client {
	return &Client{RDB: redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})}
}

func (c *Client) Name() string { return "redis" }

func (c *Client) Ping(ctx context.Context) error { return c.RDB.Ping(ctx).Err() }

func (c *Client) Close() error { return c.RDB.Close() }
