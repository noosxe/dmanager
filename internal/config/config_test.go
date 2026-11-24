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
	if cfg.Server.TrustedProxyHops != 1 {
		t.Errorf("expected default trusted_proxy_hops 1, got %d", cfg.Server.TrustedProxyHops)
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
	t.Setenv("DMANAGER_SERVER_TRUSTED_PROXY_HOPS", "2")
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
	if cfg.Server.TrustedProxyHops != 2 {
		t.Errorf("expected env server.trusted_proxy_hops 2, got %d", cfg.Server.TrustedProxyHops)
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
			name: "trusted_proxy_hops zero",
			mutate: func(c *Config) {
				c.Server.TrustedProxyHops = 0
			},
			wantErr: true,
		},
		{
			name: "trusted_proxy_hops negative",
			mutate: func(c *Config) {
				c.Server.TrustedProxyHops = -1
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
		Server: ServerConfig{Port: "9283", HTTP: HTTPConfig{Enabled: true}, TrustedProxyHops: 1},
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
	if cfg.Tailscale.Enabled {
		t.Error("expected default tailscale.enabled false")
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
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.StateDir = testTailscaleStateDir
			},
		},
		{
			name: "uppercase hostname",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = "Dmanager"
			},
			wantErr: true,
		},
		{
			name: "leading hyphen hostname",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = "-dmanager"
			},
			wantErr: true,
		},
		{
			name: "trailing hyphen hostname",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = "dmanager-"
			},
			wantErr: true,
		},
		{
			name: "hostname too long",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = strings.Repeat("a", 64)
			},
			wantErr: true,
		},
		{
			name: "port zero",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Port = 0
			},
			wantErr: true,
		},
		{
			name: "port too high",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Port = 65536
			},
			wantErr: true,
		},
		{
			name: "state dir equals database directory",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				// Default db_path is "dmanager.db" so its directory is ".".
				c.Tailscale.StateDir = "."
			},
			wantErr: true,
		},
		{
			name: "state dir root",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.StateDir = "/"
			},
			wantErr: true,
		},
		{
			name: "enabled without auth key",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.StateDir = testTailscaleStateDir
			},
			wantErr: true,
		},
		{
			name: "enabled with blank auth key",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = "   "
				c.Tailscale.StateDir = testTailscaleStateDir
			},
			wantErr: true,
		},
		{
			name: "disabled with auth key is inert even if otherwise invalid",
			mutate: func(c *Config) {
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.Hostname = "Dmanager!"
				c.Tailscale.Port = 99999
			},
		},
		{
			name: "invalid port ignored in https mode",
			mutate: func(c *Config) {
				c.Tailscale.Enabled = true
				c.Tailscale.AuthKey = validTailscaleAuthKey
				c.Tailscale.HTTPSEnabled = true
				c.Tailscale.Port = 0
				c.Tailscale.StateDir = testTailscaleStateDir
			},
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

func TestConfigTailscaleEnabledErrorMessage(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tailscale.Enabled = true
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "tailscale.enabled is true but tailscale.auth_key is empty") {
		t.Errorf("expected enabled-without-key error, got %v", err)
	}
}

func TestConfigTailscaleEnabledEnv(t *testing.T) {
	t.Setenv("TAILSCALE_ENABLED", "true")
	t.Setenv("TAILSCALE_AUTHKEY", "tskey-auth-bare")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Tailscale.Enabled {
		t.Error("expected TAILSCALE_ENABLED=true to enable the feature")
	}
	t.Setenv("DMANAGER_TAILSCALE_ENABLED", "false")
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tailscale.Enabled {
		t.Error("expected prefixed DMANAGER_TAILSCALE_ENABLED=false to beat the bare alias")
	}
}

func TestConfigTailscaleEnabledWithoutKeyFailsLoad(t *testing.T) {
	t.Setenv("TAILSCALE_ENABLED", "true")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "tailscale.auth_key is empty") {
		t.Errorf("expected startup failure for enabled without key, got %v", err)
	}
}

func TestConfigTailscaleInvalidBareEnabled(t *testing.T) {
	t.Setenv("TAILSCALE_ENABLED", "maybe")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "TAILSCALE_ENABLED") {
		t.Errorf("expected TAILSCALE_ENABLED parse error, got %v", err)
	}
}

