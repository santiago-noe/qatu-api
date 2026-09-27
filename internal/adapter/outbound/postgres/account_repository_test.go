package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

func newAccount(email string) port.NewAccount {
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.NewString()
	return port.NewAccount{
		User: domain.User{ID: userID, Email: email, Name: "Ana Pérez", AdultDeclaredAt: &now,
			Status: domain.UserActive, Roles: []domain.Role{domain.RoleClient}, CreatedAt: now},
		Identity: domain.AuthIdentity{ID: uuid.NewString(), UserID: userID, Provider: domain.ProviderPassword,
			ProviderSubject: email, SecretHash: "$argon2id$hash", CreatedAt: now},
		Consents: []domain.Consent{
			{Purpose: domain.ConsentTerms, Version: "2026-09", GrantedAt: now},
			{Purpose: domain.ConsentPrivacy, Version: "2026-09", GrantedAt: now},
		},
		Audit: domain.AuditEntry{ActorID: userID, Action: domain.AuditUserRegistered, Entity: "user", EntityID: userID, IP: "190.12.3.4"},
	}
}

func TestAccountRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewAccountRepository(db)
	ctx := context.Background()

	acc := newAccount("ana@correo.pe")
	if err := repo.CreateAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}

	t.Run("correo repetido", func(t *testing.T) {
		if err := repo.CreateAccount(ctx, newAccount("ANA@correo.pe")); !errors.Is(err, domain.ErrEmailTaken) {
			t.Fatalf("se esperaba ErrEmailTaken, llegó %v", err)
		}
	})

	t.Run("buscar identidad y usuario con roles", func(t *testing.T) {
		identity, err := repo.FindIdentity(ctx, domain.ProviderPassword, "ana@correo.pe")
		if err != nil || identity.UserID != acc.User.ID || identity.SecretHash != "$argon2id$hash" {
			t.Fatalf("FindIdentity = %+v, %v", identity, err)
		}
		user, err := repo.FindUser(ctx, identity.UserID)
		if err != nil || user.Email != "ana@correo.pe" || !user.HasRole(domain.RoleClient) || user.IsVerified() {
			t.Fatalf("FindUser = %+v, %v", user, err)
		}
		if _, err := repo.FindIdentity(ctx, domain.ProviderGoogle, "no-existe"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("se esperaba ErrNotFound, llegó %v", err)
		}
		if _, err := repo.FindUser(ctx, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("se esperaba ErrNotFound, llegó %v", err)
		}
	})

	t.Run("actualizar secreto y último uso", func(t *testing.T) {
		if err := repo.UpdateIdentitySecret(ctx, acc.Identity.ID, "$argon2id$nuevo"); err != nil {
			t.Fatal(err)
		}
		if err := repo.MarkIdentityUsed(ctx, acc.Identity.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		identity, _ := repo.FindIdentity(ctx, domain.ProviderPassword, "ana@correo.pe")
		if identity.SecretHash != "$argon2id$nuevo" || identity.LastUsedAt == nil {
			t.Fatalf("no se actualizó: %+v", identity)
		}
	})

	t.Run("consentimientos y auditoría en la misma transacción", func(t *testing.T) {
		var consents, audits int
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM consents WHERE user_id = $1`, acc.User.ID).Scan(&consents)
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id = $1 AND action = $2`,
			acc.User.ID, domain.AuditUserRegistered).Scan(&audits)
		if consents != 2 || audits != 1 {
			t.Fatalf("consents=%d audits=%d", consents, audits)
		}
	})

	t.Run("un registro fallido no deja datos a medias", func(t *testing.T) {
		bad := newAccount("beto@correo.pe")
		bad.Identity.ProviderSubject = "ana@correo.pe" // choca con la identidad existente
		if err := repo.CreateAccount(ctx, bad); !errors.Is(err, domain.ErrEmailTaken) {
			t.Fatalf("se esperaba ErrEmailTaken, llegó %v", err)
		}
		if _, err := repo.FindUser(ctx, bad.User.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("la transacción debe revertir también el usuario")
		}
	})

	t.Run("verificar correo una sola vez", func(t *testing.T) {
		audit := domain.AuditEntry{ActorID: acc.User.ID, Action: domain.AuditEmailVerified, Entity: "user", EntityID: acc.User.ID}
		if err := repo.MarkEmailVerified(ctx, acc.User.ID, time.Now(), audit); err != nil {
			t.Fatal(err)
		}
		user, _ := repo.FindUser(ctx, acc.User.ID)
		if !user.IsVerified() || user.Version != 2 {
			t.Fatalf("debe quedar verificado y con versión nueva: %+v", user)
		}
		if err := repo.MarkEmailVerified(ctx, acc.User.ID, time.Now(), audit); !errors.Is(err, domain.ErrEmailAlreadyVerified) {
			t.Fatalf("la segunda vez debe fallar, llegó %v", err)
		}
		var audits int
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id = $1 AND action = $2`,
			acc.User.ID, domain.AuditEmailVerified).Scan(&audits)
		if audits != 1 {
			t.Fatalf("una sola auditoría de verificación, hay %d", audits)
		}
	})

	t.Run("buscar por correo", func(t *testing.T) {
		user, err := repo.FindUserByEmail(ctx, "ana@correo.pe")
		if err != nil || user.ID != acc.User.ID || !user.HasRole(domain.RoleClient) {
			t.Fatalf("FindUserByEmail = %+v, %v", user, err)
		}
		if _, err := repo.FindUserByEmail(ctx, "nadie@correo.pe"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("se esperaba ErrNotFound, llegó %v", err)
		}
	})

	t.Run("cambiar contraseña reemplaza la identidad existente", func(t *testing.T) {
		up := port.PasswordUpdate{UserID: acc.User.ID, IdentityID: uuid.NewString(), Email: "ana@correo.pe",
			SecretHash: "$argon2id$reset", At: time.Now(),
			Audit: domain.AuditEntry{ActorID: acc.User.ID, Action: domain.AuditPasswordReset, Entity: "user", EntityID: acc.User.ID}}
		if err := repo.SetPassword(ctx, up); err != nil {
			t.Fatal(err)
		}
		identity, _ := repo.FindIdentity(ctx, domain.ProviderPassword, "ana@correo.pe")
		if identity.SecretHash != "$argon2id$reset" || identity.ID != acc.Identity.ID {
			t.Fatalf("debe actualizar la misma identidad: %+v", identity)
		}
	})

	t.Run("una cuenta sin contraseña la obtiene y verifica su correo", func(t *testing.T) {
		google := newAccount("beto@correo.pe")
		google.Identity.Provider, google.Identity.ProviderSubject, google.Identity.SecretHash = domain.ProviderGoogle, "sub-beto", ""
		if err := repo.CreateAccount(ctx, google); err != nil {
			t.Fatal(err)
		}
		up := port.PasswordUpdate{UserID: google.User.ID, IdentityID: uuid.NewString(), Email: "beto@correo.pe",
			SecretHash: "$argon2id$nuevo", At: time.Now(), VerifyEmail: true,
			Audit: domain.AuditEntry{ActorID: google.User.ID, Action: domain.AuditPasswordReset, Entity: "user", EntityID: google.User.ID}}
		if err := repo.SetPassword(ctx, up); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.FindIdentity(ctx, domain.ProviderPassword, "beto@correo.pe"); err != nil {
			t.Fatal("debe crearse la identidad password")
		}
		if user, _ := repo.FindUser(ctx, google.User.ID); !user.IsVerified() {
			t.Fatal("el correo queda verificado")
		}
	})

	t.Run("identidad por usuario y cambio de nombre", func(t *testing.T) {
		identity, err := repo.FindUserIdentity(ctx, acc.User.ID, domain.ProviderPassword)
		if err != nil || identity.ID != acc.Identity.ID {
			t.Fatalf("FindUserIdentity = %+v, %v", identity, err)
		}
		if _, err := repo.FindUserIdentity(ctx, acc.User.ID, domain.ProviderGoogle); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("sin Google debe ser ErrNotFound, llegó %v", err)
		}
		before, _ := repo.FindUser(ctx, acc.User.ID)
		audit := domain.AuditEntry{ActorID: acc.User.ID, Action: domain.AuditProfileUpdated, Entity: "user", EntityID: acc.User.ID}
		if err := repo.UpdateName(ctx, acc.User.ID, "Ana María", audit); err != nil {
			t.Fatal(err)
		}
		after, _ := repo.FindUser(ctx, acc.User.ID)
		if after.Name != "Ana María" || after.Version != before.Version+1 || !after.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("debe cambiar el nombre, la versión y updated_at: %+v", after)
		}
		if err := repo.UpdateName(ctx, uuid.NewString(), "X", audit); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("un usuario inexistente es ErrNotFound, llegó %v", err)
		}
	})

	t.Run("auditoría suelta", func(t *testing.T) {
		if err := repo.Record(ctx, domain.AuditEntry{ActorID: acc.User.ID, Action: domain.AuditUserLogin,
			Entity: "user", EntityID: acc.User.ID, After: map[string]any{"provider": "password"}}); err != nil {
			t.Fatal(err)
		}
	})
}
