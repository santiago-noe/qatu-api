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

func TestValidateAttributes(t *testing.T) {
	v := New()
	schema := []byte(`{"type":"object","properties":{"marca":{"type":"string","minLength":1},
		"potencia_w":{"type":"integer","minimum":1}},"required":["marca"],"additionalProperties":false}`)
	if err := v.Validate(schema, []byte(`{"marca":"Bosch","potencia_w":800}`)); err != nil {
		t.Fatalf("atributos válidos: %v", err)
	}
	for name, doc := range map[string]string{
		"falta la marca":    `{"potencia_w":800}`,
		"potencia en texto": `{"marca":"Bosch","potencia_w":"800"}`,
		"campo desconocido": `{"marca":"Bosch","color":"azul"}`,
		"no es JSON":        `{"marca":`,
		"demasiado grande":  `{"marca":"` + strings.Repeat("a", 9000) + `"}`,
	} {
		if err := v.Validate(schema, []byte(doc)); !errors.Is(err, domain.ErrListingAttributes) {
			t.Errorf("%s: quiero ErrListingAttributes, llegó %v", name, err)
		}
	}
	if err := v.Validate([]byte(`{"type":`), []byte(`{}`)); !errors.Is(err, domain.ErrListingAttributes) {
		t.Fatal("un esquema roto también rechaza")
	}
}
