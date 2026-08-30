package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dmanager/internal/config"
	"dmanager/internal/mailer/mailertest"
)

const (
	testRelayHost  = "127.0.0.1"
	testFromEmail  = "noreply@example.com"
	testOpsAddress = "ops@example.com"
)

func TestRunSMTPTestDisabled(t *testing.T) {
	cfg := config.SMTPConfig{Enabled: false}
	var out bytes.Buffer
	err := runSMTPTest(context.Background(), cfg, testOpsAddress, "", &out)
	if err == nil || !strings.Contains(err.Error(), "smtp is not configured") {
		t.Fatalf("expected not-configured error, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output on failure, got %q", out.String())
	}
}

func TestRunSMTPTestSendsVerification(t *testing.T) {
	relay := mailertest.New(t)

	cfg := config.SMTPConfig{
		Enabled:        true,
		Host:           testRelayHost,
		Port:           relay.Port(),
		FromEmail:      testFromEmail,
		FromName:       "dmanager",
		TLSMode:        config.TLSModeNone,
		TimeoutSeconds: 5,
	}

	var out bytes.Buffer
	if err := runSMTPTest(context.Background(), cfg, testOpsAddress, "", &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "test email sent to " + testOpsAddress) {
		t.Errorf("confirmation missing, got %q", out.String())
	}
	mailFrom, rcptTo, data := relay.LastMessage()
	if mailFrom != "noreply@example.com" {
		t.Errorf("envelope from = %q", mailFrom)
	}
	if len(rcptTo) != 1 || rcptTo[0] != testOpsAddress {
		t.Errorf("RCPT TO = %v, want ops@example.com", rcptTo)
	}
	if !strings.Contains(data, "Subject: dmanager SMTP test") {
		t.Errorf("default subject missing, data:\n%s", data)
	}
	if !strings.Contains(data, "If you received this, the relay path works.") {
		t.Errorf("test body missing, data:\n%s", data)
	}
}

func TestRunSMTPTestSubjectOverride(t *testing.T) {
	relay := mailertest.New(t)

	cfg := config.SMTPConfig{
		Enabled:        true,
		Host:           testRelayHost,
		Port:           relay.Port(),
		FromEmail:      testFromEmail,
		TLSMode:        config.TLSModeNone,
		TimeoutSeconds: 5,
	}

	var out bytes.Buffer
	if err := runSMTPTest(context.Background(), cfg, "ops@example.com", "relay check", &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, data := relay.LastMessage()
	if !strings.Contains(data, "Subject: relay check") {
		t.Errorf("subject override missing, data:\n%s", data)
	}
}

func TestRunSMTPTestRelayRejectionSurfaces(t *testing.T) {
	relay := mailertest.New(t)
	relay.RequireAuth("relay-user", "relay-secret")

	cfg := config.SMTPConfig{
		Enabled:        true,
		Host:           testRelayHost,
		Port:           relay.Port(),
		FromEmail:      testFromEmail,
		TLSMode:        config.TLSModeNone,
		TimeoutSeconds: 5,
	}

	var out bytes.Buffer
	err := runSMTPTest(context.Background(), cfg, testOpsAddress, "", &out)
	if err == nil || !strings.Contains(err.Error(), "test email failed") {
		t.Fatalf("expected relay rejection to surface, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no confirmation output, got %q", out.String())
	}
}
