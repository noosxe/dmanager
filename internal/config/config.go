package config

import (
	"fmt"
	"os"
	"strings"

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
}

type DockerConfig struct {
	Host string `koanf:"host"`
}

type SchedulerConfig struct {
	IntervalMinutes int `koanf:"interval_minutes"`
}

type Config struct {
	Server     ServerConfig    `koanf:"server"`
	Docker     DockerConfig    `koanf:"docker"`
	Scheduler  SchedulerConfig `koanf:"scheduler"`
	Registries []Registry      `koanf:"registries"`
}

// Load loads configuration from the specified path, default paths, and environment variables.
func Load(configPath string) (*Config, error) {
	k := koanf.New(".")

	// 1. Load default fallback values
	defaults := map[string]interface{}{
		"server.port":                "8080",
		"server.db_path":             "dmanager.db",
		"docker.host":                "unix:///var/run/docker.sock",
		"scheduler.interval_minutes": 60,
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
