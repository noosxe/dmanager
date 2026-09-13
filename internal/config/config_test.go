package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testTailscaleStateDir is the canonical persistent state directory used in
// state-dir derivation and validation tests.
const testTailscaleStateDir = "/var/lib/dmanager/tailscale"

func TestConfigDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load default config: %v", err)
	}

	if cfg.Server.Port != "9283" {
		t.Errorf("expected default port 9283, got %s", cfg.Server.Port)
	}
	if cfg.Auth.SessionIdleTimeout != 168*time.Hour {
		t.Errorf("expected default session_idle_timeout 168h, got %v", cfg.Auth.SessionIdleTimeout)
	}
	if cfg.Auth.SessionAbsoluteTimeout != 720*time.Hour {
		t.Errorf("expected default session_absolute_timeout 720h, got %v", cfg.Auth.SessionAbsoluteTimeout)
	}
	if cfg.Auth.RememberMeIdleTimeout != 720*time.Hour {
		t.Errorf("expected default remember_me_idle_timeout 720h, got %v", cfg.Auth.RememberMeIdleTimeout)
	}
	if cfg.Auth.RememberMeAbsoluteTimeout != 2160*time.Hour {
		t.Errorf("expected default remember_me_absolute_timeout 2160h, got %v", cfg.Auth.RememberMeAbsoluteTimeout)
	}
	if cfg.Auth.SecureCookies != SecureCookiesAuto {
		t.Errorf("expected default secure_cookies auto, got %s", cfg.Auth.SecureCookies)
	}
	if cfg.Auth.BcryptCost != 12 {
		t.Errorf("expected default bcrypt_cost 12, got %d", cfg.Auth.BcryptCost)
	}
	if cfg.Server.TrustedProxy {
		t.Errorf("expected default trusted_proxy false, got true")
	}
	if cfg.SMTP.Enabled {
		t.Errorf("expected default smtp.enabled false, got true")
	}
	if cfg.SMTP.Port != "25" {
		t.Errorf("expected default smtp.port 25, got %q", cfg.SMTP.Port)
	}
	if cfg.SMTP.TLSMode != TLSModeNone {
		t.Errorf("expected default smtp.tls_mode none, got %q", cfg.SMTP.TLSMode)
	}
	if cfg.SMTP.TimeoutSeconds != 15 {
		t.Errorf("expected default smtp.timeout_seconds 15, got %d", cfg.SMTP.TimeoutSeconds)
	}
	if cfg.Auth.BreachedPasswordCheck {
		t.Errorf("expected default breached_password_check false, got true")
	}
	if cfg.WebAuthn.RequireUserVerification != UserVerificationPreferred {
		t.Errorf("expected default webauthn.require_user_verification 'preferred', got %q", cfg.WebAuthn.RequireUserVerification)
	}
}

