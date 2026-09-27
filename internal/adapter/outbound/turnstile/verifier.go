// Package turnstile implementa port.HumanVerifier con Cloudflare Turnstile.
package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

const (
	siteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	// maxTokenLength: Cloudflare no emite tokens más largos; se rechazan sin llamar a la red.
	maxTokenLength = 2048
	timeout        = 5 * time.Second
)

// Verifier consulta siteverify. Cada token sirve una sola vez y vence a los 5 minutos.
type Verifier struct {
	secret string
	url    string
	client *http.Client
}

func New(secret string) *Verifier {
	return &Verifier{secret: secret, url: siteverifyURL, client: &http.Client{Timeout: timeout}}
}

type siteverifyResponse struct {
	Success    bool     `json:"success"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
	Metadata   struct {
		// Con las claves de prueba de Cloudflare no llega la acción: no se puede comparar.
		TestingKey bool `json:"result_with_testing_key"`
	} `json:"metadata"`
}

func (r siteverifyResponse) actionMatches(action string) bool {
	return action == "" || r.Metadata.TestingKey || r.Action == action
}

func (v *Verifier) Verify(ctx context.Context, token, action, ip string) error {
	if token == "" || len(token) > maxTokenLength {
		return domain.ErrHumanCheckFailed
	}
	form := url.Values{"secret": {v.secret}, "response": {token}}
	if ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("turnstile: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrHumanCheckUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: siteverify respondió %d", domain.ErrHumanCheckUnavailable, res.StatusCode)
	}
	var body siteverifyResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: respuesta ilegible: %v", domain.ErrHumanCheckUnavailable, err)
	}

	switch {
	case slices.Contains(body.ErrorCodes, "internal-error"):
		return fmt.Errorf("%w: error interno de Cloudflare", domain.ErrHumanCheckUnavailable)
	case !body.Success, !body.actionMatches(action):
		return domain.ErrHumanCheckFailed
	}
	return nil
}

// Disabled acepta cualquier petición. Solo para desarrollo sin secreto: config.Load
// exige APP__TURNSTILE__SECRET en producción.
type Disabled struct{}

func (Disabled) Verify(context.Context, string, string, string) error { return nil }
