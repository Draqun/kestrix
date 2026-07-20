// Package config provides YAML-based configuration for MetaCI.
//
// Config file: ~/.metaci/config.yaml (auto-discovered)
// CLI flags override file values.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration.
type Config struct {
	ListenAddr string         `yaml:"listen_addr"`
	Branch     string         `yaml:"default_branch"`
	Pipeline   PipelineConfig `yaml:"pipeline"`
	Webhook    WebhookConfig  `yaml:"webhook"`
	Dispatch   DispatchConfig `yaml:"dispatch"`
}

// PipelineConfig controls pipeline parsing behavior.
type PipelineConfig struct {
	Dir string `yaml:"dir"`
}

// WebhookConfig controls webhook authentication.
type WebhookConfig struct {
	Secret string `yaml:"secret"`
}

// DispatchConfig controls GitLab pipeline dispatch.
type DispatchConfig struct {
	Enabled     bool   `yaml:"enabled"`
	ProjectID   int    `yaml:"project_id"`
	GitLabURL   string `yaml:"gitlab_url"`
	GitLabToken string `yaml:"gitlab_token"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ListenAddr: ":8080",
		Branch:     "main",
		Pipeline: PipelineConfig{
			Dir: ".",
		},
		Dispatch: DispatchConfig{
			GitLabURL: "https://gitlab.com",
		},
	}
}

// DefaultDir returns ~/.metaci, creating it if needed.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".metaci")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// DefaultPath returns ~/.metaci/config.yaml.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".metaci", "config.yaml")
}

// Load reads config from the given YAML file. Missing file → defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// Save writes the config to the given path.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}

// ApplyFlags overrides config values with non-empty CLI flags.
func (c *Config) ApplyFlags(listenAddr, pipelineDir, branch, gitlabToken string, dispatchEnabled bool, projectID int) {
	if listenAddr != "" {
		c.ListenAddr = listenAddr
	}
	if pipelineDir != "" {
		c.Pipeline.Dir = pipelineDir
	}
	if branch != "" {
		c.Branch = branch
	}
	if gitlabToken != "" {
		c.Dispatch.GitLabToken = gitlabToken
	}
	if dispatchEnabled {
		c.Dispatch.Enabled = true
	}
	if projectID > 0 {
		c.Dispatch.ProjectID = projectID
	}
}
