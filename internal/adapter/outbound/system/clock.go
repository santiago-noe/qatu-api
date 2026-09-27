// Package system implementa los ports de reloj e identificadores con el sistema real.
package system

import (
	"time"

	"github.com/google/uuid"
)

type Clock struct{}

func (Clock) Now() time.Time { return time.Now().UTC() }

// UUIDGenerator genera UUID v7: ordenables por tiempo, mejores para índices de Postgres.
type UUIDGenerator struct{}

func (UUIDGenerator) NewID() string { return uuid.Must(uuid.NewV7()).String() }
