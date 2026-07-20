package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.Branch != "main" {
		t.Errorf("Branch = %q", cfg.Branch)
	}
	if cfg.Dispatch.GitLabURL != "https://gitlab.com" {
		t.Errorf("GitLabURL = %q", cfg.Dispatch.GitLabURL)
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("expected defaults, got ListenAddr=%q", cfg.ListenAddr)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &Config{
		ListenAddr: ":9090",
		Branch:     "develop",
		Pipeline:   PipelineConfig{Dir: "/app/pipelines"},
		Webhook:    WebhookConfig{Secret: "my-secret"},
		Dispatch: DispatchConfig{
			Enabled:     true,
			ProjectID:   12345,
			GitLabURL:   "https://gitlab.example.com",
			GitLabToken: "glpat-xxx",
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want :9090", loaded.ListenAddr)
	}
	if loaded.Branch != "develop" {
		t.Errorf("Branch = %q, want develop", loaded.Branch)
	}
	if loaded.Pipeline.Dir != "/app/pipelines" {
		t.Errorf("Pipeline.Dir = %q", loaded.Pipeline.Dir)
	}
	if loaded.Webhook.Secret != "my-secret" {
		t.Errorf("Webhook.Secret = %q", loaded.Webhook.Secret)
	}
	if !loaded.Dispatch.Enabled {
		t.Error("Dispatch.Enabled = false, want true")
	}
	if loaded.Dispatch.ProjectID != 12345 {
		t.Errorf("Dispatch.ProjectID = %d, want 12345", loaded.Dispatch.ProjectID)
	}
	if loaded.Dispatch.GitLabURL != "https://gitlab.example.com" {
		t.Errorf("Dispatch.GitLabURL = %q", loaded.Dispatch.GitLabURL)
	}
	if loaded.Dispatch.GitLabToken != "glpat-xxx" {
		t.Errorf("Dispatch.GitLabToken = %q", loaded.Dispatch.GitLabToken)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("{{invalid yaml"), 0o600)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestApplyFlags(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ApplyFlags(":3000", "/opt/pipelines", "release", "glpat-yep", true, 99999)

	if cfg.ListenAddr != ":3000" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.Pipeline.Dir != "/opt/pipelines" {
		t.Errorf("Pipeline.Dir = %q", cfg.Pipeline.Dir)
	}
	if cfg.Branch != "release" {
		t.Errorf("Branch = %q", cfg.Branch)
	}
	if cfg.Dispatch.GitLabToken != "glpat-yep" {
		t.Errorf("GitLabToken = %q", cfg.Dispatch.GitLabToken)
	}
	if !cfg.Dispatch.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Dispatch.ProjectID != 99999 {
		t.Errorf("ProjectID = %d", cfg.Dispatch.ProjectID)
	}
}

func TestApplyFlagsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ApplyFlags("", "", "", "", false, 0)

	if cfg.ListenAddr != ":8080" {
		t.Errorf("empty ApplyFlags changed ListenAddr to %q", cfg.ListenAddr)
	}
	if cfg.Branch != "main" {
		t.Errorf("empty ApplyFlags changed Branch to %q", cfg.Branch)
	}
}
