// Package smtp implementa port.Mailer con SMTP genérico: Mailpit en local y cualquier
// proveedor SMTP en producción, solo cambiando la configuración (decisión de clarify).
package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"fmt"
	htmltemplate "html/template"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"
)

//go:embed templates/*
var templatesFS embed.FS

// Límite de tiempo para toda la conversación SMTP: un proveedor lento no cuelga la petición.
const sendTimeout = 15 * time.Second

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "Qatu <no-responder@dominio>"
}

type Mailer struct {
	cfg  Config
	from *mail.Address
	html *htmltemplate.Template
	text *texttemplate.Template
}

func New(cfg Config) (*Mailer, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp: remitente inválido %q: %w", cfg.From, err)
	}
	html, err := htmltemplate.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("smtp: plantillas html: %w", err)
	}
	text, err := texttemplate.ParseFS(templatesFS, "templates/*.txt")
	if err != nil {
		return nil, fmt.Errorf("smtp: plantillas de texto: %w", err)
	}
	return &Mailer{cfg: cfg, from: from, html: html, text: text}, nil
}

// SendEmailVerification implementa port.Mailer.
func (m *Mailer) SendEmailVerification(ctx context.Context, to, name, code string, validFor time.Duration) error {
	data := map[string]string{"Name": name, "Code": code, "ValidFor": humanDuration(validFor)}
	return m.send(ctx, to, "Tu código de verificación de Qatu: "+code, "email_verification", data)
}

func (m *Mailer) send(ctx context.Context, to, subject, template string, data any) error {
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("smtp: destinatario inválido: %w", err)
	}
	msg, err := m.build(rcpt, subject, template, data)
	if err != nil {
		return err
	}
	return m.deliver(ctx, rcpt.Address, msg)
}

// build arma un mensaje multipart/alternative con texto plano y HTML.
func (m *Mailer) build(to *mail.Address, subject, template string, data any) ([]byte, error) {
	var textBody, htmlBody bytes.Buffer
	if err := m.text.ExecuteTemplate(&textBody, template+".txt", data); err != nil {
		return nil, fmt.Errorf("smtp: plantilla %s.txt: %w", template, err)
	}
	if err := m.html.ExecuteTemplate(&htmlBody, template+".html", data); err != nil {
		return nil, fmt.Errorf("smtp: plantilla %s.html: %w", template, err)
	}

	var body bytes.Buffer
	parts := multipart.NewWriter(&body)
	for _, p := range []struct {
		contentType string
		content     []byte
	}{
		{"text/plain; charset=UTF-8", textBody.Bytes()},
		{"text/html; charset=UTF-8", htmlBody.Bytes()},
	} {
		w, err := parts.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.contentType},
			"Content-Transfer-Encoding": {"8bit"},
		})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(p.content); err != nil {
			return nil, err
		}
	}
	if err := parts.Close(); err != nil {
		return nil, err
	}

	var msg bytes.Buffer
	headers := [][2]string{
		{"From", m.from.String()},
		{"To", to.String()},
		{"Subject", mime.QEncoding.Encode("UTF-8", subject)},
		{"Date", time.Now().Format(time.RFC1123Z)},
		{"Message-ID", fmt.Sprintf("<%s@%s>", randomID(), domainOf(m.from.Address))},
		{"MIME-Version", "1.0"},
		{"Content-Type", "multipart/alternative; boundary=" + parts.Boundary()},
	}
	for _, h := range headers {
		fmt.Fprintf(&msg, "%s: %s\r\n", h[0], h[1])
	}
	msg.WriteString("\r\n")
	msg.Write(body.Bytes())
	return msg.Bytes(), nil
}

// deliver usa STARTTLS si el servidor lo ofrece y autenticación solo si hay usuario.
// net/smtp rechaza enviar la contraseña sin TLS salvo a localhost.
func (m *Mailer) deliver(ctx context.Context, to string, msg []byte) error {
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	deadline := time.Now().Add(sendTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: conectar a %s: %w", addr, err)
	}
	conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("smtp: autenticación: %w", err)
		}
	}
	if err := client.Mail(m.from.Address); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp: RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp: escribir: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: cerrar mensaje: %w", err)
	}
	return client.Quit()
}

func humanDuration(d time.Duration) string {
	if minutes := int(d.Minutes()); minutes > 0 && d%time.Minute == 0 {
		if minutes == 1 {
			return "1 minuto"
		}
		return strconv.Itoa(minutes) + " minutos"
	}
	return d.String()
}

func randomID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func domainOf(address string) string {
	if i := strings.LastIndex(address, "@"); i >= 0 {
		return address[i+1:]
	}
	return "qatu.local"
}
