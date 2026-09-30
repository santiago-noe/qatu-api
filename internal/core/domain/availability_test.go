package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeManualBlock(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	b, err := NormalizeManualBlock(AvailabilityBlock{Start: now.Add(day), End: now.Add(3 * day), Note: "  mantenimiento ", Reason: BlockBooking}, now)
	if err != nil || b.Note != "mantenimiento" || b.Reason != BlockManual {
		t.Fatalf("un bloqueo del arrendador siempre es manual: %+v, %v", b, err)
	}
	// Empezó ayer y termina mañana: vale (bloquea lo que queda).
	if _, err := NormalizeManualBlock(AvailabilityBlock{Start: now.Add(-day), End: now.Add(day)}, now); err != nil {
		t.Fatal(err)
	}
	for name, blk := range map[string]AvailabilityBlock{
		"fin antes del inicio": {Start: now.Add(2 * day), End: now.Add(day)},
		"vacío":                {Start: now.Add(day), End: now.Add(day)},
		"todo en el pasado":    {Start: now.Add(-3 * day), End: now.Add(-day)},
		"demasiado lejos":      {Start: now.Add(600 * day), End: now.Add(601 * day)},
		"más de un año":        {Start: now.Add(day), End: now.Add(400 * day)},
		"nota larga":           {Start: now.Add(day), End: now.Add(2 * day), Note: strings.Repeat("a", 201)},
	} {
		if _, err := NormalizeManualBlock(blk, now); !errors.Is(err, ErrAvailabilityPeriod) {
			t.Errorf("%s: quiero ErrAvailabilityPeriod, llegó %v", name, err)
		}
	}
}

func TestCalendarWindow(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := CalendarWindow(from, from.AddDate(0, 3, 0)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CalendarWindow(from, from); !errors.Is(err, ErrAvailabilityPeriod) {
		t.Fatal("ventana vacía")
	}
	if _, _, err := CalendarWindow(from, from.AddDate(2, 0, 0)); !errors.Is(err, ErrAvailabilityPeriod) {
		t.Fatal("ventana de dos años")
	}
}
