// Package port define los contratos que implementan los adaptadores.
package port

import "context"

// HealthChecker es una dependencia cuyo estado se reporta en /health (Postgres, Redis…).
type HealthChecker interface {
	Name() string
	Ping(ctx context.Context) error
}
