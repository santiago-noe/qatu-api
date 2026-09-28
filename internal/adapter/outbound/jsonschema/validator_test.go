package jsonschema

import (
	"errors"
	"strings"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestCheckSchema(t *testing.T) {
	v := New()
	valid := `{"type":"object","properties":{"power_w":{"type":"integer","minimum":1},
		"power_source":{"type":"string","enum":["electric","battery"]}},"additionalProperties":false}`
	if err := v.CheckSchema([]byte(valid)); err != nil {
		t.Fatalf("un esquema de atributos válido pasa: %v", err)
	}

	for name, schema := range map[string]string{
		"no es JSON":              `{"type":`,
		"raíz que no es objeto":   `{"type":"string"}`,
		"sin tipo":                `{"properties":{}}`,
		"tipo inexistente":        `{"type":"object","properties":{"x":{"type":"entero"}}}`,
		"mínimo que no es número": `{"type":"object","properties":{"x":{"type":"integer","minimum":"uno"}}}`,
		"referencia a un archivo": `{"type":"object","properties":{"x":{"$ref":"file:///etc/passwd"}}}`,
		"referencia a la red":     `{"type":"object","properties":{"x":{"$ref":"https://evil.example/s.json"}}}`,
		"demasiado grande":        `{"type":"object","description":"` + strings.Repeat("a", 40<<10) + `"}`,
		"vacío":                   ``,
	} {
		t.Run(name, func(t *testing.T) {
			if err := v.CheckSchema([]byte(schema)); !errors.Is(err, domain.ErrInvalidSchema) {
				t.Fatalf("debe rechazarse con ErrInvalidSchema, llegó %v", err)
			}
		})
	}
}
