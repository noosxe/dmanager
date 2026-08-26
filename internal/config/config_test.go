package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
	if cfg.Auth.BreachedPasswordCheck {
		t.Errorf("expected default breached_password_check false, got true")
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
