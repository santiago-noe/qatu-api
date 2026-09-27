package logging

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

func TestStatusOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		want int
	}{
		{name: "sin error usa el estado de la respuesta", code: fiber.StatusCreated, want: fiber.StatusCreated},
		{name: "error de Fiber usa su código", err: fiber.ErrNotFound, want: fiber.StatusNotFound},
		{name: "error cualquiera es 500", err: errors.New("fallo"), want: fiber.StatusInternalServerError},
	}

	app := fiber.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := app.AcquireCtx(&fasthttp.RequestCtx{})
			defer app.ReleaseCtx(c)
			if tt.code != 0 {
				c.Status(tt.code)
			}
			if got := StatusOf(c, tt.err); got != tt.want {
				t.Fatalf("StatusOf = %d, se esperaba %d", got, tt.want)
			}
		})
	}
}
