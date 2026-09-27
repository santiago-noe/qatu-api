package handler

import (
	"errors"
	"math"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// APIError es la respuesta de error pública: {"error": "codigo", "message": "texto"}.
// El código es estable para el frontend; el mensaje, legible en español.
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *APIError) Error() string   { return e.Code }
func (e *APIError) HTTPStatus() int { return e.Status }

// domainErrors traduce errores de negocio a respuestas HTTP.
var domainErrors = []struct {
	err    error
	status int
	code   string
}{
	{domain.ErrInvalidEmail, fiber.StatusUnprocessableEntity, "correo_invalido"},
	{domain.ErrInvalidName, fiber.StatusUnprocessableEntity, "nombre_invalido"},
	{domain.ErrPasswordTooShort, fiber.StatusUnprocessableEntity, "contrasena_corta"},
	{domain.ErrPasswordTooLong, fiber.StatusUnprocessableEntity, "contrasena_larga"},
	{domain.ErrPasswordBreached, fiber.StatusUnprocessableEntity, "contrasena_filtrada"},
	{domain.ErrPasswordIsEmail, fiber.StatusUnprocessableEntity, "contrasena_igual_correo"},
	{domain.ErrAdultRequired, fiber.StatusUnprocessableEntity, "mayoria_de_edad_requerida"},
	{domain.ErrConsentRequired, fiber.StatusUnprocessableEntity, "consentimiento_requerido"},
	{domain.ErrEmailTaken, fiber.StatusConflict, "correo_registrado"},
	{domain.ErrInvalidCredentials, fiber.StatusUnauthorized, "credenciales_invalidas"},
	{domain.ErrSessionInvalid, fiber.StatusUnauthorized, "no_autenticado"},
	{domain.ErrForbidden, fiber.StatusForbidden, "sin_permiso"},
	{domain.ErrAccountSuspended, fiber.StatusForbidden, "cuenta_suspendida"},
	{domain.ErrNotFound, fiber.StatusNotFound, "no_encontrado"},
	{domain.ErrTooManyRequests, fiber.StatusTooManyRequests, "demasiados_intentos"},
	{domain.ErrCodeInvalid, fiber.StatusUnprocessableEntity, "codigo_invalido"},
	{domain.ErrCodeExhausted, fiber.StatusUnprocessableEntity, "codigo_agotado"},
	{domain.ErrEmailAlreadyVerified, fiber.StatusConflict, "correo_ya_verificado"},
	// 422 y no 401: la sesión es válida; si fuera 401 el BFF pensaría que venció.
	{domain.ErrCurrentPasswordInvalid, fiber.StatusUnprocessableEntity, "contrasena_actual_incorrecta"},
}

// toAPIError convierte cualquier error en una respuesta segura; lo desconocido es un 500 sin detalles.
func toAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	for _, m := range domainErrors {
		if errors.Is(err, m.err) {
			return &APIError{Status: m.status, Code: m.code, Message: m.err.Error()}
		}
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		// Los middlewares usan como mensaje un código conocido ("no_autenticado"): se completa el texto.
		for _, m := range domainErrors {
			if m.code == fe.Message {
				return &APIError{Status: fe.Code, Code: m.code, Message: m.err.Error()}
			}
		}
		return &APIError{Status: fe.Code, Code: fe.Message, Message: fe.Message}
	}
	return &APIError{Status: fiber.StatusInternalServerError, Code: "error_interno", Message: "Ocurrió un error. Intenta de nuevo."}
}

// StatusOf devuelve el estado HTTP que recibirá el cliente para un error (lo usa el log).
func StatusOf(err error) int { return toAPIError(err).Status }

// ErrorHandler es el manejador de errores de Fiber para toda la API.
func ErrorHandler(c fiber.Ctx, err error) error {
	var rl *domain.RateLimitError
	if errors.As(err, &rl) && rl.RetryAfter > 0 {
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(int(math.Ceil(rl.RetryAfter.Seconds()))))
	}
	apiErr := toAPIError(err)
	return c.Status(apiErr.Status).JSON(apiErr)
}

func badRequest(message string) *APIError {
	return &APIError{Status: fiber.StatusBadRequest, Code: "solicitud_invalida", Message: message}
}
