// Package domain contiene las entidades del negocio, sin dependencias externas.
package domain

type HealthStatus string

const (
	HealthUp   HealthStatus = "up"
	HealthDown HealthStatus = "down"
)

// HealthReport resume el estado de la API y de cada dependencia.
// No incluye mensajes de error: la respuesta es pública.
type HealthReport struct {
	Status     HealthStatus            `json:"status"`
	Components map[string]HealthStatus `json:"components"`
}

func (r HealthReport) IsUp() bool { return r.Status == HealthUp }
