package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

type CodesConfig struct {
	TTL            time.Duration
	MaxAttempts    int
	ResendCooldown time.Duration
}

// OneTimeCodes emite y valida códigos de 6 dígitos para cualquier finalidad
// (verificación de correo, recuperación de contraseña, dos pasos).
type OneTimeCodes struct {
	store  port.CodeStore
	secret []byte
	cfg    CodesConfig
}

func NewOneTimeCodes(store port.CodeStore, secret string, cfg CodesConfig) *OneTimeCodes {
	return &OneTimeCodes{store: store, secret: []byte(secret), cfg: cfg}
}

// Issue genera un código nuevo (invalida el anterior) respetando la espera entre envíos.
func (c *OneTimeCodes) Issue(ctx context.Context, purpose domain.CodePurpose, subject string) (string, error) {
	ok, err := c.store.AcquireCooldown(ctx, purpose, subject, c.cfg.ResendCooldown)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", domain.ErrTooManyRequests
	}
	code, err := randomDigits(domain.CodeLength)
	if err != nil {
		return "", err
	}
	if err := c.store.Save(ctx, purpose, subject, c.hash(purpose, subject, code), c.cfg.TTL, c.cfg.MaxAttempts); err != nil {
		return "", err
	}
	return code, nil
}

// Verify consume el código si es correcto.
func (c *OneTimeCodes) Verify(ctx context.Context, purpose domain.CodePurpose, subject, code string) error {
	if !isDigits(code, domain.CodeLength) {
		// Formato inválido: no gasta un intento, pero tampoco puede ser correcto.
		return domain.ErrCodeInvalid
	}
	return c.store.Consume(ctx, purpose, subject, c.hash(purpose, subject, code))
}

func (c *OneTimeCodes) ValidFor() time.Duration { return c.cfg.TTL }

// hash firma el código con HMAC-SHA256 y la clave del servidor: con un volcado de Redis
// no se pueden probar el millón de combinaciones sin conocer la clave.
func (c *OneTimeCodes) hash(purpose domain.CodePurpose, subject, code string) string {
	mac := hmac.New(sha256.New, c.secret)
	fmt.Fprintf(mac, "%s|%s|%s", purpose, subject, code)
	return hex.EncodeToString(mac.Sum(nil))
}

// randomDigits genera dígitos uniformes con crypto/rand (sin sesgo de módulo).
func randomDigits(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", fmt.Errorf("código: %w", err)
		}
		b[i] = byte('0' + d.Int64())
	}
	return string(b), nil
}

func isDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
