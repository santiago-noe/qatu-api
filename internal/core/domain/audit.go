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
	AuditLocationUpdated  = "user.location_updated"

	// Catálogo y configuración (feature 002).
	AuditCategoryCreated   = "catalog.category_created"
	AuditCategoryUpdated   = "catalog.category_updated"
	AuditCityCreated       = "catalog.city_created"
	AuditCityUpdated       = "catalog.city_updated"
	AuditZoneCreated       = "catalog.zone_created"
	AuditZoneUpdated       = "catalog.zone_updated"
	AuditCategoryCityScope = "catalog.category_city_scope"
	AuditSettingChanged    = "platform.setting_changed"

	// Al prohibir una categoría, sus publicaciones activas salen del catálogo (spec 002).
	AuditListingsRetired = "listing.retired_prohibited_category"

	// Publicaciones de herramientas (feature 003).
	AuditLenderActivated  = "lender.activated"
	AuditLenderUpdated    = "lender.updated"
	AuditListingCreated   = "listing.created"
	AuditListingUpdated   = "listing.updated"
	AuditListingStatus    = "listing.status_changed"
	AuditListingBlocked   = "listing.dates_blocked"
	AuditListingUnblocked = "listing.dates_unblocked"
	AuditListingApproved  = "listing.approved"
	AuditListingRejected  = "listing.rejected"
)
