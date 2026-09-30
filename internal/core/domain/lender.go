package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// LenderKind distingue a una persona de un negocio. Con la feature 021 el negocio acredita su RUC.
type LenderKind string

const (
	LenderPerson   LenderKind = "person"
	LenderBusiness LenderKind = "business"
)

// LenderProfile activa el rol de arrendador (decisión de clarify): celular privado y distrito.
type LenderProfile struct {
	UserID       string
	Kind         LenderKind
	BusinessName string // solo negocios
	Phone        string // +519XXXXXXXX; privado hasta confirmar una reserva (docs/05)
	CityID       string
	ZoneID       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NormalizePeruMobile acepta "987 654 321", "987-654-321", "+51 987654321" o "51987654321" y
// devuelve "+51987654321". Solo celulares (9 dígitos que empiezan con 9).
func NormalizePeruMobile(raw string) (string, error) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r == ' ' || r == '-' || r == '(' || r == ')' || r == '+' || r == '.' {
			return -1
		}
		return 'x' // cualquier otro carácter invalida el número
	}, raw)
	digits = strings.TrimPrefix(digits, "51")
	if len(digits) == 10 && strings.HasPrefix(digits, "0") {
		digits = digits[1:] // algunos escriben 0 delante por costumbre
	}
	if len(digits) != 9 || digits[0] != '9' || strings.ContainsRune(digits, 'x') {
		return "", ErrLenderPhone
	}
	return "+51" + digits, nil
}

// NormalizeLenderProfile limpia y valida el perfil.
func NormalizeLenderProfile(p LenderProfile) (LenderProfile, error) {
	phone, err := NormalizePeruMobile(p.Phone)
	if err != nil {
		return LenderProfile{}, err
	}
	p.Phone = phone
	p.BusinessName = strings.Join(strings.Fields(p.BusinessName), " ")
	switch p.Kind {
	case LenderPerson:
		p.BusinessName = ""
	case LenderBusiness:
		if n := utf8.RuneCountInString(p.BusinessName); n < 2 || n > 80 {
			return LenderProfile{}, ErrLenderBusinessName
		}
	default:
		return LenderProfile{}, ErrLenderBusinessName
	}
	if p.CityID == "" || p.ZoneID == "" {
		return LenderProfile{}, ErrInvalidLocation
	}
	return p, nil
}
