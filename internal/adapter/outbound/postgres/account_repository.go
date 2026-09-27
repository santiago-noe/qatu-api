package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

const uniqueViolation = "23505"

// AccountRepository implementa port.AccountRepository y port.AuditLog.
type AccountRepository struct {
	db *Client
}

func NewAccountRepository(db *Client) *AccountRepository { return &AccountRepository{db: db} }

// CreateAccount inserta usuario, identidad, rol cliente, consentimientos y auditoría en una transacción.
func (r *AccountRepository) CreateAccount(ctx context.Context, a port.NewAccount) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		u := a.User
		// Con Google el correo ya llega verificado (EmailVerifiedAt) y con foto.
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (id, email, email_verified_at, name, avatar_url, adult_declared_at, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $8)`,
			u.ID, u.Email, u.EmailVerifiedAt, u.Name, u.AvatarURL, u.AdultDeclaredAt, u.Status, u.CreatedAt); err != nil {
			return err
		}
		if err := insertIdentity(ctx, tx, a.Identity); err != nil {
			return err
		}
		for _, role := range u.Roles {
			if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role, granted_at) VALUES ($1, $2, $3)`,
				u.ID, role, u.CreatedAt); err != nil {
				return err
			}
		}
		for _, c := range a.Consents {
			if _, err := tx.Exec(ctx, `
				INSERT INTO consents (user_id, purpose, version, granted_at, ip) VALUES ($1, $2, $3, $4, $5)`,
				u.ID, c.Purpose, c.Version, c.GrantedAt, parseIP(a.Audit.IP)); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, a.Audit)
	})

	if isUniqueViolation(err, "") {
		return domain.ErrEmailTaken
	}
	return err
}

// LinkIdentity agrega una identidad a una cuenta existente y audita, en una transacción.
func (r *AccountRepository) LinkIdentity(ctx context.Context, identity domain.AuthIdentity, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if err := insertIdentity(ctx, tx, identity); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	if isUniqueViolation(err, "auth_identities_one_per_provider") {
		return domain.ErrOAuthAlreadyLinked
	}
	return err
}

func insertIdentity(ctx context.Context, tx pgx.Tx, id domain.AuthIdentity) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO auth_identities (id, user_id, provider, provider_subject, secret_hash, created_at, last_used_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7)`,
		id.ID, id.UserID, id.Provider, id.ProviderSubject, id.SecretHash, id.CreatedAt, id.LastUsedAt)
	return err
}

// isUniqueViolation: la restricción constraint (vacía = cualquiera) rechazó un duplicado.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation &&
		(constraint == "" || pgErr.ConstraintName == constraint)
}

func (r *AccountRepository) FindIdentity(ctx context.Context, provider domain.AuthProvider, subject string) (domain.AuthIdentity, error) {
	return r.findIdentityWhere(ctx, "provider = $1 AND provider_subject = $2", provider, subject)
}

func (r *AccountRepository) FindUserIdentity(ctx context.Context, userID string, provider domain.AuthProvider) (domain.AuthIdentity, error) {
	return r.findIdentityWhere(ctx, "user_id = $1 AND provider = $2", userID, provider)
}

// findIdentityWhere es la única consulta de identidades; cambia solo el filtro.
func (r *AccountRepository) findIdentityWhere(ctx context.Context, where string, args ...any) (domain.AuthIdentity, error) {
	var id domain.AuthIdentity
	var secret *string
	err := r.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, provider, provider_subject, secret_hash, created_at, last_used_at
		FROM auth_identities WHERE `+where, args...).
		Scan(&id.ID, &id.UserID, &id.Provider, &id.ProviderSubject, &secret, &id.CreatedAt, &id.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthIdentity{}, domain.ErrNotFound
	}
	id.SecretHash = deref(secret)
	return id, err
}

func (r *AccountRepository) FindUser(ctx context.Context, userID string) (domain.User, error) {
	return r.findUserWhere(ctx, "u.id = $1", userID)
}

func (r *AccountRepository) FindUserByEmail(ctx context.Context, email string) (domain.User, error) {
	return r.findUserWhere(ctx, "u.email = $1", email)
}

