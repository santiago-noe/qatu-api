package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

type fakeChecker struct {
	name  string
	err   error
	delay time.Duration
}

func (f fakeChecker) Name() string { return f.name }

func (f fakeChecker) Ping(ctx context.Context) error {
	select {
	case <-time.After(f.delay):
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestHealthServiceCheck(t *testing.T) {
	tests := []struct {
		name       string
		checkers   []port.HealthChecker
		wantStatus domain.HealthStatus
		wantDown   []string
	}{
		{name: "sin dependencias", wantStatus: domain.HealthUp},
		{
			name:       "todo arriba",
			checkers:   []port.HealthChecker{fakeChecker{name: "postgres"}, fakeChecker{name: "redis"}},
			wantStatus: domain.HealthUp,
		},
		{
			name: "una dependencia caída",
			checkers: []port.HealthChecker{
				fakeChecker{name: "postgres"},
				fakeChecker{name: "redis", err: errors.New("connection refused")},
			},
			wantStatus: domain.HealthDown,
			wantDown:   []string{"redis"},
		},
		{
			name:       "una dependencia lenta supera el límite",
			checkers:   []port.HealthChecker{fakeChecker{name: "postgres", delay: time.Second}},
			wantStatus: domain.HealthDown,
			wantDown:   []string{"postgres"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewHealthService(50*time.Millisecond, tt.checkers...)

			start := time.Now()
			report := svc.Check(context.Background())

			if report.Status != tt.wantStatus {
				t.Fatalf("status = %s, se esperaba %s", report.Status, tt.wantStatus)
			}
			if len(report.Components) != len(tt.checkers) {
				t.Fatalf("componentes = %d, se esperaban %d", len(report.Components), len(tt.checkers))
			}
			for _, name := range tt.wantDown {
				if report.Components[name] != domain.HealthDown {
					t.Fatalf("%s debería estar caído: %+v", name, report.Components)
				}
			}
			if time.Since(start) > 500*time.Millisecond {
				t.Fatal("el límite de tiempo no cortó la comprobación lenta")
			}
		})
	}
}
