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
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (id, email, name, adult_declared_at, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			u.ID, u.Email, u.Name, u.AdultDeclaredAt, u.Status, u.CreatedAt); err != nil {
			return err
		}
		id := a.Identity
		if _, err := tx.Exec(ctx, `
			INSERT INTO auth_identities (id, user_id, provider, provider_subject, secret_hash, created_at, last_used_at)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7)`,
			id.ID, id.UserID, id.Provider, id.ProviderSubject, id.SecretHash, id.CreatedAt, id.LastUsedAt); err != nil {
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

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrEmailTaken
	}
	return err
}

func (r *AccountRepository) FindIdentity(ctx context.Context, provider domain.AuthProvider, subject string) (domain.AuthIdentity, error) {
	var id domain.AuthIdentity
	var secret *string
	err := r.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, provider, provider_subject, secret_hash, created_at, last_used_at
		FROM auth_identities WHERE provider = $1 AND provider_subject = $2`,
		provider, subject).Scan(&id.ID, &id.UserID, &id.Provider, &id.ProviderSubject, &secret, &id.CreatedAt, &id.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthIdentity{}, domain.ErrNotFound
	}
	if secret != nil {
		id.SecretHash = *secret
	}
	return id, err
}

func (r *AccountRepository) FindUser(ctx context.Context, userID string) (domain.User, error) {
	var u domain.User
	var email, phone, avatar, reason *string
	var roles []string
	err := r.db.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.email_verified_at, u.phone, u.phone_verified_at, u.name, u.avatar_url,
		       u.adult_declared_at, u.status, u.suspended_reason, u.verification_level,
		       u.created_at, u.updated_at, u.version,
		       COALESCE(array_agg(r.role ORDER BY r.role) FILTER (WHERE r.role IS NOT NULL), '{}')
		FROM users u LEFT JOIN user_roles r ON r.user_id = u.id
		WHERE u.id = $1
		GROUP BY u.id`, userID).Scan(
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