func TestConfigExternalManagement(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if len(cfg.Docker.ExternalLabelSelectors) != 0 || cfg.Scheduler.NotifyExternalUpdates {
		t.Errorf("unexpected defaults: %+v %+v", cfg.Docker, cfg.Scheduler)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "docker:\n  external_label_selectors:\n    - com.example.pool=true\n    - nixos\nscheduler:\n  notify_external_updates: true\n"
	if err = os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("load yaml: %v", err)
	}
	if got := strings.Join(cfg.Docker.ExternalLabelSelectors, "|"); got != "com.example.pool=true|nixos" {
		t.Errorf("yaml selectors: %q", got)
	}
	if !cfg.Scheduler.NotifyExternalUpdates {
		t.Error("yaml notify_external_updates not set")
	}

	t.Setenv("DMANAGER_DOCKER_EXTERNAL_LABEL_SELECTORS", " a=b , ,c ")
	t.Setenv("DMANAGER_SCHEDULER_NOTIFY_EXTERNAL_UPDATES", "false")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("load env: %v", err)
	}
	if got := strings.Join(cfg.Docker.ExternalLabelSelectors, "|"); got != "a=b|c" {
		t.Errorf("env selectors: %q", got)
	}
	if cfg.Scheduler.NotifyExternalUpdates {
		t.Error("env should override notify_external_updates to false")
	}

	t.Setenv("DMANAGER_DOCKER_EXTERNAL_LABEL_SELECTORS", "=oops")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "external_label_selectors") {
		t.Errorf("expected selector validation error, got %v", err)
	}
}

func TestConfigHTTPListenerDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if !cfg.Server.HTTP.Enabled {
		t.Error("server.http.enabled must default to true")
	}
	if cfg.Server.ListenAddress != "" {
		t.Errorf("server.listen_address must default to all interfaces (empty), got %q", cfg.Server.ListenAddress)
	}
}

func TestConfigHTTPListenerYAMLAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "server:\n  listen_address: 127.0.0.1\n  http:\n    enabled: false\ntailscale:\n  enabled: true\n  auth_key: tskey-test\n  https_enabled: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load yaml: %v", err)
	}
	if cfg.Server.HTTP.Enabled || cfg.Server.ListenAddress != "127.0.0.1" {
		t.Errorf("yaml not applied: %+v", cfg.Server)
	}

	t.Setenv("DMANAGER_SERVER_HTTP_ENABLED", "true")
	t.Setenv("DMANAGER_SERVER_LISTEN_ADDRESS", "::1")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("load env: %v", err)
	}
	if !cfg.Server.HTTP.Enabled || cfg.Server.ListenAddress != "::1" {
		t.Errorf("env not applied: %+v", cfg.Server)
	}

	t.Setenv("DMANAGER_SERVER_HTTP_ENABLED", "false")
	cfg, err = Load("")
	if err == nil {
		t.Fatalf("expected error when no listener is up, got %+v", cfg.Server)
	}
	if !strings.Contains(err.Error(), "no listener would be up") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfigHTTPListenerValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{"http on", func(c *Config) {}, ""},
		{"http off tailscale on", func(c *Config) {
			c.Server.HTTP.Enabled = false
			c.Tailscale = TailscaleConfig{Enabled: true, AuthKey: "k", Hostname: "node", Port: 80, HTTPSEnabled: true, StateDir: testTailscaleStateDir}
		}, ""},
		{"http off tailscale off", func(c *Config) { c.Server.HTTP.Enabled = false }, "no listener would be up"},
		{"ipv4 address", func(c *Config) { c.Server.ListenAddress = "127.0.0.1" }, ""},
		{"ipv6 address", func(c *Config) { c.Server.ListenAddress = "::1" }, ""},
		{"host name", func(c *Config) { c.Server.ListenAddress = "localhost" }, ""},
		{"address with port", func(c *Config) { c.Server.ListenAddress = "127.0.0.1:80" }, "server.listen_address"},
		{"bad port", func(c *Config) { c.Server.Port = "abc" }, "server.port"},
		{"port out of range", func(c *Config) { c.Server.Port = "70000" }, "server.port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Server: ServerConfig{Port: "9283", DBPath: "/var/lib/dmanager/dmanager.db", HTTP: HTTPConfig{Enabled: true}, TrustedProxyHops: 1},
				Auth: AuthConfig{
					SessionIdleTimeout:        time.Hour,
					SessionAbsoluteTimeout:    time.Hour,
					RememberMeIdleTimeout:     time.Hour,
					RememberMeAbsoluteTimeout: time.Hour,
					SecureCookies:             SecureCookiesAuto,
					BcryptCost:                12,
				},
			}
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

