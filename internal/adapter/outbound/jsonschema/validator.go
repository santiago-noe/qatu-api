// Package jsonschema implementa port.SchemaValidator con santhosh-tekuri/jsonschema (2020-12).
package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// maxSchemaBytes: un esquema de atributos es chico; el límite evita esquemas gigantes.
const maxSchemaBytes = 32 << 10

// resourceURL es un nombre interno para compilar el esquema en memoria.
const resourceURL = "urn:qatu:attributes-schema"

// errExternalRef: el esquema no puede traer otros documentos (ni archivos del servidor ni la red).
var errExternalRef = errors.New("referencias externas no permitidas")

type noLoader struct{}

func (noLoader) Load(string) (any, error) { return nil, errExternalRef }

type Validator struct{}

func New() Validator { return Validator{} }

// CheckSchema exige un JSON Schema válido cuyo tipo raíz sea "object" (los atributos de una
// publicación son un objeto: potencia, voltaje…).
func (Validator) CheckSchema(schema json.RawMessage) error {
	if len(schema) == 0 || len(schema) > maxSchemaBytes {
		return domain.ErrInvalidSchema
	}
	var root struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(schema, &root); err != nil || root.Type != "object" {
		return domain.ErrInvalidSchema
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return domain.ErrInvalidSchema
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(noLoader{})
	if err := c.AddResource(resourceURL, doc); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidSchema, err)
	}
	if _, err := c.Compile(resourceURL); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidSchema, err)
	}
	return nil
}