func TestConfigYAML(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "config.yaml")

	content := `
server:
  port: "8080"
  db_path: "/tmp/custom.db"
  trusted_proxy: true
auth:
  session_idle_timeout: 48h
  session_absolute_timeout: 120h
  remember_me_idle_timeout: 100h
  remember_me_absolute_timeout: 300h
  secure_cookies: always
  bcrypt_cost: 14
  breached_password_check: true
webauthn:
  rp_id: "dmanager.example.com"
  origins:
    - "https://dmanager.example.com"
    - "https://localhost:9283"
  require_user_verification: "required"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}

	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("failed to load yaml config: %v", err)
	}

	if cfg.Server.Port != "8080" {
		t.Errorf("expected port 8080, got %s", cfg.Server.Port)
	}
	if !cfg.Server.TrustedProxy {
		t.Errorf("expected trusted_proxy true, got false")
	}
	if cfg.Auth.SessionIdleTimeout != 48*time.Hour {
		t.Errorf("expected session_idle_timeout 48h, got %v", cfg.Auth.SessionIdleTimeout)
	}
	if cfg.Auth.SessionAbsoluteTimeout != 120*time.Hour {
		t.Errorf("expected session_absolute_timeout 120h, got %v", cfg.Auth.SessionAbsoluteTimeout)
	}
	if cfg.Auth.RememberMeIdleTimeout != 100*time.Hour {
		t.Errorf("expected remember_me_idle_timeout 100h, got %v", cfg.Auth.RememberMeIdleTimeout)
	}
	if cfg.Auth.RememberMeAbsoluteTimeout != 300*time.Hour {
		t.Errorf("expected remember_me_absolute_timeout 300h, got %v", cfg.Auth.RememberMeAbsoluteTimeout)
	}
	if cfg.Auth.SecureCookies != "always" {
		t.Errorf("expected secure_cookies always, got %s", cfg.Auth.SecureCookies)
	}
	if cfg.Auth.BcryptCost != 14 {
		t.Errorf("expected bcrypt_cost 14, got %d", cfg.Auth.BcryptCost)
	}
	if !cfg.Auth.BreachedPasswordCheck {
		t.Errorf("expected breached_password_check true, got false")
	}
	if cfg.WebAuthn.RPID != "dmanager.example.com" {
		t.Errorf("expected rp_id 'dmanager.example.com', got %q", cfg.WebAuthn.RPID)
	}
	if len(cfg.WebAuthn.Origins) != 2 || cfg.WebAuthn.Origins[0] != "https://dmanager.example.com" {
		t.Errorf("unexpected origins: %v", cfg.WebAuthn.Origins)
	}
	if cfg.WebAuthn.RequireUserVerification != "required" {
		t.Errorf("expected require_user_verification 'required', got %q", cfg.WebAuthn.RequireUserVerification)
	}
}

func TestConfigEnvOverrides(t *testing.T) {
	t.Setenv("DMANAGER_SERVER_TRUSTED_PROXY", "true")
	t.Setenv("DMANAGER_AUTH_SESSION_IDLE_TIMEOUT", "24h")
	t.Setenv("DMANAGER_AUTH_SESSION_ABSOLUTE_TIMEOUT", "48h")
	t.Setenv("DMANAGER_AUTH_REMEMBER_ME_IDLE_TIMEOUT", "72h")
	t.Setenv("DMANAGER_AUTH_REMEMBER_ME_ABSOLUTE_TIMEOUT", "168h")
	t.Setenv("DMANAGER_AUTH_SECURE_COOKIES", "never")
	t.Setenv("DMANAGER_AUTH_BCRYPT_COST", "13")
	t.Setenv("DMANAGER_AUTH_BREACHED_PASSWORD_CHECK", "true")
	t.Setenv("DMANAGER_WEBAUTHN_RP_ID", "auth.local")
	t.Setenv("DMANAGER_WEBAUTHN_ORIGINS", "http://localhost:9283,https://auth.local:9283")
	t.Setenv("DMANAGER_WEBAUTHN_REQUIRE_USER_VERIFICATION", "preferred")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load env config: %v", err)
	}

	if !cfg.Server.TrustedProxy {
		t.Errorf("expected env server.trusted_proxy true, got false")
	}
	if !cfg.Auth.BreachedPasswordCheck {
		t.Errorf("expected env auth.breached_password_check true, got false")
	}

	if cfg.Auth.SessionIdleTimeout != 24*time.Hour {
		t.Errorf("expected session_idle_timeout 24h, got %v", cfg.Auth.SessionIdleTimeout)
	}
	if cfg.Auth.SessionAbsoluteTimeout != 48*time.Hour {
		t.Errorf("expected session_absolute_timeout 48h, got %v", cfg.Auth.SessionAbsoluteTimeout)
	}
	if cfg.Auth.RememberMeIdleTimeout != 72*time.Hour {
		t.Errorf("expected remember_me_idle_timeout 72h, got %v", cfg.Auth.RememberMeIdleTimeout)
	}
	if cfg.Auth.RememberMeAbsoluteTimeout != 168*time.Hour {
		t.Errorf("expected remember_me_absolute_timeout 168h, got %v", cfg.Auth.RememberMeAbsoluteTimeout)
	}
	if cfg.Auth.SecureCookies != "never" {
		t.Errorf("expected secure_cookies never, got %s", cfg.Auth.SecureCookies)
	}
	if cfg.Auth.BcryptCost != 13 {
		t.Errorf("expected bcrypt_cost 13, got %d", cfg.Auth.BcryptCost)
	}
	if cfg.WebAuthn.RPID != "auth.local" {
		t.Errorf("expected rp_id 'auth.local', got %q", cfg.WebAuthn.RPID)
	}
	if len(cfg.WebAuthn.Origins) != 2 || cfg.WebAuthn.Origins[0] != "http://localhost:9283" {
		t.Errorf("unexpected origins: %v", cfg.WebAuthn.Origins)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr bool
	}{
		{
			name: "idle timeout zero",
			mutate: func(c *Config) {
				c.Auth.SessionIdleTimeout = 0
			},
			wantErr: true,
		},
		{
			name: "absolute timeout less than idle timeout",
			mutate: func(c *Config) {
				c.Auth.SessionIdleTimeout = 100 * time.Hour
				c.Auth.SessionAbsoluteTimeout = 50 * time.Hour
			},
			wantErr: true,
		},
		{
			name: "remember_me idle zero",
			mutate: func(c *Config) {
				c.Auth.RememberMeIdleTimeout = 0
			},
			wantErr: true,
		},
		{
			name: "remember_me absolute less than idle",
			mutate: func(c *Config) {
				c.Auth.RememberMeIdleTimeout = 100 * time.Hour
				c.Auth.RememberMeAbsoluteTimeout = 50 * time.Hour
			},
			wantErr: true,
		},
		{
			name: "invalid secure_cookies",
			mutate: func(c *Config) {
				c.Auth.SecureCookies = "sometimes"
			},
			wantErr: true,
		},
		{
			name: "bcrypt cost too low",
			mutate: func(c *Config) {
				c.Auth.BcryptCost = 3
			},
			wantErr: true,
		},
		{
			name: "bcrypt cost too high",
			mutate: func(c *Config) {
				c.Auth.BcryptCost = 32
			},
			wantErr: true,
		},
		{
			name: "invalid webauthn require_user_verification",
			mutate: func(c *Config) {
				c.WebAuthn.RequireUserVerification = "mandatory"
			},
			wantErr: true,
		},
		{
			name: "webauthn rp_id set without origins",
			mutate: func(c *Config) {
				c.WebAuthn.RPID = "localhost"
				c.WebAuthn.Origins = []string{}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load("")
			if err != nil {
				t.Fatalf("failed to load base config: %v", err)
			}
			tt.mutate(cfg)
			err = cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigSMTPYAMLAndEnv(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "config.yaml")
	content := `
