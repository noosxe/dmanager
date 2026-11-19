package mailer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"

	"dmanager/internal/config"
	"dmanager/internal/mailer/mailertest"
)

const (
	testRecipient     = "a@b.c"
	testRecipientHTML = "alice@example.com"
)

func testSMTPConfig(host, port string) config.SMTPConfig {
	return config.SMTPConfig{
		Enabled:        true,
		Host:           host,
		Port:           port,
		FromEmail:      "noreply@example.com",
		FromName:       "dmanager",
		TLSMode:        config.TLSModeNone,
		TimeoutSeconds: 5,
	}
}

func TestNewDisabledIsNoop(t *testing.T) {
	cfg := testSMTPConfig("localhost", "25")
	cfg.Enabled = false
	m := New(cfg, slog.Default())
	if m.Enabled() {
		t.Fatal("expected disabled mailer to report not enabled")
	}
	err := m.Send(context.Background(), Message{To: []string{testRecipient}, Subject: "x", TextBody: "y"})
	if err == nil || !strings.Contains(err.Error(), ErrNotConfigured.Error()) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestSendDeliversEnvelopeAndHeaders(t *testing.T) {
	relay := mailertest.New(t)
	m := New(testSMTPConfig("127.0.0.1", relay.Port()), slog.Default())

	err := m.Send(context.Background(), Message{
		To:       []string{testRecipientHTML, "bob@example.com"},
		Subject:  "dmanager test",
		TextBody: "plain body line",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mailFrom, rcptTo, data := relay.LastMessage()
	if mailFrom != "noreply@example.com" {
		t.Errorf("envelope MAIL FROM = %q, want noreply@example.com", mailFrom)
	}
	if len(rcptTo) != 2 || rcptTo[0] != testRecipientHTML || rcptTo[1] != "bob@example.com" {
		t.Errorf("RCPT TO = %v, want both recipients", rcptTo)
	}
	if !strings.Contains(data, `From: "dmanager" <noreply@example.com>`) {
		t.Errorf("From header missing display name, data:\n%s", data)
	}
	if !strings.Contains(data, "Subject: dmanager test") {
		t.Errorf("Subject header missing, data:\n%s", data)
	}
	if !strings.Contains(data, "plain body line") {
		t.Errorf("text body missing, data:\n%s", data)
	}
}

func TestSendIncludesHTMLAlternative(t *testing.T) {
	relay := mailertest.New(t)
	m := New(testSMTPConfig("127.0.0.1", relay.Port()), slog.Default())

	err := m.Send(context.Background(), Message{
		To:       []string{testRecipientHTML},
		Subject:  "rich",
		TextBody: "text part",
		HTMLBody: "<p>html part</p>",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, data := relay.LastMessage()
	if !strings.Contains(data, "text part") || !strings.Contains(data, "html part") {
		t.Errorf("expected both body parts, data:\n%s", data)
	}
	if !strings.Contains(strings.ToUpper(data), "MULTIPART/ALTERNATIVE") {
		t.Errorf("expected multipart/alternative, data:\n%s", data)
	}
}

func TestSendValidatesMessage(t *testing.T) {
	relay := mailertest.New(t)
	m := New(testSMTPConfig("127.0.0.1", relay.Port()), slog.Default())

	if err := m.Send(context.Background(), Message{Subject: "no to", TextBody: "x"}); err == nil {
		t.Error("expected error for missing recipients")
	}
	if err := m.Send(context.Background(), Message{To: []string{testRecipient}, Subject: "no body"}); err == nil {
		t.Error("expected error for empty body")
	}

	mailFrom, _, _ := relay.LastMessage()
	if mailFrom != "" {
		t.Error("validation failures must not contact the relay")
	}
}

func TestSendAuthPlain(t *testing.T) {
	relay := mailertest.New(t)
	relay.RequireAuth("relay-user", "relay-secret")

	cfg := testSMTPConfig("127.0.0.1", relay.Port())
	m := New(cfg, slog.Default())
	if err := m.Send(context.Background(), Message{To: []string{testRecipient}, Subject: "no creds", TextBody: "x"}); err == nil {
		t.Fatal("expected auth failure without credentials")
	}

	cfg.Username = "relay-user"
	cfg.Password = "relay-secret"
	m = New(cfg, slog.Default())
	if err := m.Send(context.Background(), Message{To: []string{testRecipient}, Subject: "with creds", TextBody: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !relay.AuthSucceeded() {
		t.Error("expected server to see a successful AUTH PLAIN")
	}
}

func TestSendContextCanceled(t *testing.T) {
	relay := mailertest.New(t)
	m := New(testSMTPConfig("127.0.0.1", relay.Port()), slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := m.Send(ctx, Message{To: []string{testRecipient}, Subject: "late", TextBody: "x"})
	if err == nil {
		t.Fatal("expected canceled context to fail the send")
	}
}

func TestPortOrDefault(t *testing.T) {
	if got := portOrDefault(""); got != 25 {
		t.Errorf("empty port = %d, want 25", got)
	}
	if got := portOrDefault("587"); got != 587 {
		t.Errorf("587 = %d", got)
	}
	if got := portOrDefault("bogus"); got != 25 {
		t.Errorf("bogus port = %d, want 25", got)
	}
}

// TestSendStartTLSIsMandatory pins that starttls never degrades to plaintext:
// a relay that does not offer STARTTLS must make the send fail before AUTH or
// any message content is sent.
func TestSendStartTLSIsMandatory(t *testing.T) {
	relay := mailertest.New(t)
	relay.RequireAuth("relay-user", "relay-secret")

	cfg := testSMTPConfig("127.0.0.1", relay.Port())
	cfg.TLSMode = config.TLSModeStartTLS
	cfg.Username = "relay-user"
	cfg.Password = "relay-secret"
	m := New(cfg, slog.Default())

	err := m.Send(context.Background(), Message{To: []string{testRecipient}, Subject: "x", TextBody: "secret body"})
	if err == nil {
		t.Fatal("expected an error when the relay does not offer STARTTLS")
	}
	if !strings.Contains(err.Error(), "STARTTLS") || !strings.Contains(err.Error(), "tls_mode") {
		t.Errorf("error should explain the STARTTLS requirement, got %q", err)
	}
	if relay.AuthSucceeded() {
		t.Error("credentials must not be sent without TLS")
	}
	if mailFrom, _, data := relay.LastMessage(); mailFrom != "" || data != "" {
		t.Error("no message may be sent in plaintext")
	}
}

func TestIsTLSPhaseError(t *testing.T) {
	tlsErrs := []error{
		fmt.Errorf("wrapped: %w", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}),
		fmt.Errorf("wrapped: %w", x509.HostnameError{}),
		fmt.Errorf("wrapped: %w", tls.RecordHeaderError{}),
		errors.New(`STARTTLS mode set to: "TLSMandatory", but target host does not support STARTTLS`),
	}
	for _, err := range tlsErrs {
		if !isTLSPhaseError(err) {
			t.Errorf("expected TLS-phase error: %v", err)
		}
	}
	for _, err := range []error{
		errors.New("550 mailbox unavailable"),
		errors.New("535 auth failed"),
		errors.New("dial tcp: connection refused"),
	} {
		if isTLSPhaseError(err) {
			t.Errorf("non-TLS failure classified as TLS: %v", err)
		}
	}
}

// A failure before the TLS phase in starttls mode must not carry the STARTTLS hint.
func TestSendStartTLSNonTLSFailureHasNoHint(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close() // nothing listens: dial fails

	cfg := testSMTPConfig("127.0.0.1", port)
	cfg.TLSMode = config.TLSModeStartTLS
	err = New(cfg, slog.Default()).Send(context.Background(), Message{To: []string{testRecipient}, Subject: "x", TextBody: "y"})
	if err == nil {
		t.Fatal("expected dial error")
	}
	if strings.Contains(err.Error(), "STARTTLS") || strings.Contains(err.Error(), "tls_mode") {
		t.Errorf("dial failure must not carry the STARTTLS hint, got %q", err)
	}
}
