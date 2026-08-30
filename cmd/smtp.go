// Subcommand wiring for SMTP-related operations. These are ops-only CLI
// tools: the product UI and API expose no send path (issue #226).
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"dmanager/internal/config"
	"dmanager/internal/mailer"

	"github.com/spf13/cobra"
)

var (
	smtpTo      string
	smtpSubject string
)

var smtpCmd = &cobra.Command{
	Use:   "smtp",
	Short: "SMTP relay operations (ops-only)",
	Long:  `Operations to verify the configured SMTP relay. The product itself sends system emails only; these commands exist for the operator.`,
}

var smtpTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send a verification email through the configured relay",
	Long: `Loads the deployment configuration (same file/env as 'serve') and sends
a short diagnostic email to the given address. A non-zero exit status reports
the relay's error.`,
	RunE: func(runCmd *cobra.Command, _ []string) error {
		if smtpTo == "" {
			return fmt.Errorf("--to is required: dmanager smtp test --to=you@example.com")
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
		return runSMTPTest(runCmd.Context(), cfg.SMTP, smtpTo, smtpSubject, os.Stdout)
	},
}

// runSMTPTest sends the diagnostic email and reports the outcome on stdout.
// Split from RunE so tests can drive it with a fixture config and buffer.
func runSMTPTest(ctx context.Context, cfg config.SMTPConfig, to, subject string, stdout io.Writer) error {
	m := mailer.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !m.Enabled() {
		return fmt.Errorf("smtp is not configured: set smtp.enabled=true (and host, port, from_email) in the config file or via DMANAGER_SMTP_* env vars")
	}

	if subject == "" {
		subject = "dmanager SMTP test"
	}
	err := m.Send(ctx, mailer.Message{
		To:       []string{to},
		Subject:  subject,
		TextBody: fmt.Sprintf("This is a test email from dmanager (relay %s:%s, from %s).\n\nIf you received this, the relay path works.", cfg.Host, cfg.Port, cfg.FromEmail),
	})
	if err != nil {
		return fmt.Errorf("test email failed: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "test email sent to %s (relay %s:%s, from %s)\n", to, cfg.Host, cfg.Port, cfg.FromEmail)
	return nil
}

func init() {
	smtpTestCmd.Flags().StringVar(&smtpTo, "to", "", "recipient address for the test email (required)")
	smtpTestCmd.Flags().StringVar(&smtpSubject, "subject", "", "optional subject override")
	smtpTestCmd.Flags().StringVarP(&configPath, "config", "c", "", "path to yaml configuration file")
	smtpCmd.AddCommand(smtpTestCmd)
	rootCmd.AddCommand(smtpCmd)
}