smtp:
  enabled: true
  host: "postfix.relay.internal"
  port: "587"
  from_email: "noreply@example.com"
  from_name: "dmanager"
  tls_mode: "starttls"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("failed to load smtp yaml config: %v", err)
	}
	if !cfg.SMTP.Enabled || cfg.SMTP.Host != "postfix.relay.internal" || cfg.SMTP.Port != "587" {
		t.Fatalf("unexpected smtp config from yaml: %+v", cfg.SMTP)
	}
	if cfg.SMTP.TLSMode != TLSModeStartTLS || cfg.SMTP.FromEmail != "noreply@example.com" {
		t.Fatalf("unexpected smtp config from yaml: %+v", cfg.SMTP)
	}
	if cfg.SMTP.TimeoutSeconds != 15 {
		t.Errorf("expected default timeout to survive, got %d", cfg.SMTP.TimeoutSeconds)
	}

	t.Setenv("DMANAGER_SMTP_HOST", "other.relay.internal")
	t.Setenv("DMANAGER_SMTP_PASSWORD", "relay-secret")
	cfg, err = Load(yamlPath)
	if err != nil {
		t.Fatalf("failed to load smtp env config: %v", err)
	}
	if cfg.SMTP.Host != "other.relay.internal" {
		t.Errorf("expected env host override, got %q", cfg.SMTP.Host)
	}
	if cfg.SMTP.Password != "relay-secret" {
		t.Errorf("expected env password, got %q", cfg.SMTP.Password)
	}
}

const invalidModeValue = "sometimes"

func TestConfigSMTPValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *SMTPConfig)
		wantErr bool
	}{
		{
			name: "missing host",
			mutate: func(c *SMTPConfig) {
				c.Host = ""
			},
			wantErr: true,
		},
		{
			name: "missing port",
			mutate: func(c *SMTPConfig) {
				c.Port = ""
			},
			wantErr: true,
		},
		{
			name: "non-numeric port",
			mutate: func(c *SMTPConfig) {
				c.Port = "smtp"
			},
			wantErr: true,
		},
		{
			name: "missing from_email",
			mutate: func(c *SMTPConfig) {
				c.FromEmail = ""
			},
			wantErr: true,
		},
		{
			name: "malformed from_email",
			mutate: func(c *SMTPConfig) {
				c.FromEmail = "noreply"
			},
			wantErr: true,
		},
		{
			name: "invalid tls_mode",
			mutate: func(c *SMTPConfig) {
				c.TLSMode = invalidModeValue
			},
			wantErr: true,
		},
		{
			name: "timeout over range",
			mutate: func(c *SMTPConfig) {
				c.TimeoutSeconds = 121
			},
			wantErr: true,
		},
		{
			name: "timeout negative",
			mutate: func(c *SMTPConfig) {
				c.TimeoutSeconds = -5
			},
			wantErr: true,
		},
		{
			name: "zero timeout falls back to default",
			mutate: func(c *SMTPConfig) {
				c.TimeoutSeconds = 0
			},
			wantErr: false,
		},
		{
			name: "valid full section",
			mutate: func(c *SMTPConfig) {
				c.Username = "relay-user"
				c.Password = "relay-secret"
				c.TLSMode = TLSModeTLS
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &SMTPConfig{
				Enabled:   true,
				Host:      "postfix.relay.internal",
				Port:      "25",
				FromEmail: "noreply@example.com",
				FromName:  "dmanager",
				TLSMode:   TLSModeNone,
			}
			tc.mutate(c)
			err := c.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigSMTPDisabledInert(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{
			SessionIdleTimeout:        168 * time.Hour,
			SessionAbsoluteTimeout:    720 * time.Hour,
			RememberMeIdleTimeout:     720 * time.Hour,
			RememberMeAbsoluteTimeout: 2160 * time.Hour,
			SecureCookies:             SecureCookiesAuto,
			BcryptCost:                12,
		},
		SMTP: SMTPConfig{
			Enabled:  false,
			TLSMode:  invalidModeValue,
			Port:     "not-a-port",
			FromName: "x",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("disabled smtp section must be inert, got: %v", err)
	}
}

func TestConfigTailscaleDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load default config: %v", err)
	}
	if cfg.Tailscale.AuthKey != "" {
		t.Errorf("expected default empty tailscale.auth_key, got %q", cfg.Tailscale.AuthKey)
	}
	if cfg.Tailscale.Hostname != "dmanager" {
		t.Errorf("expected default tailscale.hostname dmanager, got %q", cfg.Tailscale.Hostname)
	}
	if cfg.Tailscale.Port != 80 {
		t.Errorf("expected default tailscale.port 80, got %d", cfg.Tailscale.Port)
	}
	if cfg.Tailscale.HTTPSEnabled {
		t.Error("expected default tailscale.https_enabled false")
	}
	// The state dir derives from the default db_path ("dmanager.db") whose
	// directory is the working directory.
	if cfg.Tailscale.StateDir != "tailscale" {
		t.Errorf("expected derived tailscale.state_dir %q, got %q", "tailscale", cfg.Tailscale.StateDir)
	}
}

func TestConfigTailscaleYAML(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "config.yaml")
	content := `
server:
  db_path: "/var/lib/dmanager/dmanager.db"
tailscale:
  auth_key: "tskey-auth-yaml"
  hostname: "dm-yaml"
  state_dir: "/var/lib/tsnet-state"
  port: 9283
  https_enabled: true
`
	if err := os.WriteFile(yamlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}

	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("failed to load yaml config: %v", err)
	}

	if cfg.Tailscale.AuthKey != "tskey-auth-yaml" {
		t.Errorf("expected tailscale.auth_key tskey-auth-yaml, got %q", cfg.Tailscale.AuthKey)
	}
	if cfg.Tailscale.Hostname != "dm-yaml" {
		t.Errorf("expected tailscale.hostname dm-yaml, got %q", cfg.Tailscale.Hostname)
	}
	if cfg.Tailscale.StateDir != "/var/lib/tsnet-state" {
		t.Errorf("expected tailscale.state_dir /var/lib/tsnet-state, got %q", cfg.Tailscale.StateDir)
	}
	if cfg.Tailscale.Port != 9283 {
		t.Errorf("expected tailscale.port 9283, got %d", cfg.Tailscale.Port)
	}
	if !cfg.Tailscale.HTTPSEnabled {
		t.Error("expected tailscale.https_enabled true")
	}
}

func TestConfigTailscaleBareEnvAliases(t *testing.T) {
	t.Setenv("TAILSCALE_AUTHKEY", "tskey-auth-bare")
	t.Setenv("TAILSCALE_HOSTNAME", "dm-bare")
	t.Setenv("TAILSCALE_STATE_DIR", "/tmp/dm-bare-state")
	t.Setenv("TAILSCALE_PORT", "9000")
	t.Setenv("TAILSCALE_HTTPS_ENABLED", "true")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load env config: %v", err)
	}

	if cfg.Tailscale.AuthKey != "tskey-auth-bare" {
		t.Errorf("expected bare-env auth key, got %q", cfg.Tailscale.AuthKey)
	}
	if cfg.Tailscale.Hostname != "dm-bare" {
		t.Errorf("expected bare-env hostname, got %q", cfg.Tailscale.Hostname)
	}
	if cfg.Tailscale.StateDir != "/tmp/dm-bare-state" {
		t.Errorf("expected bare-env state dir, got %q", cfg.Tailscale.StateDir)
	}
	if cfg.Tailscale.Port != 9000 {
		t.Errorf("expected bare-env port 9000, got %d", cfg.Tailscale.Port)
	}
	if !cfg.Tailscale.HTTPSEnabled {
		t.Error("expected bare-env https_enabled true")
	}
}

