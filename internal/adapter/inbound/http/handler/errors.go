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
	{domain.ErrRoleNotAssignable, fiber.StatusUnprocessableEntity, "rol_no_asignable"},
	{domain.ErrSelfLockout, fiber.StatusUnprocessableEntity, "autobloqueo"},
	{domain.ErrSuspensionReason, fiber.StatusUnprocessableEntity, "motivo_requerido"},
	{domain.ErrInvalidStatus, fiber.StatusUnprocessableEntity, "estado_invalido"},
	{domain.ErrTwoFactorRequired, fiber.StatusForbidden, "dos_pasos_requerido"},
	{domain.ErrTwoFactorNotPending, fiber.StatusConflict, "dos_pasos_no_pendiente"},
	{domain.ErrHumanCheckFailed, fiber.StatusForbidden, "captcha_invalido"},
	{domain.ErrHumanCheckUnavailable, fiber.StatusServiceUnavailable, "captcha_no_disponible"},
	{domain.ErrOAuthUnavailable, fiber.StatusServiceUnavailable, "google_no_disponible"},
	{domain.ErrOAuthState, fiber.StatusBadRequest, "google_estado_invalido"},
	{domain.ErrOAuthFailed, fiber.StatusUnauthorized, "google_fallido"},
	{domain.ErrOAuthEmailUnverified, fiber.StatusUnprocessableEntity, "google_correo_no_verificado"},
	{domain.ErrOAuthLinkNeedsPassword, fiber.StatusConflict, "vincular_con_contrasena"},
	{domain.ErrOAuthSignupRequired, fiber.StatusConflict, "registro_requerido"},
	{domain.ErrOAuthAlreadyLinked, fiber.StatusConflict, "google_ya_vinculado"},
	{domain.ErrAccountDeleted, fiber.StatusForbidden, "cuenta_eliminada"},
	{domain.ErrInvalidLocation, fiber.StatusBadRequest, "ubicacion_invalida"},
	{domain.ErrOutOfCoverage, fiber.StatusNotFound, "fuera_de_cobertura"},
	{domain.ErrInvalidVertical, fiber.StatusBadRequest, "vertical_invalida"},
	{domain.ErrInvalidSlug, fiber.StatusUnprocessableEntity, "slug_invalido"},
	{domain.ErrInvalidDescription, fiber.StatusUnprocessableEntity, "descripcion_invalida"},
	{domain.ErrInvalidIcon, fiber.StatusUnprocessableEntity, "icono_invalido"},
	{domain.ErrInvalidRisk, fiber.StatusUnprocessableEntity, "riesgo_invalido"},
	{domain.ErrInvalidSortOrder, fiber.StatusUnprocessableEntity, "orden_invalido"},
	{domain.ErrInvalidSchema, fiber.StatusUnprocessableEntity, "esquema_invalido"},
	{domain.ErrSlugTaken, fiber.StatusConflict, "slug_registrado"},
	{domain.ErrCategoryTree, fiber.StatusUnprocessableEntity, "arbol_invalido"},
	{domain.ErrUnknownSetting, fiber.StatusUnprocessableEntity, "ajuste_desconocido"},
	{domain.ErrInvalidSettingValue, fiber.StatusUnprocessableEntity, "valor_invalido"},
	{domain.ErrInvalidUbigeo, fiber.StatusUnprocessableEntity, "ubigeo_invalido"},
	{domain.ErrCityTaken, fiber.StatusConflict, "ciudad_registrada"},
	{domain.ErrZoneTaken, fiber.StatusConflict, "distrito_registrado"},
	{domain.ErrInvalidBoundary, fiber.StatusUnprocessableEntity, "limite_invalido"},
	{domain.ErrZoneFarFromCity, fiber.StatusUnprocessableEntity, "limite_lejano"},
	{domain.ErrZoneOverlap, fiber.StatusConflict, "limite_superpuesto"},
	{domain.ErrCityWithoutZones, fiber.StatusConflict, "ciudad_sin_distritos"},
	{domain.ErrLenderProfileRequired, fiber.StatusForbidden, "perfil_arrendador_requerido"},
	{domain.ErrLenderPhone, fiber.StatusUnprocessableEntity, "celular_invalido"},
	{domain.ErrLenderBusinessName, fiber.StatusUnprocessableEntity, "nombre_negocio_invalido"},
	{domain.ErrLenderEmailUnverified, fiber.StatusForbidden, "correo_no_verificado"},
	{domain.ErrLenderTermsRequired, fiber.StatusUnprocessableEntity, "condiciones_requeridas"},
	{domain.ErrListingTitle, fiber.StatusUnprocessableEntity, "titulo_invalido"},
	{domain.ErrListingText, fiber.StatusUnprocessableEntity, "texto_largo"},
	{domain.ErrListingAccessories, fiber.StatusUnprocessableEntity, "accesorios_invalidos"},
	{domain.ErrListingAmount, fiber.StatusUnprocessableEntity, "monto_invalido"},
	{domain.ErrListingBookingMode, fiber.StatusUnprocessableEntity, "modo_reserva_invalido"},
	{domain.ErrListingCancelPolicy, fiber.StatusUnprocessableEntity, "politica_invalida"},
	{domain.ErrListingVerification, fiber.StatusUnprocessableEntity, "verificacion_invalida"},
	{domain.ErrListingDurations, fiber.StatusUnprocessableEntity, "duraciones_invalidas"},
	{domain.ErrListingAttributes, fiber.StatusUnprocessableEntity, "atributos_invalidos"},
	{domain.ErrListingCategory, fiber.StatusUnprocessableEntity, "categoria_no_publicable"},
	{domain.ErrListingProhibited, fiber.StatusUnprocessableEntity, "categoria_prohibida"},
	{domain.ErrListingCategoryLocked, fiber.StatusConflict, "categoria_fija"},
	{domain.ErrListingDayPrice, fiber.StatusUnprocessableEntity, "precio_dia_requerido"},
	{domain.ErrListingReplacementValue, fiber.StatusUnprocessableEntity, "valor_reposicion_requerido"},
	{domain.ErrListingDeposit, fiber.StatusUnprocessableEntity, "garantia_fuera_de_rango"},
	{domain.ErrListingFulfillment, fiber.StatusUnprocessableEntity, "entrega_requerida"},
	{domain.ErrListingPickupLocation, fiber.StatusUnprocessableEntity, "punto_recojo_invalido"},
	{domain.ErrListingDeliveryZones, fiber.StatusUnprocessableEntity, "distritos_delivery_invalidos"},
	{domain.ErrListingPhotos, fiber.StatusUnprocessableEntity, "fotos_insuficientes"},
	{domain.ErrListingOwnerLocation, fiber.StatusConflict, "perfil_sin_distrito"},
	{domain.ErrListingTransition, fiber.StatusConflict, "transicion_invalida"},
	{domain.ErrListingNotEditable, fiber.StatusConflict, "publicacion_no_editable"},
	{domain.ErrListingVersion, fiber.StatusConflict, "version_desactualizada"},
	{domain.ErrAvailabilityOverlap, fiber.StatusConflict, "fechas_ocupadas"},
	{domain.ErrAvailabilityPeriod, fiber.StatusUnprocessableEntity, "periodo_invalido"},
	{domain.ErrSettingMissing, fiber.StatusInternalServerError, "ajuste_faltante"},
	{domain.ErrPhotoType, fiber.StatusUnprocessableEntity, "foto_invalida"},
	{domain.ErrPhotoLimit, fiber.StatusConflict, "limite_fotos"},
	{domain.ErrPhotoNotUploaded, fiber.StatusConflict, "foto_no_subida"},
	{domain.ErrPhotoInvalid, fiber.StatusUnprocessableEntity, "imagen_ilegible"},
	{domain.ErrPhotoOrder, fiber.StatusUnprocessableEntity, "orden_invalido_fotos"},
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
