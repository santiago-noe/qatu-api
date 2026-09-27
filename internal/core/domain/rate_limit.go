package domain

import "time"

// RateLimitError indica cuánto esperar antes de reintentar. errors.Is(err, ErrTooManyRequests) es true.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string        { return ErrTooManyRequests.Error() }
func (e *RateLimitError) Is(target error) bool { return target == ErrTooManyRequests }

// Limit es un límite de "Max" acciones por "Window" (ventana deslizante).
type Limit struct {
	Max    int
	Window time.Duration
}
