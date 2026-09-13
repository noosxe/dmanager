package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Registry struct {
	Host     string `koanf:"host"`
	Username string `koanf:"username"`
	Password string `koanf:"password"`
}

type ServerConfig struct {
	Port           string   `koanf:"port"`
	DBPath         string   `koanf:"db_path"`
	AllowedOrigins []string `koanf:"allowed_origins"`
	TrustedProxy   bool     `koanf:"trusted_proxy"`
}

type DockerConfig struct {
	Host string `koanf:"host"`
}

const (
	SecureCookiesAuto   = "auto"
	SecureCookiesAlways = "always"
	SecureCookiesNever  = "never"

	TLSModeNone     = "none"
	TLSModeStartTLS = "starttls"
	TLSModeTLS      = "tls"

	UserVerificationPreferred   = "preferred"
	UserVerificationRequired    = "required"
	UserVerificationDiscouraged = "discouraged"
)

type SchedulerConfig struct {
	IntervalMinutes int `koanf:"interval_minutes"`
}

type AuthConfig struct {
	SessionIdleTimeout        time.Duration `koanf:"session_idle_timeout"`
	SessionAbsoluteTimeout    time.Duration `koanf:"session_absolute_timeout"`
	RememberMeIdleTimeout     time.Duration `koanf:"remember_me_idle_timeout"`
	RememberMeAbsoluteTimeout time.Duration `koanf:"remember_me_absolute_timeout"`
	SecureCookies             string        `koanf:"secure_cookies"`
	BcryptCost                int           `koanf:"bcrypt_cost"`
	BreachedPasswordCheck     bool          `koanf:"breached_password_check"`
}

type SMTPConfig struct {
	Enabled        bool   `koanf:"enabled"`
	Host           string `koanf:"host"`
	Port           string `koanf:"port"`
	Username       string `koanf:"username"`
	Password       string `koanf:"password"`
	FromEmail      string `koanf:"from_email"`
	FromName       string `koanf:"from_name"`
	TLSMode        string `koanf:"tls_mode"`
	TimeoutSeconds int    `koanf:"timeout_seconds"`
}

type WebAuthnConfig struct {
	RPID                    string   `koanf:"rp_id"`
	Origins                 []string `koanf:"origins"`
	RequireUserVerification string   `koanf:"require_user_verification"`
}

// TailscaleConfig configures the embedded tsnet node (see docs/tailscale.md).
// The whole section is inert while AuthKey is empty.
type TailscaleConfig struct {
	AuthKey      string `koanf:"auth_key"`
	Hostname     string `koanf:"hostname"`
	StateDir     string `koanf:"state_dir"`
	Port         int    `koanf:"port"`
	HTTPSEnabled bool   `koanf:"https_enabled"`
}

type Config struct {
	Server     ServerConfig    `koanf:"server"`
	Docker     DockerConfig    `koanf:"docker"`
	Scheduler  SchedulerConfig `koanf:"scheduler"`
	Auth       AuthConfig      `koanf:"auth"`
	WebAuthn   WebAuthnConfig  `koanf:"webauthn"`
	SMTP       SMTPConfig      `koanf:"smtp"`
	Tailscale  TailscaleConfig `koanf:"tailscale"`
	Registries []Registry      `koanf:"registries"`
}