// findUserWhere es la única consulta de usuario con sus roles; cambia solo el filtro.
func (r *AccountRepository) findUserWhere(ctx context.Context, where string, arg any) (domain.User, error) {
	var u domain.User
	var email, phone, avatar, reason *string
	var roles []string
	err := r.db.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.email_verified_at, u.phone, u.phone_verified_at, u.name, u.avatar_url,
		       u.adult_declared_at, u.status, u.suspended_reason, u.verification_level,
		       u.created_at, u.updated_at, u.version,
		       COALESCE(array_agg(r.role ORDER BY r.role) FILTER (WHERE r.role IS NOT NULL), '{}')
		FROM users u LEFT JOIN user_roles r ON r.user_id = u.id
		WHERE `+where+`
		GROUP BY u.id`, arg).Scan(
		&u.ID, &email, &u.EmailVerifiedAt, &phone, &u.PhoneVerifiedAt, &u.Name, &avatar,
		&u.AdultDeclaredAt, &u.Status, &reason, &u.VerificationLevel,
		&u.CreatedAt, &u.UpdatedAt, &u.Version, &roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	u.Email, u.Phone, u.AvatarURL, u.SuspendedReason = deref(email), deref(phone), deref(avatar), deref(reason)
	for _, role := range roles {
		u.Roles = append(u.Roles, domain.Role(role))
	}
	return u, nil
}

func (r *AccountRepository) MarkIdentityUsed(ctx context.Context, identityID string, at time.Time) error {
	_, err := r.db.Pool.Exec(ctx, `UPDATE auth_identities SET last_used_at = $2 WHERE id = $1`, identityID, at)
	return err
}

func (r *AccountRepository) UpdateIdentitySecret(ctx context.Context, identityID, secretHash string) error {
	_, err := r.db.Pool.Exec(ctx, `UPDATE auth_identities SET secret_hash = $2 WHERE id = $1 AND provider = 'password'`,
		identityID, secretHash)
	return err
}

// MarkEmailVerified confirma el correo y audita en la misma transacción. Si ya estaba
// verificado no cambia nada (la fecha original se conserva).
func (r *AccountRepository) MarkEmailVerified(ctx context.Context, userID string, at time.Time, audit domain.AuditEntry) error {
	return pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE users SET email_verified_at = $2, version = version + 1
			WHERE id = $1 AND email_verified_at IS NULL`, userID, at)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrEmailAlreadyVerified
		}
		return insertAudit(ctx, tx, audit)
	})
}

// UpdateName cambia el nombre (sube la versión) y audita en una transacción.
func (r *AccountRepository) UpdateName(ctx context.Context, userID, name string, audit domain.AuditEntry) error {
	return pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET name = $2, version = version + 1 WHERE id = $1`, userID, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, audit)
	})
}

// ChangeRoles agrega y quita roles, sube la versión del usuario y audita en una transacción.
func (r *AccountRepository) ChangeRoles(ctx context.Context, c port.RoleChange) error {
	return pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		for _, role := range c.Add {
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_roles (user_id, role, granted_by, granted_at) VALUES ($1, $2, NULLIF($3, '')::uuid, $4)
				ON CONFLICT DO NOTHING`, c.UserID, role, c.GrantedBy, c.At); err != nil {
				return err
			}
		}
		if len(c.Remove) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1 AND role = ANY($2)`, c.UserID, c.Remove); err != nil {
				return err
			}
		}
		if err := bumpVersion(ctx, tx, c.UserID); err != nil {
			return err
		}
		return insertAudit(ctx, tx, c.Audit)
	})
}

// ChangeStatus cambia el estado y el motivo de suspensión y audita en una transacción.
func (r *AccountRepository) ChangeStatus(ctx context.Context, c port.StatusChange) error {
	return pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE users SET status = $2, suspended_reason = NULLIF($3, ''), version = version + 1 WHERE id = $1`,
			c.UserID, c.Status, c.Reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, c.Audit)
	})
}

// bumpVersion sube la versión del usuario (bloqueo optimista) o devuelve ErrNotFound.
func bumpVersion(ctx context.Context, tx pgx.Tx, userID string) error {
	tag, err := tx.Exec(ctx, `UPDATE users SET version = version + 1 WHERE id = $1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetPassword crea la identidad "password" o reemplaza su hash, opcionalmente verifica el
// correo y audita, todo en una transacción.
func (r *AccountRepository) SetPassword(ctx context.Context, up port.PasswordUpdate) error {
	return pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO auth_identities (id, user_id, provider, provider_subject, secret_hash, created_at)
			VALUES ($1, $2, 'password', $3, $4, $5)
			ON CONFLICT ON CONSTRAINT auth_identities_one_per_provider
			DO UPDATE SET secret_hash = EXCLUDED.secret_hash, provider_subject = EXCLUDED.provider_subject`,
			up.IdentityID, up.UserID, up.Email, up.SecretHash, up.At); err != nil {
			return err
		}
		if up.VerifyEmail {
			if _, err := tx.Exec(ctx, `
				UPDATE users SET email_verified_at = COALESCE(email_verified_at, $2), version = version + 1
				WHERE id = $1`, up.UserID, up.At); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, up.Audit)
	})
}

// Record implementa port.AuditLog fuera de una transacción de negocio.
func (r *AccountRepository) Record(ctx context.Context, entry domain.AuditEntry) error {
	return insertAudit(ctx, r.db.Pool, entry)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func insertAudit(ctx context.Context, db execer, e domain.AuditEntry) error {
	before, err := jsonOrNil(e.Before)
	if err != nil {
		return err
	}
	after, err := jsonOrNil(e.After)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, entity, entity_id, before, after, ip)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7)`,
		e.ActorID, e.Action, e.Entity, e.EntityID, before, after, parseIP(e.IP))
	if err != nil {
		return fmt.Errorf("auditoría: %w", err)
	}
	return nil
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// parseIP devuelve nil si la IP está vacía o es inválida (la columna es inet).
func parseIP(raw string) *netip.Addr {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return nil
	}
	return &addr
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
