package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.Server.Port != "8080" {
		t.Errorf("expected Server.Port to be 8080, got %q", cfg.Server.Port)
	}
	if cfg.Server.DBPath != "dmanager.db" {
		t.Errorf("expected Server.DBPath to be dmanager.db, got %q", cfg.Server.DBPath)
	}
	if cfg.Docker.Host != "unix:///var/run/docker.sock" {
		t.Errorf("expected Docker.Host to be unix:///var/run/docker.sock, got %q", cfg.Docker.Host)
	}
	if cfg.Scheduler.IntervalMinutes != 60 {
		t.Errorf("expected Scheduler.IntervalMinutes to be 60, got %d", cfg.Scheduler.IntervalMinutes)
	}
}

func TestLoadYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
server:
  port: "9090"
  db_path: "custom.db"
  allowed_origins:
    - "http://localhost:3000"
    - "http://example.com"
docker:
  host: "tcp://localhost:2375"
scheduler:
  interval_minutes: 30
registries:
  - host: "registry1.com"
    username: "user1"
    password: "pass1"
  - host: "registry2.com"
    username: "user2"
    password: "pass2"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != "9090" {
		t.Errorf("expected Port 9090, got %q", cfg.Server.Port)
	}
	if cfg.Server.DBPath != "custom.db" {
		t.Errorf("expected DBPath custom.db, got %q", cfg.Server.DBPath)
	}
	expectedOrigins := []string{"http://localhost:3000", "http://example.com"}
	if !reflect.DeepEqual(cfg.Server.AllowedOrigins, expectedOrigins) {
		t.Errorf("expected AllowedOrigins %v, got %v", expectedOrigins, cfg.Server.AllowedOrigins)
	}
	if cfg.Docker.Host != "tcp://localhost:2375" {
		t.Errorf("expected Docker Host tcp://localhost:2375, got %q", cfg.Docker.Host)
	}
	if cfg.Scheduler.IntervalMinutes != 30 {
		t.Errorf("expected IntervalMinutes 30, got %d", cfg.Scheduler.IntervalMinutes)
	}
	if len(cfg.Registries) != 2 {
		t.Errorf("expected 2 registries, got %d", len(cfg.Registries))
	} else {
		if cfg.Registries[0].Host != "registry1.com" || cfg.Registries[0].Username != "user1" {
			t.Errorf("registry 0 mismatch: %+v", cfg.Registries[0])
		}
		if cfg.Registries[1].Host != "registry2.com" || cfg.Registries[1].Username != "user2" {
			t.Errorf("registry 1 mismatch: %+v", cfg.Registries[1])
		}
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("DMANAGER_SERVER_PORT", "9999")
	t.Setenv("DMANAGER_SERVER_DB_PATH", "env.db")
	t.Setenv("DMANAGER_SERVER_ALLOWED_ORIGINS", "http://env1.com,http://env2.com")
	t.Setenv("DMANAGER_DOCKER_HOST", "tcp://env:2375")
	t.Setenv("DMANAGER_SCHEDULER_INTERVAL_MINUTES", "15")
	t.Setenv("DMANAGER_REGISTRIES_0_HOST", "envreg.com")
	t.Setenv("DMANAGER_REGISTRIES_0_USERNAME", "envuser")
	t.Setenv("DMANAGER_REGISTRIES_0_PASSWORD", "envpass")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != "9999" {
		t.Errorf("expected Port 9999, got %q", cfg.Server.Port)
	}
	if cfg.Server.DBPath != "env.db" {
		t.Errorf("expected DBPath env.db, got %q", cfg.Server.DBPath)
	}
	expectedOrigins := []string{"http://env1.com", "http://env2.com"}
	if !reflect.DeepEqual(cfg.Server.AllowedOrigins, expectedOrigins) {
		t.Errorf("expected AllowedOrigins %v, got %v", expectedOrigins, cfg.Server.AllowedOrigins)
	}
	if cfg.Docker.Host != "tcp://env:2375" {
		t.Errorf("expected Docker Host tcp://env:2375, got %q", cfg.Docker.Host)
	}
	if cfg.Scheduler.IntervalMinutes != 15 {
		t.Errorf("expected IntervalMinutes 15, got %d", cfg.Scheduler.IntervalMinutes)
	}
	if len(cfg.Registries) < 1 {
		t.Fatalf("expected at least 1 registry from env override, got 0")
	}
	if cfg.Registries[0].Host != "envreg.com" || cfg.Registries[0].Username != "envuser" || cfg.Registries[0].Password != "envpass" {
		t.Errorf("registry 0 env override mismatch: %+v", cfg.Registries[0])
	}
}
