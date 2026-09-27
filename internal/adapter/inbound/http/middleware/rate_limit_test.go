package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

type countingLimiter struct{ keys map[string]int }

func (l *countingLimiter) Allow(_ context.Context, key string, limit domain.Limit) (bool, time.Duration, error) {
	l.keys[key]++
	return l.keys[key] <= limit.Max, time.Minute, nil
}

func TestRateLimitByIP(t *testing.T) {
	limiter := &countingLimiter{keys: map[string]int{}}
	var got error
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		got = err
		return c.SendStatus(fiber.StatusTooManyRequests)
	}})
	app.Post("/login", RateLimitByIP(limiter, "login", domain.Limit{Max: 2, Window: time.Minute}), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	for i, want := range []int{200, 200, 429} {
		res, _ := app.Test(httptest.NewRequest(fiber.MethodPost, "/login", nil))
		if res.StatusCode != want {
			t.Fatalf("petición %d: status %d, se esperaba %d", i+1, res.StatusCode, want)
		}
	}
	var rl *domain.RateLimitError
	if !errors.As(got, &rl) || rl.RetryAfter != time.Minute || !errors.Is(got, domain.ErrTooManyRequests) {
		t.Fatalf("debe devolver RateLimitError con la espera: %v", got)
	}
	for key := range limiter.keys {
		if key != "login:ip:0.0.0.0" {
			t.Fatalf("la clave incluye la ruta y la IP, llegó %q", key)
		}
	}
}
