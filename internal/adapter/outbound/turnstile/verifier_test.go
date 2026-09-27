package turnstile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// fakeSiteverify imita a Cloudflare: el token "ok-<action>" es válido para esa acción.
func fakeSiteverify(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if err := r.ParseForm(); err != nil || r.PostForm.Get("secret") != "secreto" {
			t.Errorf("debe enviar el secreto por formulario: %v", r.PostForm)
		}
		if r.PostForm.Get("remoteip") != "1.2.3.4" {
			t.Errorf("debe enviar la IP del usuario, llegó %q", r.PostForm.Get("remoteip"))
		}
		switch token := r.PostForm.Get("response"); {
		case token == "caido":
			w.WriteHeader(http.StatusBadGateway)
		case token == "interno":
			_, _ = w.Write([]byte(`{"success":false,"error-codes":["internal-error"]}`))
		case token == "prueba":
			_, _ = w.Write([]byte(`{"success":true,"metadata":{"result_with_testing_key":true}}`))
		case strings.HasPrefix(token, "ok-"):
			_, _ = w.Write([]byte(`{"success":true,"action":"` + strings.TrimPrefix(token, "ok-") + `"}`))
		default:
			_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestVerify(t *testing.T) {
	calls := 0
	v := New("secreto")
	v.url = fakeSiteverify(t, &calls).URL
	ctx := context.Background()

	tests := []struct {
		name, token string
		want        error
	}{
		{"token válido para la acción", "ok-register", nil},
		{"token de otro formulario", "ok-password_forgot", domain.ErrHumanCheckFailed},
		{"token rechazado", "falso", domain.ErrHumanCheckFailed},
		{"clave de prueba de Cloudflare (sin acción)", "prueba", nil},
		{"Cloudflare responde error", "caido", domain.ErrHumanCheckUnavailable},
		{"error interno de Cloudflare", "interno", domain.ErrHumanCheckUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Verify(ctx, tt.token, "register", "1.2.3.4"); !errors.Is(err, tt.want) {
				t.Fatalf("Verify = %v, se esperaba %v", err, tt.want)
			}
		})
	}

	before := calls
	for _, token := range []string{"", strings.Repeat("x", maxTokenLength+1)} {
		if err := v.Verify(ctx, token, "register", "1.2.3.4"); !errors.Is(err, domain.ErrHumanCheckFailed) {
			t.Fatalf("un token vacío o enorme se rechaza, llegó %v", err)
		}
	}
	if calls != before {
		t.Fatal("un token vacío o enorme no debe llegar a Cloudflare")
	}
}

func TestVerifyUnreachable(t *testing.T) {
	v := New("secreto")
	v.url = "http://127.0.0.1:1"
	if err := v.Verify(context.Background(), "ok-register", "register", ""); !errors.Is(err, domain.ErrHumanCheckUnavailable) {
		t.Fatalf("sin conexión, el servicio no está disponible: %v", err)
	}
}
