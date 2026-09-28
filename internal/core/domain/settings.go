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
)

// settingKinds: claves admitidas. Una clave desconocida se rechaza (evita errores de tipeo que
// nadie leería). Los timeouts y políticas se agregan con su feature (006 en adelante).
var settingKinds = map[string]settingKind{
	"rental.owner_commission_bps":     kindBasisPoints,
	"rental.client_service_fee_bps":   kindBasisPoints,
	"service.provider_commission_bps": kindBasisPoints,
	"service.client_service_fee_bps":  kindBasisPoints,
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
	switch kind {
	case kindBasisPoints:
		// Entero JSON literal: rechaza "1000" (texto), 10.5, 1e3, null y objetos.
		v, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
		if err != nil || v < 0 || v > 10000 {
			return nil, ErrInvalidSettingValue
		}
		return json.RawMessage(strconv.FormatInt(v, 10)), nil
	}
	return nil, ErrInvalidSettingValue
}
