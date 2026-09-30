package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// metersPerDegreeLat: metros por grado de latitud (aproximación esférica, suficiente para < 5 km).
const metersPerDegreeLat = 111_320.0

// minOffsetShare: el punto público nunca cae a menos del 30 % del radio del real; si cayera casi
// encima, el círculo seguiría revelando la dirección.
const minOffsetShare = 0.3

// PublicPoint desplaza el punto exacto dentro de un círculo de radiusM metros (docs/03, "Ubicación
// y mapa"). El desplazamiento sale de HMAC(secret, seed) con seed = id de la publicación: es el mismo
// cada vez que se guarda, así que no se puede promediar para recuperar el punto real, y sin el
// secreto del servidor no se puede calcular. El punto real queda siempre dentro del círculo público
// (distancia ≤ radiusM).
func PublicPoint(exact GeoPoint, secret []byte, seed string, radiusM int) GeoPoint {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(seed))
	sum := mac.Sum(nil)
	u1 := unitFloat(sum[0:8])
	u2 := unitFloat(sum[8:16])

	angle := 2 * math.Pi * u1
	// sqrt: reparte el punto de forma pareja en el anillo (no se amontona cerca del centro).
	share := minOffsetShare + (1-minOffsetShare)*math.Sqrt(u2)
	distance := float64(radiusM) * share

	dLat := distance * math.Cos(angle) / metersPerDegreeLat
	dLng := distance * math.Sin(angle) / (metersPerDegreeLat * math.Cos(exact.Lat*math.Pi/180))
	return GeoPoint{Lat: exact.Lat + dLat, Lng: exact.Lng + dLng}
}

// unitFloat convierte 8 bytes en un número en [0, 1).
func unitFloat(b []byte) float64 {
	return float64(binary.BigEndian.Uint64(b)>>11) / float64(uint64(1)<<53)
}

// DistanceMeters es la distancia aproximada entre dos puntos (equirrectangular: < 0,1 % de error en
// distancias de ciudad). Sirve para comprobar el desplazamiento y mostrar "a 1,2 km".
func DistanceMeters(a, b GeoPoint) float64 {
	latRad := (a.Lat + b.Lat) / 2 * math.Pi / 180
	dx := (b.Lng - a.Lng) * math.Cos(latRad) * metersPerDegreeLat
	dy := (b.Lat - a.Lat) * metersPerDegreeLat
	return math.Hypot(dx, dy)
}
