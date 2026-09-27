package smtp

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	m, err := New(Config{Host: "localhost", Port: 1025, From: "Qatu <no-responder@qatu.local>"})
	if err != nil {
		t.Fatal(err)
	}
	to, _ := mail.ParseAddress("ana@correo.pe")
	data := map[string]string{"Name": "<script>Ana</script>", "Code": "482913", "ValidFor": humanDuration(15 * time.Minute)}
	raw, err := m.build(to, "Tu código de verificación de Qatu: 482913", "email_verification", data)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("mensaje MIME inválido: %v", err)
	}
	if !strings.HasPrefix(msg.Header.Get("Content-Type"), "multipart/alternative") {
		t.Fatal("debe tener versión de texto y HTML")
	}
	body := string(raw)
	for _, want := range []string{"482913", "15 minutos", "text/plain", "text/html", "Si no creaste una cuenta"} {
		if !strings.Contains(body, want) {
			t.Fatalf("falta %q en el mensaje", want)
		}
	}
	if strings.Contains(body, "<script>Ana</script>") && !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("el nombre debe escaparse en el HTML")
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "Tu código de verificación de Qatu: 482913" {
		t.Fatalf("asunto mal codificado: %q", subject)
	}
}

func TestNewRejectsInvalidSender(t *testing.T) {
	if _, err := New(Config{From: "no es un correo"}); err == nil {
		t.Fatal("un remitente inválido debe fallar al arrancar")
	}
}

// Integración con Mailpit (docker compose). Se ejecuta si existe QATU_TEST_MAILPIT (por ejemplo localhost).
func TestSendThroughMailpit(t *testing.T) {
	host := os.Getenv("QATU_TEST_MAILPIT")
	if host == "" {
		t.Skip("QATU_TEST_MAILPIT no definido: se omite la integración con Mailpit")
	}
	m, err := New(Config{Host: host, Port: 1025, From: "Qatu <no-responder@qatu.local>"})
	if err != nil {
		t.Fatal(err)
	}
	to := fmt.Sprintf("prueba.%d@correo.pe", time.Now().UnixNano())
	if err := m.SendEmailVerification(context.Background(), to, "Ana", "482913", 15*time.Minute); err != nil {
		t.Fatal(err)
	}

	res, err := http.Get(fmt.Sprintf("http://%s:8025/api/v1/search?query=to:%s", host, to))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var found struct {
		Messages []struct {
			Subject string `json:"Subject"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(res.Body).Decode(&found); err != nil {
		t.Fatal(err)
	}
	if len(found.Messages) != 1 || !strings.Contains(found.Messages[0].Subject, "482913") {
		t.Fatalf("Mailpit no recibió el correo: %+v", found)
	}
}
