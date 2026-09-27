package domain

import "errors"

// Errores de negocio. Los handlers los traducen a respuestas HTTP; nunca se filtran detalles internos.
var (
	ErrInvalidEmail           = errors.New("correo inválido")
	ErrInvalidName            = errors.New("nombre inválido")
	ErrPasswordTooShort       = errors.New("la contraseña es demasiado corta")
	ErrPasswordTooLong        = errors.New("la contraseña es demasiado larga")
	ErrPasswordBreached       = errors.New("la contraseña aparece en filtraciones conocidas")
	ErrPasswordIsEmail        = errors.New("la contraseña no puede ser tu correo")
	ErrAdultRequired          = errors.New("debes declarar que eres mayor de 18 años")
	ErrConsentRequired        = errors.New("debes aceptar los términos y la política de privacidad")
	ErrEmailTaken             = errors.New("el correo ya está registrado")
	ErrInvalidCredentials     = errors.New("correo o contraseña incorrectos")
	ErrAccountSuspended       = errors.New("la cuenta está suspendida")
	ErrAccountDeleted         = errors.New("la cuenta fue eliminada")
	ErrNotFound               = errors.New("no encontrado")
	ErrSessionInvalid         = errors.New("sesión inválida o vencida")
	ErrForbidden              = errors.New("no tienes permiso para esta acción")
	ErrTooManyRequests        = errors.New("demasiados intentos, espera un momento")
	ErrCodeInvalid            = errors.New("el código es incorrecto o venció")
	ErrCodeExhausted          = errors.New("agotaste los intentos, pide un código nuevo")
	ErrEmailAlreadyVerified   = errors.New("tu correo ya está verificado")
	ErrCurrentPasswordInvalid = errors.New("la contraseña actual no es correcta")
	ErrRoleNotAssignable      = errors.New("ese rol no lo asigna un administrador")
	ErrSelfLockout            = errors.New("no puedes quitarte el rol de admin ni suspender tu propia cuenta")
	ErrSuspensionReason       = errors.New("indica el motivo de la suspensión")
	ErrInvalidStatus          = errors.New("estado de cuenta inválido")
)
