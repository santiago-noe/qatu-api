package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuildCategoryTree(t *testing.T) {
	flat := []Category{
		{ID: "c1", Slug: "construccion"},
		{ID: "t1", ParentID: "c1", Slug: "rotomartillo"},
		{ID: "c2", Slug: "jardin"},
		{ID: "t2", ParentID: "c1", Slug: "amoladora"},
		{ID: "t3", ParentID: "apagada", Slug: "proyector"}, // su raíz no está en la lista
	}
	tree := BuildCategoryTree(flat)
	if len(tree) != 2 || tree[0].Slug != "construccion" || tree[1].Slug != "jardin" {
		t.Fatalf("raíces en el orden recibido: %+v", tree)
	}
	if kids := tree[0].Children; len(kids) != 2 || kids[0].Slug != "rotomartillo" || kids[1].Slug != "amoladora" {
		t.Fatalf("hijos en el orden recibido: %+v", kids)
	}
	for _, root := range tree {
		for _, c := range root.Children {
			if c.Slug == "proyector" {
				t.Fatal("un tipo cuya categoría está apagada no se muestra")
			}
		}
	}
}

func TestGeoPointValidate(t *testing.T) {
	if err := (GeoPoint{Lat: -13.1631, Lng: -74.2237}).Validate(); err != nil {
		t.Fatalf("la Plaza de Armas es válida: %v", err)
	}
	for _, p := range []GeoPoint{{Lat: 91}, {Lat: -91}, {Lng: 181}, {Lng: -181}} {
		if err := p.Validate(); !errors.Is(err, ErrInvalidLocation) {
			t.Fatalf("%+v debe ser inválido, llegó %v", p, err)
		}
	}
}

func TestParseVertical(t *testing.T) {
	if v, ok := ParseVertical("rental"); !ok || v != VerticalRental {
		t.Fatal("rental es válida")
	}
	if _, ok := ParseVertical("vehicles"); ok {
		t.Fatal("una vertical desconocida no es válida")
	}
}

func TestEffectiveSchema(t *testing.T) {
	root := Category{ID: "construccion", AttributesSchema: json.RawMessage(`{"type":"object","title":"Herramienta",
		"properties":{"brand":{"type":"string"},"power_w":{"type":"integer","maximum":20000}},
		"required":["brand"],"additionalProperties":false}`)}
	leaf := Category{ID: "rotomartillo", ParentID: "construccion", AttributesSchema: json.RawMessage(`{"type":"object",
		"properties":{"power_w":{"type":"integer","maximum":3000},"impact_j":{"type":"number"}},"required":["brand","impact_j"]}`)}

	var got struct {
		Title                string                     `json:"title"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal(leaf.EffectiveSchema(root), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Properties) != 3 || string(got.Properties["power_w"]) != `{"maximum":3000,"type":"integer"}` {
		t.Fatalf("hereda de la raíz y el tipo redefine: %s", got.Properties)
	}
	if strings.Join(got.Required, ",") != "brand,impact_j" {
		t.Fatalf("required sin repetidos: %v", got.Required)
	}
	if got.AdditionalProperties == nil || *got.AdditionalProperties || got.Title != "Herramienta" {
		t.Fatalf("cerrado como la raíz: %+v", got)
	}

	if string(root.EffectiveSchema(Category{})) != string(root.AttributesSchema) {
		t.Fatal("una raíz no hereda")
	}
	other := Category{ID: "otra"}
	if string(leaf.EffectiveSchema(other)) != string(leaf.AttributesSchema) {
		t.Fatal("solo hereda de su propio padre")
	}
	broken := Category{ID: "b", AttributesSchema: json.RawMessage(`{`)}
	leaf.ParentID = "b"
	if string(leaf.EffectiveSchema(broken)) != string(leaf.AttributesSchema) {
		t.Fatal("con un esquema roto no se inventa nada")
	}
}