const inlineSecret = "inline"

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigSecretFilesEnv(t *testing.T) {
	t.Setenv("DMANAGER_TAILSCALE_ENABLED", "true")
	t.Setenv("DMANAGER_TAILSCALE_AUTHKEY_FILE", writeSecretFile(t, "tskey-from-file\n"))
	t.Setenv("DMANAGER_SMTP_PASSWORD_FILE", writeSecretFile(t, "smtp-pass\r\n"))
	t.Setenv("DMANAGER_REGISTRIES_0_HOST", "registry.example.com")
	t.Setenv("DMANAGER_REGISTRIES_0_PASSWORD_FILE", writeSecretFile(t, "reg-pass\n\n"))
	t.Setenv("DMANAGER_REGISTRIES_1_PASSWORD_FILE", writeSecretFile(t, " spaced pass "))

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tailscale.AuthKey != "tskey-from-file" {
		t.Errorf("auth key = %q", cfg.Tailscale.AuthKey)
	}
	if cfg.SMTP.Password != "smtp-pass" {
		t.Errorf("smtp password = %q", cfg.SMTP.Password)
	}
	if len(cfg.Registries) != 2 || cfg.Registries[0].Password != "reg-pass" || cfg.Registries[1].Password != " spaced pass " {
		t.Errorf("registries = %+v", cfg.Registries)
	}
}

func TestConfigSecretFilesYAMLAndBareAlias(t *testing.T) {
	keyFile := writeSecretFile(t, "tskey-yaml\n")
	pwFile := writeSecretFile(t, "yaml-reg\n")
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "tailscale:\n  enabled: true\n  auth_key_file: " + keyFile + "\nregistries:\n  - host: registry.example.com\n    username: u\n    password_file: " + pwFile + "\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tailscale.AuthKey != "tskey-yaml" || cfg.Registries[0].Password != "yaml-reg" {
		t.Errorf("unexpected secrets: %q %+v", cfg.Tailscale.AuthKey, cfg.Registries)
	}

	t.Setenv("TAILSCALE_AUTHKEY_FILE", writeSecretFile(t, "tskey-bare\n"))
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("load bare: %v", err)
	}
	if cfg.Tailscale.AuthKey != "tskey-bare" {
		t.Errorf("bare alias key = %q", cfg.Tailscale.AuthKey)
	}
}

func TestConfigSecretFileConflictsAndErrors(t *testing.T) {
	file := writeSecretFile(t, "x\n")
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"tailscale both", map[string]string{"DMANAGER_TAILSCALE_ENABLED": "true", "DMANAGER_TAILSCALE_AUTHKEY": inlineSecret, "DMANAGER_TAILSCALE_AUTHKEY_FILE": file}, "tailscale.auth_key and tailscale.auth_key_file are both set"},
		{"smtp both", map[string]string{"DMANAGER_SMTP_PASSWORD": inlineSecret, "DMANAGER_SMTP_PASSWORD_FILE": file}, "smtp.password and smtp.password_file are both set"},
		{"registry both", map[string]string{"DMANAGER_REGISTRIES_0_PASSWORD": inlineSecret, "DMANAGER_REGISTRIES_0_PASSWORD_FILE": file}, "registries[0].password and registries[0].password_file are both set"},
		{"missing file", map[string]string{"DMANAGER_SMTP_PASSWORD_FILE": filepath.Join(t.TempDir(), "nope")}, "failed to read smtp.password_file"},
		{"empty tailscale file fails validation", map[string]string{"DMANAGER_TAILSCALE_ENABLED": "true", "DMANAGER_TAILSCALE_AUTHKEY_FILE": writeSecretFile(t, "\n")}, "auth_key is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := Load("")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if strings.Contains(err.Error(), inlineSecret) {
				t.Errorf("error leaks secret value: %v", err)
			}
		})
	}
}
