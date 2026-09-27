package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "  Ana.Perez@Correo.PE ", want: "ana.perez@correo.pe"},
		{in: "tecnico+qatu@gmail.com", want: "tecnico+qatu@gmail.com"},
		{in: "sin-arroba", wantErr: true},
		{in: "ana@localhost", wantErr: true},
		{in: "Ana <ana@correo.pe>", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeEmail(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidEmail) {
					t.Fatalf("se esperaba ErrInvalidEmail, llegó %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NormalizeEmail(%q) = %q, %v; se esperaba %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	if got, err := NormalizeName("  Ana   María  "); err != nil || got != "Ana María" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", NameMaxLength+1)} {
		if _, err := NormalizeName(bad); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("NormalizeName(%q) debería fallar", bad)
		}
	}
}

func TestValidatePasswordShape(t *testing.T) {
	tests := []struct {
		name     string
		password string
		email    string
		want     error
	}{
		{name: "válida", password: "tornillo-verde-9", email: "ana@correo.pe"},
		{name: "cuenta caracteres, no bytes", password: "ñandúñandú", email: ""},
		{name: "demasiado corta", password: "corta123", want: ErrPasswordTooShort},
		{name: "demasiado larga", password: strings.Repeat("x", PasswordMaxLength+1), want: ErrPasswordTooLong},
		{name: "igual al correo", password: "ANA@correo.pe", email: "ana@correo.pe", want: ErrPasswordIsEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidatePasswordShape(tt.password, tt.email); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, se esperaba %v", err, tt.want)
			}
		})
	}
}

func TestUserRules(t *testing.T) {
	now := time.Now()
	verified := User{Status: UserActive, EmailVerifiedAt: &now, Roles: []Role{RoleClient}}

	if !verified.CanTransact() {
		t.Fatal("una cuenta activa y verificada puede transaccionar")
	}
	if (User{Status: UserActive}).CanTransact() {
		t.Fatal("sin correo verificado no se transacciona")
	}
	suspended := verified
	suspended.Status = UserSuspended
	if suspended.CanTransact() || suspended.CanSignIn() != nil {
		t.Fatal("suspendida: no transacciona, pero sí entra a ver su historial")
	}
	if (User{Status: UserDeleted}).CanSignIn() == nil {
		t.Fatal("una cuenta eliminada no entra")
	}
	if verified.RequiresTwoFactor() {
		t.Fatal("un cliente no está obligado a dos pasos")
	}
	admin := verified
	admin.Roles = []Role{RoleClient, RoleAdmin}
	if !admin.RequiresTwoFactor() {
		t.Fatal("los roles internos requieren dos pasos")
	}
}
