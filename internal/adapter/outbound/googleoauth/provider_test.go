package googleoauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

const clientID = "qatu-test.apps.googleusercontent.com"

// fakeGoogle imita el endpoint de tokens: acepta el código "codigo-ok" solo si el
// code_verifier corresponde al reto PKCE enviado en AuthURL, y devuelve un id_token firmado.
type fakeGoogle struct {
	t         *testing.T
	key       *rsa.PrivateKey
	challenge string
	claims    map[string]any
}

func (f *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if r.PostForm.Get("code") != "codigo-ok" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": f.sign(f.claims),
	})
}

func (f *fakeGoogle) sign(claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		f.t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	obj, err := signer.Sign(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	raw, _ := obj.CompactSerialize()
	return raw
}

func validClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": issuer, "aud": clientID, "sub": "1234567890", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"email": "ana@gmail.com", "email_verified": true, "name": "Ana Quispe", "picture": "https://lh3.googleusercontent.com/a",
	}
}

func newTestProvider(t *testing.T, claims map[string]any) (*Provider, *fakeGoogle) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeGoogle{t: t, key: key, claims: claims}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	keys := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{key.Public()}}
	ep := oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}
	cfg := Config{ClientID: clientID, ClientSecret: "secreto", RedirectURL: "http://localhost:3000/api/auth/google/callback"}
	return newProvider(cfg, ep, oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: clientID})), fake
}

// start simula la ida a Google: guarda el reto PKCE que viaja en la URL.
func start(t *testing.T, p *Provider, fake *fakeGoogle, verifier string) url.Values {
	t.Helper()
	raw, err := p.AuthURL("state-1", verifier)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	fake.challenge = q.Get("code_challenge")
	return q
}

func TestAuthURL(t *testing.T) {
	p, fake := newTestProvider(t, validClaims())
	q := start(t, p, fake, "verificador-de-prueba-suficientemente-largo-43")
	for key, want := range map[string]string{
		"client_id": clientID, "state": "state-1", "response_type": "code", "code_challenge_method": "S256",
		"scope": "openid email profile", "redirect_uri": "http://localhost:3000/api/auth/google/callback",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, se esperaba %q", key, got, want)
		}
	}
	if q.Get("code_challenge") == "" || q.Get("code_verifier") != "" {
		t.Fatal("viaja el reto PKCE, nunca el verificador")
	}
}

func TestExchange(t *testing.T) {
	const verifier = "verificador-de-prueba-suficientemente-largo-43"
	ctx := context.Background()

	t.Run("perfil válido", func(t *testing.T) {
		p, fake := newTestProvider(t, validClaims())
		start(t, p, fake, verifier)
		profile, err := p.Exchange(ctx, "codigo-ok", verifier)
		if err != nil {
			t.Fatal(err)
		}
		if profile.Subject != "1234567890" || profile.Email != "ana@gmail.com" || !profile.EmailVerified ||
			profile.Name != "Ana Quispe" || profile.AvatarURL == "" {
			t.Fatalf("perfil = %+v", profile)
		}
	})

	t.Run("verificador PKCE distinto", func(t *testing.T) {
		p, fake := newTestProvider(t, validClaims())
		start(t, p, fake, verifier)
		if _, err := p.Exchange(ctx, "codigo-ok", "otro-verificador-que-no-corresponde-al-reto"); !errors.Is(err, domain.ErrOAuthFailed) {
			t.Fatalf("sin el verificador correcto no hay canje, llegó %v", err)
		}
	})

	rejected := map[string]func(map[string]any){
		"otra audiencia": func(c map[string]any) { c["aud"] = "otra-app.apps.googleusercontent.com" },
		"otro emisor":    func(c map[string]any) { c["iss"] = "https://evil.example" },
		"token vencido":  func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
	}
	for name, tamper := range rejected {
		t.Run(name, func(t *testing.T) {
			claims := validClaims()
			tamper(claims)
			p, fake := newTestProvider(t, claims)
			start(t, p, fake, verifier)
			if _, err := p.Exchange(ctx, "codigo-ok", verifier); !errors.Is(err, domain.ErrOAuthFailed) {
				t.Fatalf("el id_token debe rechazarse, llegó %v", err)
			}
		})
	}

	t.Run("firma de otra clave", func(t *testing.T) {
		p, fake := newTestProvider(t, validClaims())
		start(t, p, fake, verifier)
		fake.key, _ = rsa.GenerateKey(rand.Reader, 2048) // firma con una clave que Google no publicó
		if _, err := p.Exchange(ctx, "codigo-ok", verifier); !errors.Is(err, domain.ErrOAuthFailed) {
			t.Fatalf("una firma desconocida se rechaza, llegó %v", err)
		}
	})
}

func TestDisabled(t *testing.T) {
	if _, err := (Disabled{}).AuthURL("s", "v"); !errors.Is(err, domain.ErrOAuthUnavailable) {
		t.Fatalf("sin configuración no hay Google, llegó %v", err)
	}
}
