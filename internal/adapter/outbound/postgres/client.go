// Package postgres implementa los repositorios sobre PostgreSQL con pgx.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Client envuelve el pool de conexiones. Implementa port.HealthChecker.
type Client struct {
	Pool *pgxpool.Pool
}

// New crea el pool; la conexión real se abre en la primera consulta.
func New(ctx context.Context, url string) (*Client, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: configuración inválida: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return &Client{Pool: pool}, nil
}

func (c *Client) Name() string { return "postgres" }

func (c *Client) Ping(ctx context.Context) error { return c.Pool.Ping(ctx) }

func (c *Client) Close() { c.Pool.Close() }
