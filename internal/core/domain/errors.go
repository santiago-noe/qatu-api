package domain

import "errors"

// Errores de negocio. Los handlers los traducen a respuestas HTTP; nunca se filtran detalles internos.
var (
	ErrInvalidEmail       = errors.New("correo inválido")
	ErrInvalidName        = errors.New("nombre inválido")
	ErrPasswordTooShort   = errors.New("la contraseña es demasiado corta")
	ErrPasswordTooLong    = errors.New("la contraseña es demasiado larga")
	ErrPasswordBreached   = errors.New("la contraseña aparece en filtraciones conocidas")
	ErrPasswordIsEmail    = errors.New("la contraseña no puede ser tu correo")
	ErrAdultRequired      = errors.New("debes declarar que eres mayor de 18 años")
	ErrConsentRequired    = errors.New("debes aceptar los términos y la política de privacidad")
	ErrEmailTaken         = errors.New("el correo ya está registrado")
	ErrInvalidCredentials = errors.New("correo o contraseña incorrectos")
	ErrAccountSuspended   = errors.New("la cuenta está suspendida")
	ErrAccountDeleted     = errors.New("la cuenta fue eliminada")
	ErrNotFound           = errors.New("no encontrado")
)