// Validate verifies that the loaded configuration contains valid parameters.
func (c *Config) Validate() error {
	if c.Auth.SessionIdleTimeout <= 0 {
		return fmt.Errorf("auth.session_idle_timeout must be greater than 0")
	}
	if c.Auth.SessionAbsoluteTimeout < c.Auth.SessionIdleTimeout {
		return fmt.Errorf("auth.session_absolute_timeout must be greater than or equal to auth.session_idle_timeout")
	}
	if c.Auth.RememberMeIdleTimeout <= 0 {
		return fmt.Errorf("auth.remember_me_idle_timeout must be greater than 0")
	}
	if c.Auth.RememberMeAbsoluteTimeout < c.Auth.RememberMeIdleTimeout {
		return fmt.Errorf("auth.remember_me_absolute_timeout must be greater than or equal to auth.remember_me_idle_timeout")
	}
	switch c.Auth.SecureCookies {
	case SecureCookiesAuto, SecureCookiesAlways, SecureCookiesNever:
	default:
		return fmt.Errorf("auth.secure_cookies must be one of 'auto', 'always', 'never', got %q", c.Auth.SecureCookies)
	}
	if c.Auth.BcryptCost < 4 || c.Auth.BcryptCost > 31 {
		return fmt.Errorf("auth.bcrypt_cost must be between 4 and 31, got %d", c.Auth.BcryptCost)
	}

	if c.WebAuthn.RequireUserVerification == "" {
		c.WebAuthn.RequireUserVerification = UserVerificationPreferred
	}
	switch c.WebAuthn.RequireUserVerification {
	case UserVerificationPreferred, UserVerificationRequired, UserVerificationDiscouraged:
	default:
		return fmt.Errorf("webauthn.require_user_verification must be one of 'preferred', 'required', 'discouraged', got %q", c.WebAuthn.RequireUserVerification)
	}

	if c.WebAuthn.RPID != "" && len(c.WebAuthn.Origins) == 0 {
		return fmt.Errorf("webauthn.origins cannot be empty when webauthn.rp_id is set")
	}

	// The whole SMTP section is inert while disabled: partial or commented-out
	// relay details in the compose file must not break startup.
	if c.SMTP.Enabled {
		if err := c.SMTP.Validate(); err != nil {
			return err
		}
	}

	// The tailscale section is inert while no auth key is configured.
	if c.Tailscale.AuthKey != "" {
		if err := c.Tailscale.Validate(filepath.Dir(c.Server.DBPath)); err != nil {
			return err
		}
	}

	return nil
}

// Validate checks the SMTP section itself; only called when Enabled is true.
func (c *SMTPConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("smtp.host is required when smtp.enabled is true")
	}
	if strings.TrimSpace(c.Port) == "" {
		return fmt.Errorf("smtp.port is required when smtp.enabled is true")
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("smtp.port must be a valid port number, got %q", c.Port)
	}
	if !strings.Contains(c.FromEmail, "@") {
		return fmt.Errorf("smtp.from_email must be a valid address, got %q", c.FromEmail)
	}
	switch c.TLSMode {
	case TLSModeNone, TLSModeStartTLS, TLSModeTLS:
	default:
		return fmt.Errorf("smtp.tls_mode must be one of 'none', 'starttls', 'tls', got %q", c.TLSMode)
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 15
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 120 {
		return fmt.Errorf("smtp.timeout_seconds must be between 1 and 120, got %d", c.TimeoutSeconds)
	}
	return nil
}

// Validate checks the tailscale section itself; only called when an auth key
// is set. dbDir is the directory holding the SQLite database, used to reject
// state directories that would collide with it.
func (c *TailscaleConfig) Validate(dbDir string) error {
	if !isValidTailscaleHostname(c.Hostname) {
		return fmt.Errorf("tailscale.hostname must use lowercase letters, digits and hyphens, start and end with a letter or digit, and be at most 63 characters, got %q", c.Hostname)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("tailscale.port must be between 1 and 65535, got %d", c.Port)
	}
	if strings.TrimSpace(c.StateDir) == "" {
		return fmt.Errorf("tailscale.state_dir must not be empty when tailscale.auth_key is set")
	}
	cleanDir := filepath.Clean(c.StateDir)
	if cleanDir == "/" || cleanDir == filepath.Clean(dbDir) {
		return fmt.Errorf("tailscale.state_dir %q must be a dedicated directory, not the database directory or root", c.StateDir)
	}
	return nil
}

// isValidTailscaleHostname reports whether s is acceptable as a Tailscale
// MagicDNS hostname: lowercase letters, digits and hyphens; starts and ends
// with a letter or digit; at most 63 characters.
func isValidTailscaleHostname(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		if c == '-' && i > 0 && i < len(s)-1 {
			continue
		}
		return false
	}
	return true
}

