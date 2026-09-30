// Package jsonschema implementa port.SchemaValidator y port.AttributesValidator con
// santhosh-tekuri/jsonschema (2020-12).
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
	if _, err := compile(schema); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidSchema, err)
	}
	return nil
}

// maxAttributesBytes: los atributos de una herramienta (marca, potencia…) son pocos campos.
const maxAttributesBytes = 8 << 10

// Validate revisa los atributos de una publicación contra el esquema de su categoría. Un esquema
// guardado que ya no compila también rechaza (el admin debe corregirlo).
func (Validator) Validate(schema, doc []byte) error {
	if len(doc) > maxAttributesBytes {
		return domain.ErrListingAttributes
	}
	compiled, err := compile(schema)
	if err != nil {
		return fmt.Errorf("%w: esquema de la categoría: %v", domain.ErrListingAttributes, err)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return domain.ErrListingAttributes
	}
	if err := compiled.Validate(value); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrListingAttributes, err)
	}
	return nil
}

// compile arma el esquema en memoria, sin cargar referencias externas.
func compile(schema []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(noLoader{})
	if err := c.AddResource(resourceURL, doc); err != nil {
		return nil, err
	}
	return c.Compile(resourceURL)
}
