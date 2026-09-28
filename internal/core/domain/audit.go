package domain

// AuditEntry es una fila del registro de auditoría inmutable (audit_log).
type AuditEntry struct {
	ActorID  string // vacío si la acción no tiene autor autenticado (por ejemplo, un registro)
	Action   string // user.registered, user.login, session.revoked_all…
	Entity   string
	EntityID string
	Before   any
	After    any
	IP       string
}

// Acciones de auditoría de la feature 001.
const (
	AuditUserRegistered   = "user.registered"
	AuditUserLogin        = "user.login"
	AuditSessionsRevoked  = "user.sessions_revoked"
	AuditPasswordRehashed = "user.password_rehashed"
	AuditEmailVerified    = "user.email_verified"
	AuditPasswordReset    = "user.password_reset"
	AuditPasswordChanged  = "user.password_changed"
	AuditProfileUpdated   = "user.profile_updated"
	AuditSessionRevoked   = "user.session_revoked"
	AuditRolesChanged     = "user.roles_changed"
	AuditStatusChanged    = "user.status_changed"
	AuditTwoFactorPassed  = "user.two_factor_passed"
	AuditIdentityLinked   = "user.identity_linked"

	// Catálogo y configuración (feature 002).
	AuditCategoryCreated   = "catalog.category_created"
	AuditCategoryUpdated   = "catalog.category_updated"
	AuditCityUpdated       = "catalog.city_updated"
	AuditCategoryCityScope = "catalog.category_city_scope"
	AuditSettingChanged    = "platform.setting_changed"
)