// Load loads configuration from the specified path, default paths, and environment variables.
func Load(configPath string) (*Config, error) {
	k := koanf.New(".")

	// 1. Load default fallback values
	defaults := map[string]interface{}{
		"server.port":                        "9283",
		"server.db_path":                     "dmanager.db",
		"server.trusted_proxy":               false,
		"docker.host":                        "unix:///var/run/docker.sock",
		"scheduler.interval_minutes":         60,
		"auth.session_idle_timeout":          168 * time.Hour,
		"auth.session_absolute_timeout":      720 * time.Hour,
		"auth.remember_me_idle_timeout":      720 * time.Hour,
		"auth.remember_me_absolute_timeout":  2160 * time.Hour,
		"auth.secure_cookies":                SecureCookiesAuto,
		"auth.bcrypt_cost":                   12,
		"auth.breached_password_check":       false,
		"webauthn.require_user_verification": "preferred",
		"smtp.enabled":                       false,
		"smtp.port":                          "25",
		"smtp.tls_mode":                      TLSModeNone,
		"smtp.timeout_seconds":               15,
		"tailscale.hostname":                 "dmanager",
		"tailscale.port":                     80,
		"tailscale.https_enabled":            false,
	}
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return nil, fmt.Errorf("failed to load default configuration: %w", err)
	}

	// 2. Load YAML configuration file
	var targetFile string
	if configPath != "" {
		targetFile = configPath
		if _, err := os.Stat(targetFile); err != nil {
			return nil, fmt.Errorf("config file %q not found: %w", targetFile, err)
		}
	} else {
		// Default search paths
		etcPath := "/etc/dmanager/config.yaml"
		localPath := "config.yaml"
		if _, err := os.Stat(etcPath); err == nil {
			targetFile = etcPath
		} else if _, err := os.Stat(localPath); err == nil {
			targetFile = localPath
		}
	}

	if targetFile != "" {
		if err := k.Load(file.Provider(targetFile), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("failed to load config file %q: %w", targetFile, err)
		}
	}

	// 3. Load environment variables with prefix DMANAGER_ (except registries which are post-processed)
	err := k.Load(env.Provider(".", env.Opt{
		Prefix: "DMANAGER_",
		TransformFunc: func(s string, v string) (string, interface{}) {
			key := strings.TrimPrefix(s, "DMANAGER_")
			key = strings.ToLower(key)

			if strings.HasPrefix(key, "server_") {
				sub := strings.TrimPrefix(key, "server_")
				if sub == "allowed_origins" {
					var origins []string
					for _, part := range strings.Split(v, ",") {
						trimmed := strings.TrimSpace(part)
						if trimmed != "" {
							origins = append(origins, trimmed)
						}
					}
					return "server.allowed_origins", origins
				}
				return "server." + sub, v
			}
			if strings.HasPrefix(key, "docker_") {
				sub := strings.TrimPrefix(key, "docker_")
				return "docker." + sub, v
			}
			if strings.HasPrefix(key, "scheduler_") {
				sub := strings.TrimPrefix(key, "scheduler_")
				return "scheduler." + sub, v
			}
			if strings.HasPrefix(key, "auth_") {
				sub := strings.TrimPrefix(key, "auth_")
				return "auth." + sub, v
			}
			if strings.HasPrefix(key, "webauthn_") {
				sub := strings.TrimPrefix(key, "webauthn_")
				if sub == "origins" {
					var origins []string
					for _, part := range strings.Split(v, ",") {
						trimmed := strings.TrimSpace(part)
						if trimmed != "" {
							origins = append(origins, trimmed)
						}
					}
					return "webauthn.origins", origins
				}
				return "webauthn." + sub, v
			}
			if strings.HasPrefix(key, "smtp_") {
				sub := strings.TrimPrefix(key, "smtp_")
				return "smtp." + sub, v
			}
			if strings.HasPrefix(key, "tailscale_") {
				sub := strings.TrimPrefix(key, "tailscale_")
				if sub == "authkey" {
					// TAILSCALE_AUTHKEY (per feature spec) maps to the auth_key YAML key.
					sub = "auth_key"
				}
				return "tailscale." + sub, v
			}
			// Registries are handled in manual post-processing
			return "", nil
		},
	}), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load environment variables: %w", err)
	}

	// Unmarshal configuration into struct
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration: %w", err)
	}

	// Apply bare TAILSCALE_* environment aliases; the DMANAGER_TAILSCALE_*
	// variables were already merged by the env provider above and win here.
	if err := applyTailscaleEnvAliases(&cfg); err != nil {
		return nil, fmt.Errorf("invalid environment: %w", err)
	}

	// Default the node state directory to a sibling of the SQLite database so
	// container and bare-metal deployments persist it on the same volume.
	if cfg.Tailscale.StateDir == "" {
		cfg.Tailscale.StateDir = filepath.Join(filepath.Dir(cfg.Server.DBPath), "tailscale")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// 4. Manually post-process DMANAGER_REGISTRIES_ environment variables
	for i := 0; ; i++ {
		hostKey := fmt.Sprintf("DMANAGER_REGISTRIES_%d_HOST", i)
		userKey := fmt.Sprintf("DMANAGER_REGISTRIES_%d_USERNAME", i)
		passKey := fmt.Sprintf("DMANAGER_REGISTRIES_%d_PASSWORD", i)

		hostVal, hostOk := os.LookupEnv(hostKey)
		userVal, userOk := os.LookupEnv(userKey)
		passVal, passOk := os.LookupEnv(passKey)

		if !hostOk && !userOk && !passOk {
			break
		}

		reg := Registry{}
		if hostOk {
			reg.Host = hostVal
		}
		if userOk {
			reg.Username = userVal
		}
		if passOk {
			reg.Password = passVal
		}

		if i < len(cfg.Registries) {
			if hostOk {
				cfg.Registries[i].Host = hostVal
			}
			if userOk {
				cfg.Registries[i].Username = userVal
			}
			if passOk {
				cfg.Registries[i].Password = passVal
			}
		} else {
			cfg.Registries = append(cfg.Registries, reg)
		}
	}

	return &cfg, nil
}

