// Package config provides YAML-based configuration for Kestrix.
//
// Config file: ~/.kestrix/config.yaml (auto-discovered)
// CLI flags override file values.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var (
	marshalYAML = yaml.Marshal
	userHomeDir = os.UserHomeDir
	mkdirAll    = os.MkdirAll
	readFile    = os.ReadFile
	writeFile   = os.WriteFile
)

// Config is the top-level configuration.
type Config struct {
	ListenAddr  string         `yaml:"listen_addr"`
	DatabaseURL string         `yaml:"database_url"`
	Branch      string         `yaml:"default_branch"`
	Pipeline    PipelineConfig `yaml:"pipeline"`
	Webhook     WebhookConfig  `yaml:"webhook"`
	Dispatch    DispatchConfig `yaml:"dispatch"`
	Docker      DockerConfig   `yaml:"docker"`
	Runner      RunnerConfig   `yaml:"runner"`
}

// RunnerConfig controls how pipeline jobs are executed.
type RunnerConfig struct {
	// Mode selects the execution backend: "local" (direct sh -c) or "gitlab-runner" (gitlab-runner exec).
	Mode string `yaml:"mode"`
	// Executor is the gitlab-runner executor to use (shell, docker, etc.). Only used with mode=gitlab-runner.
	Executor string `yaml:"executor"`
}

// DockerConfig holds Docker registry and deployment settings.
type DockerConfig struct {
	Registry string `yaml:"registry"`
	Image    string `yaml:"image"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
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
		Runner: RunnerConfig{
			Mode:     "local",
			Executor: "shell",
		},
	}
}

// DefaultDir returns ~/.kestrix, creating it if needed.
func DefaultDir() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".kestrix")
	if err := mkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// DefaultPath returns ~/.kestrix/config.yaml.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".kestrix", "config.yaml")
}

// Load reads config from the given YAML file. Missing file → defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := readFile(path)
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
	if err := mkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	data, err := marshalYAML(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return writeFile(path, data, 0o600)
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
