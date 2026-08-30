// Package mailer provides system-only outbound email for dmanager. Mail is
// delivered through an administrator-configured SMTP relay; there is no
// user-facing or RPC-reachable send path. When SMTP is not configured, New
// returns a no-op mailer so consumers degrade to logged drops instead of
// branching on configuration.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"dmanager/internal/config"

	"github.com/wneessen/go-mail"
)

// ErrNotConfigured is returned by a no-op mailer's Send when SMTP is disabled.
// Consumers that must surface the difference (e.g. the ops test command) can
// check for it; operational flows treat a drop like any other failure.
var ErrNotConfigured = errors.New("smtp is not configured")

// Message is one outbound email. TextBody is required; HTMLBody is an
// optional alternative part rendered by MIME-aware clients.
type Message struct {
	To       []string
	Subject  string
	TextBody string
	HTMLBody string
}

// Mailer sends system emails. Implementations must be safe for concurrent
// use and must never panic or block beyond the configured timeout.
type Mailer interface {
	// Send performs a single synchronous delivery attempt bounded by the
	// configured timeout and the given context. It returns an error on
	// failure; whether that failure is best-effort or surfaced is decided
	// by the calling flow, not by the mailer.
	Send(ctx context.Context, msg Message) error
	// Enabled reports whether SMTP is configured, so flows can skip
	// expensive work (rendering, token creation) before calling Send.
	Enabled() bool
}

// New builds the mailer for the given SMTP config: a real relay client when
// enabled, a no-op otherwise.
func New(cfg config.SMTPConfig, logger *slog.Logger) Mailer {
	if !cfg.Enabled {
		return &NoopMailer{logger: logger}
	}
	return &smtpMailer{cfg: cfg, logger: logger}
}

// NoopMailer is used when SMTP is disabled: Send logs the dropped message
// at debug and returns ErrNotConfigured.
type NoopMailer struct {
	logger *slog.Logger
}

func (n *NoopMailer) Enabled() bool { return false }

func (n *NoopMailer) Send(_ context.Context, msg Message) error {
	n.logger.Debug("smtp not configured, dropping email",
		slog.String("to", strings.Join(msg.To, ",")),
		slog.String("subject", msg.Subject))
	return ErrNotConfigured
}

type smtpMailer struct {
	cfg    config.SMTPConfig
	logger *slog.Logger
}

func (m *smtpMailer) Enabled() bool { return true }

func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	if len(msg.To) == 0 {
		return fmt.Errorf("email %q has no recipients", msg.Subject)
	}
	if strings.TrimSpace(msg.TextBody) == "" && strings.TrimSpace(msg.HTMLBody) == "" {
		return fmt.Errorf("email %q has no body", msg.Subject)
	}

	timeout := time.Duration(m.cfg.TimeoutSeconds) * time.Second
	if dl, ok := ctx.Deadline(); !ok || time.Until(dl) > timeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// All header values (From, To, Subject) go through the library API,
	// which validates addresses and encodes headers — never concatenate
	// them by hand.
	mi := mail.NewMsg()
	if err := mi.FromFormat(m.cfg.FromName, m.cfg.FromEmail); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	if err := mi.To(msg.To...); err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	mi.Subject(msg.Subject)
	mi.SetBodyString(mail.TypeTextPlain, msg.TextBody)
	if strings.TrimSpace(msg.HTMLBody) != "" {
		mi.AddAlternativeString(mail.TypeTextHTML, msg.HTMLBody)
	}

	opts := []mail.Option{
		mail.WithTimeout(timeout),
		mail.WithPort(portOrDefault(m.cfg.Port)),
	}
	switch m.cfg.TLSMode {
	case config.TLSModeNone:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	case config.TLSModeStartTLS:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
	case config.TLSModeTLS:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory), mail.WithSSLPort(true))
	}
	if m.cfg.Username != "" {
		// AUTH PLAIN only: plain variant when the relay is reached without
		// TLS. LOGIN-only relays are not supported yet — add SMTPAuthLogin
		// here if a deployment ever needs it.
		authType := mail.SMTPAuthPlain
		if m.cfg.TLSMode == config.TLSModeNone {
			authType = mail.SMTPAuthPlainNoEnc
		}
		opts = append(opts,
			mail.WithSMTPAuth(authType),
			mail.WithUsername(m.cfg.Username),
			mail.WithPassword(m.cfg.Password))
	}

	client, err := mail.NewClient(m.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("failed to create smtp client: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, mi); err != nil {
		// Never log AUTH material: library errors quote the SMTP status,
		// not the credentials.
		m.logger.Warn("smtp send failed",
			slog.String("to", strings.Join(msg.To, ",")),
			slog.String("subject", msg.Subject),
			slog.String("error", err.Error()))
		return fmt.Errorf("smtp send failed: %w", err)
	}

	m.logger.Info("email sent",
		slog.String("to", strings.Join(msg.To, ",")),
		slog.String("subject", msg.Subject))
	return nil
}

func portOrDefault(port string) int {
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n < 1 || n > 65535 {
		return 25
	}
	return n
}
