package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// BlockReason dice por qué una publicación no está disponible en un periodo.
type BlockReason string

const (
	BlockManual  BlockReason = "manual"  // el arrendador bloqueó las fechas
	BlockBooking BlockReason = "booking" // una reserva confirmada (006)
	BlockHold    BlockReason = "hold"    // una reserva en curso de pago (006)
)

// Límites del calendario del arrendador.
const (
	AvailabilityNoteMax = 200
	// availabilityHorizon: no se bloquea más allá de un año y medio (nadie reserva tan lejos).
	availabilityHorizon = 18 * 30 * 24 * time.Hour
	// availabilityMaxSpan: un bloqueo manual dura como mucho un año; para más, se pausa la publicación.
	availabilityMaxSpan = 365 * 24 * time.Hour
)

// AvailabilityBlock es un periodo [Start, End) en que la herramienta no se alquila. Los bloqueos
// de una publicación nunca se cruzan (restricción de exclusión de la migración 0006).
type AvailabilityBlock struct {
	ID        string
	ListingID string
	Start     time.Time
	End       time.Time
	Reason    BlockReason
	Note      string
	CreatedBy string
	CreatedAt time.Time
}

// NormalizeManualBlock valida un bloqueo del arrendador: fin después del inicio, que no termine en
// el pasado, dentro del horizonte y con una nota corta opcional.
func NormalizeManualBlock(b AvailabilityBlock, now time.Time) (AvailabilityBlock, error) {
	b.Note = strings.TrimSpace(b.Note)
	b.Reason = BlockManual
	switch {
	case !b.End.After(b.Start),
		!b.End.After(now),
		b.Start.After(now.Add(availabilityHorizon)),
		b.End.Sub(b.Start) > availabilityMaxSpan:
		return AvailabilityBlock{}, ErrAvailabilityPeriod
	case utf8.RuneCountInString(b.Note) > AvailabilityNoteMax:
		return AvailabilityBlock{}, ErrAvailabilityPeriod
	}
	return b, nil
}

// CalendarWindow acota la consulta del calendario: como mucho 13 meses a partir de from.
func CalendarWindow(from, to time.Time) (time.Time, time.Time, error) {
	if !to.After(from) || to.Sub(from) > 13*31*24*time.Hour {
		return time.Time{}, time.Time{}, ErrAvailabilityPeriod
	}
	return from, to, nil
}
