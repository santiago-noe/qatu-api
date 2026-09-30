package domain

import (
	"fmt"
	"testing"
)

func TestPublicPoint(t *testing.T) {
	plaza := GeoPoint{Lat: -13.1631, Lng: -74.2237}
	secret := []byte("secreto-de-prueba")

	a := PublicPoint(plaza, secret, "listing-1", 500)
	if b := PublicPoint(plaza, secret, "listing-1", 500); a != b {
		t.Fatal("determinista: la misma publicación siempre da el mismo punto (no se puede promediar)")
	}
	if PublicPoint(plaza, []byte("otro-secreto"), "listing-1", 500) == a {
		t.Fatal("sin el secreto del servidor no se reproduce el desplazamiento")
	}

	// En muchas publicaciones: el real siempre dentro del círculo y nunca casi encima.
	for i := range 500 {
		p := PublicPoint(plaza, secret, fmt.Sprintf("listing-%d", i), 500)
		d := DistanceMeters(plaza, p)
		if d > 500.5 || d < 0.3*500-0.5 {
			t.Fatalf("listing-%d: a %.1f m del real; quiero entre 150 y 500", i, d)
		}
	}
}

func TestDistanceMeters(t *testing.T) {
	// Plaza de Armas de Ayacucho → mercado Santa Clara: unos 500 m.
	d := DistanceMeters(GeoPoint{Lat: -13.1631, Lng: -74.2237}, GeoPoint{Lat: -13.1590, Lng: -74.2210})
	if d < 450 || d > 600 {
		t.Fatalf("distancia = %.0f m", d)
	}
}
