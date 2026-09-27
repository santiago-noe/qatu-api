package domain

import (
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Parámetros de la feature 001 (decisiones de clarify, 2026-09-27).
const (
	PasswordMinLength = 10
	// Límite superior para acotar el costo de argon2id.
	PasswordMaxLength = 128
	NameMaxLength     = 120
)

type UserStatus string

const (
	UserActive    UserStatus = "active"
	UserSuspended UserStatus = "suspended"
	UserDeleted   UserStatus = "deleted"
)

type Role string

const (
	RoleClient    Role = "client"
	RoleLender    Role = "lender"
	RoleProvider  Role = "provider"
	RoleSupport   Role = "support"
	RoleModerator Role = "moderator"
	RoleAdmin     Role = "admin"
)

// IsInternal indica los roles del equipo de Qatu (ven datos sensibles).
func (r Role) IsInternal() bool {
	return r == RoleSupport || r == RoleModerator || r == RoleAdmin
}

// hasInternalRole: con un rol interno la verificación en dos pasos es obligatoria.
func hasInternalRole(roles []Role) bool { return slices.ContainsFunc(roles, Role.IsInternal) }

// AuthProvider identifica una forma de entrar a la cuenta. Agregar OTP (feature 022)
// es un valor nuevo aquí y un adaptador nuevo; el resto del dominio no cambia.
type AuthProvider string

const (
	ProviderPassword AuthProvider = "password"
	ProviderGoogle   AuthProvider = "google"
	ProviderPhoneOTP AuthProvider = "phone_otp"
)

type ConsentPurpose string

const (
	ConsentTerms     ConsentPurpose = "terms"
	ConsentPrivacy   ConsentPurpose = "privacy"
	ConsentMarketing ConsentPurpose = "marketing"
)

// User es la persona, independiente de cómo inicia sesión.
type User struct {
	ID                string
	Email             string // vacío si la cuenta es solo de celular (feature 022)
	EmailVerifiedAt   *time.Time
	Phone             string
	PhoneVerifiedAt   *time.Time
	Name              string
	AvatarURL         string
	AdultDeclaredAt   *time.Time
	Status            UserStatus
	SuspendedReason   string
	VerificationLevel int
	Roles             []Role
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Version           int
}

func (u User) HasRole(r Role) bool { return slices.Contains(u.Roles, r) }

// IsVerified: nivel 0 alcanzado (correo verificado; en 022 también el celular).
func (u User) IsVerified() bool { return u.EmailVerifiedAt != nil || u.PhoneVerifiedAt != nil }

// CanSignIn: una cuenta eliminada no entra; una suspendida sí, para consultar su historial.
func (u User) CanSignIn() error {
	if u.Status == UserDeleted {
		return ErrAccountDeleted
	}
	return nil
}

// CanTransact: sin verificación o suspendida, solo puede navegar (docs/05).
func (u User) CanTransact() bool { return u.Status == UserActive && u.IsVerified() }

// RequiresTwoFactor: obligatorio para roles internos (decisión de clarify).
func (u User) RequiresTwoFactor() bool { return hasInternalRole(u.Roles) }

// AuthIdentity es una forma de entrar a una cuenta.
type AuthIdentity struct {
	ID              string
	UserID          string
	Provider        AuthProvider
	ProviderSubject string // correo normalizado, "sub" de Google o celular E.164
	SecretHash      string // solo ProviderPassword
	CreatedAt       time.Time
	LastUsedAt      *time.Time
}

type Consent struct {
	Purpose   ConsentPurpose
	Version   string
	GrantedAt time.Time
}

// NormalizeEmail valida y normaliza un correo (minúsculas, sin espacios ni nombre visible).
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "", ErrInvalidEmail
	}
	return email, nil
}

// NormalizeName recorta espacios y valida la longitud.
func NormalizeName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" || utf8.RuneCountInString(name) > NameMaxLength {
		return "", ErrInvalidName
	}
	return name, nil
}

// ValidatePasswordShape aplica la política local (longitud, distinta del correo).
// La comprobación contra contraseñas filtradas la hace el servicio con su port.
func ValidatePasswordShape(password, email string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < PasswordMinLength:
		return ErrPasswordTooShort
	case n > PasswordMaxLength:
		return ErrPasswordTooLong
	case email != "" && strings.EqualFold(password, email):
		return ErrPasswordIsEmail
	}
	return nil
}

// ParseRole valida un rol recibido desde fuera (API, línea de comandos).
func ParseRole(raw string) (Role, bool) {
	switch r := Role(raw); r {
	case RoleClient, RoleLender, RoleProvider, RoleSupport, RoleModerator, RoleAdmin:
		return r, true
	}
	return "", false
}
