// Package service contiene los casos de uso; orquesta los ports.
package service

import (
	"context"
	"sync"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

type HealthService struct {
	checkers []port.HealthChecker
	timeout  time.Duration
}

func NewHealthService(timeout time.Duration, checkers ...port.HealthChecker) *HealthService {
	return &HealthService{checkers: checkers, timeout: timeout}
}

// Check consulta todas las dependencias en paralelo, cada una con su límite de tiempo.
func (s *HealthService) Check(ctx context.Context) domain.HealthReport {
	report := domain.HealthReport{
		Status:     domain.HealthUp,
		Components: make(map[string]domain.HealthStatus, len(s.checkers)),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, checker := range s.checkers {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()

			status := domain.HealthUp
			if err := checker.Ping(ctx); err != nil {
				status = domain.HealthDown
			}

			mu.Lock()
			defer mu.Unlock()
			report.Components[checker.Name()] = status
			if status == domain.HealthDown {
				report.Status = domain.HealthDown
			}
		})
	}
	wg.Wait()
	return report
}
