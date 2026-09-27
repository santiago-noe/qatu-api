// Package argon2 implementa port.PasswordHasher con argon2id y formato PHC.
package argon2

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params de argon2id. Los valores por defecto siguen la recomendación de OWASP
// (19 MiB, 2 iteraciones, 1 hilo).
type Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultParams = Params{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}

var ErrInvalidHash = errors.New("argon2: hash con formato inválido")

var b64 = base64.RawStdEncoding

type Hasher struct {
	params Params
}

func New(p Params) *Hasher { return &Hasher{params: p} }

// Hash devuelve $argon2id$v=19$m=...,t=...,p=...$sal$hash.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2: sal: %w", err)
	}
	p := h.params
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, p.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify compara en tiempo constante y avisa si el hash usa parámetros distintos a los actuales.
func (h *Hasher) Verify(password, encoded string) (bool, bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, false, err
	}
	other := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(key)))
	if subtle.ConstantTimeCompare(key, other) != 1 {
		return false, false, nil
	}
	needsRehash := p.MemoryKiB != h.params.MemoryKiB || p.Iterations != h.params.Iterations ||
		p.Parallelism != h.params.Parallelism || uint32(len(key)) != h.params.KeyLength
	return true, needsRehash, nil
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrInvalidHash
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	return p, salt, key, nil
}