func TestConfigTailscalePrefixedEnvBeatsBare(t *testing.T) {
	t.Setenv("DMANAGER_TAILSCALE_AUTHKEY", "tskey-auth-prefixed")
	t.Setenv("DMANAGER_TAILSCALE_HOSTNAME", "dm-prefixed")
	t.Setenv("DMANAGER_TAILSCALE_HTTPS_ENABLED", "true")
	t.Setenv("TAILSCALE_AUTHKEY", "tskey-auth-bare")
	t.Setenv("TAILSCALE_HOSTNAME", "dm-bare")
	t.Setenv("TAILSCALE_HTTPS_ENABLED", "false")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load env config: %v", err)
	}

	if cfg.Tailscale.AuthKey != "tskey-auth-prefixed" {
		t.Errorf("expected prefixed auth key to win, got %q", cfg.Tailscale.AuthKey)
	}
	if cfg.Tailscale.Hostname != "dm-prefixed" {
		t.Errorf("expected prefixed hostname to win, got %q", cfg.Tailscale.Hostname)
	}
	if !cfg.Tailscale.HTTPSEnabled {
		t.Error("expected prefixed https_enabled true to win over bare false")
	}
}

func TestConfigTailscaleStateDirDerivedFromDBPath(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "config.yaml")
	content := `
server:
  db_path: "/var/lib/dmanager/dmanager.db"
tailscale:
  auth_key: "tskey-auth-yaml"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}

	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("failed to load yaml config: %v", err)
	}

	want := testTailscaleStateDir
	if cfg.Tailscale.StateDir != want {
		t.Errorf("expected derived tailscale.state_dir %q, got %q", want, cfg.Tailscale.StateDir)
	}
}

func TestConfigTailscaleInvalidBarePort(t *testing.T) {
	t.Setenv("TAILSCALE_AUTHKEY", "tskey-auth-bare")
	t.Setenv("TAILSCALE_PORT", "not-a-port")

	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "TAILSCALE_PORT") {
		t.Errorf("expected TAILSCALE_PORT parse error, got %v", err)
	}
}

func TestConfigTailscaleInvalidBareHTTPSEnabled(t *testing.T) {
	t.Setenv("TAILSCALE_AUTHKEY", "tskey-auth-bare")
	t.Setenv("TAILSCALE_HTTPS_ENABLED", "yes-please")

	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "TAILSCALE_HTTPS_ENABLED") {
		t.Errorf("expected TAILSCALE_HTTPS_ENABLED parse error, got %v", err)
	}
}

func TestConfigTailscaleValidation(t *testing.T) {
	const validTailscaleAuthKey = "tskey-auth-valid"

	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr bool
	}{
		{
			name: "valid enabled section",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.StateDir = testTailscaleStateDir
			},
		},
		{
			name: "uppercase hostname",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = "Dmanager"
			},
			wantErr: true,
		},
		{
			name: "leading hyphen hostname",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = "-dmanager"
			},
			wantErr: true,
		},
		{
			name: "trailing hyphen hostname",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = "dmanager-"
			},
			wantErr: true,
		},
		{
			name: "hostname too long",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = strings.Repeat("a", 64)
			},
			wantErr: true,
		},
		{
			name: "port zero",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Port = 0
			},
			wantErr: true,
		},
		{
			name: "port too high",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Port = 65536
			},
			wantErr: true,
		},
		{
			name: "state dir equals database directory",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				// Default db_path is "dmanager.db" so its directory is ".".
				c.Tailscale.StateDir = "."
			},
			wantErr: true,
		},
		{
			name: "state dir root",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.StateDir = "/"
			},
			wantErr: true,
		},
		{
			name: "invalid hostname ignored while disabled",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = ""
				c.Tailscale.Hostname = "Dmanager!"
				c.Tailscale.Port = 99999
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load("")
			if err != nil {
				t.Fatalf("failed to load base config: %v", err)
			}
			// Load derives the state dir; simulate an explicit one for tests
			// that do not set it so the derivation cannot mask collisions.
			if cfg.Tailscale.StateDir == "" {
				cfg.Tailscale.StateDir = testTailscaleStateDir
			}
			tt.mutate(cfg)
			err = cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