// applyTailscaleEnvAliases maps the bare TAILSCALE_* variable names onto the
// tailscale config section. A bare alias only applies when its
// DMANAGER_-prefixed counterpart is unset; bare aliases still override YAML
// values, giving the precedence: prefixed > bare > YAML > defaults.
func applyTailscaleEnvAliases(cfg *Config) error {
	if v := os.Getenv("TAILSCALE_AUTHKEY"); v != "" && os.Getenv("DMANAGER_TAILSCALE_AUTHKEY") == "" {
		cfg.Tailscale.AuthKey = v
	}
	if v := os.Getenv("TAILSCALE_HOSTNAME"); v != "" && os.Getenv("DMANAGER_TAILSCALE_HOSTNAME") == "" {
		cfg.Tailscale.Hostname = v
	}
	if v := os.Getenv("TAILSCALE_STATE_DIR"); v != "" && os.Getenv("DMANAGER_TAILSCALE_STATE_DIR") == "" {
		cfg.Tailscale.StateDir = v
	}
	if v := strings.TrimSpace(os.Getenv("TAILSCALE_PORT")); v != "" && os.Getenv("DMANAGER_TAILSCALE_PORT") == "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid TAILSCALE_PORT value %q: must be a port number", v)
		}
		cfg.Tailscale.Port = p
	}
	if v := strings.TrimSpace(os.Getenv("TAILSCALE_HTTPS_ENABLED")); v != "" && os.Getenv("DMANAGER_TAILSCALE_HTTPS_ENABLED") == "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid TAILSCALE_HTTPS_ENABLED value %q: must be a boolean", v)
		}
		cfg.Tailscale.HTTPSEnabled = b
	}
	return nil
}
