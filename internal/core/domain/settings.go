package domain

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// Setting es un valor de platform_settings con su alcance: global, por ciudad, por categoría o
// ambos. Gana el más específico (migración 0003).
type Setting struct {
	ID          string
	Key         string
	CityID      string // vacío = todas las ciudades
	CategoryID  string // vacío = todas las categorías
	Value       json.RawMessage
	Description string
	UpdatedBy   string
	UpdatedAt   time.Time
	Version     int
}

// SettingChange es una entrada del historial (sale de audit_log).
type SettingChange struct {
	At      time.Time
	ActorID string
	IP      string
	Before  json.RawMessage
	After   json.RawMessage
}

// settingKind dice cómo validar el valor de una clave.
type settingKind int

const (
	// kindBasisPoints: entero de 0 a 10000 (0 % a 100 %). Nunca decimales para dinero (constitución).
	kindBasisPoints settingKind = iota + 1
	// kindFactorBps: factor en puntos básicos de 0 a 50000 (hasta 5 veces); 15000 = 150 %.
	kindFactorBps
	// kindMeters: distancia en metros de 100 a 3000 (radio del círculo público en el mapa).
	kindMeters
)

// Rango de cada tipo de ajuste: todos son enteros literales.
var settingRanges = map[settingKind][2]int64{
	kindBasisPoints: {0, 10_000},
	kindFactorBps:   {0, 50_000},
	kindMeters:      {100, 3_000},
}

// settingKinds: claves admitidas. Una clave desconocida se rechaza (evita errores de tipeo que
// nadie leería). Los timeouts y políticas se agregan con su feature (006 en adelante).
var settingKinds = map[string]settingKind{
	"rental.owner_commission_bps":     kindBasisPoints,
	"rental.client_service_fee_bps":   kindBasisPoints,
	"service.provider_commission_bps": kindBasisPoints,
	"service.client_service_fee_bps":  kindBasisPoints,
	// Feature 003: garantía sugerida por riesgo, rango de ajuste y radio público.
	"listings.deposit_low_bps":        kindBasisPoints,
	"listings.deposit_medium_bps":     kindBasisPoints,
	"listings.deposit_high_bps":       kindBasisPoints,
	"listings.deposit_min_factor_bps": kindFactorBps,
	"listings.deposit_max_factor_bps": kindFactorBps,
	"listings.public_radius_m":        kindMeters,
}

// IsKnownSetting indica si la clave está admitida.
func IsKnownSetting(key string) bool {
	_, ok := settingKinds[key]
	return ok
}

// NormalizeSettingValue valida el valor según su clave y lo devuelve en forma canónica.
func NormalizeSettingValue(key string, raw json.RawMessage) (json.RawMessage, error) {
	kind, ok := settingKinds[key]
	if !ok {
		return nil, ErrUnknownSetting
	}
	bounds := settingRanges[kind]
	// Entero JSON literal: rechaza "1000" (texto), 10.5, 1e3, null y objetos.
	v, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	if err != nil || v < bounds[0] || v > bounds[1] {
		return nil, ErrInvalidSettingValue
	}
	return json.RawMessage(strconv.FormatInt(v, 10)), nil
}

// SettingScope es dónde se aplica un ajuste: la ciudad y la categoría de una transacción. La
// categoría padre también cuenta: una comisión para Construcción vale para Rotomartillo.
type SettingScope struct {
	CityID           string
	CategoryID       string
	ParentCategoryID string
}

// specificity ordena los alcances que aplican (mayor = más específico) y 0 si no aplica. La
// categoría pesa más que la ciudad: ciudad+categoría > categoría > ciudad+padre > padre > ciudad > global.
func (sc SettingScope) specificity(s Setting) int {
	cityRank := 0
	switch s.CityID {
	case "":
	case sc.CityID:
		cityRank = 1
	default:
		return 0
	}
	categoryRank := 0
	switch {
	case s.CategoryID == "":
	case s.CategoryID == sc.CategoryID:
		categoryRank = 2
	case s.CategoryID == sc.ParentCategoryID:
		categoryRank = 1
	default:
		return 0
	}
	return 1 + categoryRank*2 + cityRank
}

// ResolveSetting devuelve el valor de la clave que aplica al alcance, o false si no hay ninguno.
func ResolveSetting(all []Setting, key string, scope SettingScope) (Setting, bool) {
	var best Setting
	bestRank := 0
	for _, s := range all {
		if s.Key != key {
			continue
		}
		if r := scope.specificity(s); r > bestRank {
			best, bestRank = s, r
		}
	}
	return best, bestRank > 0
}

// SnapshotValue es un ajuste copiado en una transacción, con la versión exacta que se usó.
type SnapshotValue struct {
	Value     json.RawMessage
	SettingID string
	Version   int
}

// SettingsSnapshot es la copia de los ajustes que una transacción guarda al crearse
// (price_snapshot en 006 y 008): un cambio posterior de comisión no la afecta (spec 002).
type SettingsSnapshot struct {
	TakenAt time.Time
	Values  map[string]SnapshotValue
}

// TakeSettingsSnapshot copia las claves pedidas en su alcance. Cada clave necesita al menos su
// valor global (las migraciones lo siembran): si falta es un error de configuración, no un 0.
func TakeSettingsSnapshot(all []Setting, keys []string, scope SettingScope, now time.Time) (SettingsSnapshot, error) {
	snap := SettingsSnapshot{TakenAt: now, Values: make(map[string]SnapshotValue, len(keys))}
	for _, key := range keys {
		if !IsKnownSetting(key) {
			return SettingsSnapshot{}, ErrUnknownSetting
		}
		s, ok := ResolveSetting(all, key, scope)
		if !ok {
			return SettingsSnapshot{}, ErrSettingMissing
		}
		snap.Values[key] = SnapshotValue{Value: s.Value, SettingID: s.ID, Version: s.Version}
	}
	return snap, nil
}

// Int devuelve un valor entero de la copia (todos los ajustes actuales lo son).
func (s SettingsSnapshot) Int(key string) (int64, error) {
	v, ok := s.Values[key]
	if !ok {
		return 0, ErrSettingMissing
	}
	n, err := strconv.ParseInt(string(v.Value), 10, 64)
	if err != nil {
		return 0, ErrInvalidSettingValue
	}
	return n, nil
}
