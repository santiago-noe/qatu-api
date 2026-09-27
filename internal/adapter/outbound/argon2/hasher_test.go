package argon2

import (
	"errors"
	"strings"
	"testing"
)

// Parámetros bajos para que las pruebas sean rápidas.
var testParams = Params{MemoryKiB: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestHashAndVerify(t *testing.T) {
	h := New(testParams)
	encoded, err := h.Hash("tornillo-verde-9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("formato inesperado: %s", encoded)
	}

	if ok, rehash, err := h.Verify("tornillo-verde-9", encoded); err != nil || !ok || rehash {
		t.Fatalf("la contraseña correcta debe coincidir: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, err := h.Verify("tornillo-verde-8", encoded); err != nil || ok {
		t.Fatal("una contraseña distinta no debe coincidir")
	}
}

func TestSaltIsRandom(t *testing.T) {
	h := New(testParams)
	a, _ := h.Hash("misma-contrasena")
	b, _ := h.Hash("misma-contrasena")
	if a == b {
		t.Fatal("dos hashes de la misma contraseña deben diferir (sal aleatoria)")
	}
}

func TestNeedsRehashWhenParamsChange(t *testing.T) {
	old, _ := New(testParams).Hash("tornillo-verde-9")
	stronger := testParams
	stronger.Iterations = 2

	ok, rehash, err := New(stronger).Verify("tornillo-verde-9", old)
	if err != nil || !ok || !rehash {
		t.Fatalf("un hash con parámetros viejos coincide pero pide recalcular: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	h := New(testParams)
	for _, bad := range []string{"", "texto", "$bcrypt$x$y$z$w", "$argon2id$v=19$m=x$salt$hash", "$argon2id$v=18$m=1,t=1,p=1$c2FsdA$aGFzaA"} {
		if _, _, err := h.Verify("x", bad); !errors.Is(err, ErrInvalidHash) {
			t.Fatalf("Verify(%q) debería devolver ErrInvalidHash, llegó %v", bad, err)
		}
	}
}
