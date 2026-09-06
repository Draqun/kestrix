// Package config provides YAML-based configuration for Kestrix.
//
// Config file: ~/.kestrix/config.yaml (auto-discovered)
// CLI flags override file values.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	marshalYAML   = yaml.Marshal
	unmarshalYAML = yaml.Unmarshal
	userHomeDir   = os.UserHomeDir
	mkdirAll      = os.MkdirAll
	readFile      = os.ReadFile
	writeFile     = os.WriteFile
	statPath      = os.Stat
	chmodPath     = os.Chmod
)

// Config is the top-level configuration.
type Config struct {
	ListenAddr  string            `yaml:"listen_addr"`
	DatabaseURL string            `yaml:"database_url"`
	Branch      string            `yaml:"default_branch"`
	Pipeline    PipelineConfig    `yaml:"pipeline"`
	Webhook     WebhookConfig     `yaml:"webhook"`
	Dispatch    DispatchConfig    `yaml:"dispatch"`
	Docker      DockerConfig      `yaml:"docker"`
	Runner      RunnerConfig      `yaml:"runner"`
	SourceCache SourceCacheConfig `yaml:"source_cache"`

	sourceCacheDirectorySet bool
	sourceCacheMaxAgeSet    bool
	sourceCacheMaxBytesSet  bool
}

// Duration is a YAML duration encoded with Go duration syntax, for example
// "24h". It keeps runtime configuration typed after parsing.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// SourceCacheConfig controls persistent Git object caching. Session workspaces
// remain temporary and are not stored below this directory.
type SourceCacheConfig struct {
	Directory string   `yaml:"directory"`
	MaxAge    Duration `yaml:"max_age"`
	MaxBytes  int64    `yaml:"max_bytes"`
}

// Validate ensures the cache has bounded positive retention and a private
// service-account directory.
func (c SourceCacheConfig) Validate() error {
	if c.Directory == "" {
		return fmt.Errorf("source cache directory is required")
	}
	if time.Duration(c.MaxAge) <= 0 {
		return fmt.Errorf("source cache max_age must be positive")
	}
	if c.MaxBytes <= 0 {
		return fmt.Errorf("source cache max_bytes must be positive")
	}
	if err := mkdirAll(c.Directory, 0o700); err != nil {
		return fmt.Errorf("create source cache directory: %w", err)
	}
	info, err := statPath(c.Directory)
	if err != nil {
		return fmt.Errorf("inspect source cache directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source cache path is not a directory")
	}
	if err := chmodPath(c.Directory, 0o700); err != nil {
		return fmt.Errorf("secure source cache directory: %w", err)
	}
	return nil
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

// WebhookConfig controls webhook authentication and history retention.
type WebhookConfig struct {
	Secret string `yaml:"secret"`
	// HistoryLimit is the maximum number of webhook events kept in history.
	// Older events are discarded once the limit is exceeded.
	HistoryLimit int `yaml:"history_limit"`
}

// DefaultWebhookHistoryLimit is the number of webhook events retained when no
// limit is configured.
const DefaultWebhookHistoryLimit = 1000

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
		Webhook: WebhookConfig{
			HistoryLimit: DefaultWebhookHistoryLimit,
		},
		Dispatch: DispatchConfig{
			GitLabURL: "https://gitlab.com",
		},
		Runner: RunnerConfig{
			Mode:     "local",
			Executor: "shell",
		},
		SourceCache: SourceCacheConfig{
			Directory: filepath.Join(filepath.Dir(DefaultPath()), "cache", "git"),
			MaxAge:    Duration(30 * 24 * time.Hour),
			MaxBytes:  10 << 30,
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

	if err := unmarshalYAML(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	var presence struct {
		SourceCache *struct {
			Directory *string   `yaml:"directory"`
			MaxAge    *Duration `yaml:"max_age"`
			MaxBytes  *int64    `yaml:"max_bytes"`
		} `yaml:"source_cache"`
	}
	if err := unmarshalYAML(data, &presence); err != nil {
		return nil, fmt.Errorf("parse config presence: %w", err)
	}
	if presence.SourceCache != nil {
		cfg.sourceCacheDirectorySet = presence.SourceCache.Directory != nil
		cfg.sourceCacheMaxAgeSet = presence.SourceCache.MaxAge != nil
		cfg.sourceCacheMaxBytesSet = presence.SourceCache.MaxBytes != nil
	}
	if cfg.Webhook.HistoryLimit <= 0 {
		cfg.Webhook.HistoryLimit = DefaultWebhookHistoryLimit
	}

	return cfg, nil
}

// ApplyWebhookEnvironment applies the canonical webhook history limit from the
// environment. The environment wins over the config file so operators can tune
// retention per deployment without editing a mounted file.
func (c *Config) ApplyWebhookEnvironment(env *Environment) error {
	if env == nil {
		return nil
	}
	if err := env.ValidationError(); err != nil {
		return err
	}
	if env.WebhookHistoryLimit > 0 {
		c.Webhook.HistoryLimit = env.WebhookHistoryLimit
	}
	if c.Webhook.HistoryLimit <= 0 {
		c.Webhook.HistoryLimit = DefaultWebhookHistoryLimit
	}
	return nil
}

// ApplySourceCacheEnvironment applies canonical environment values only where
// the config file did not explicitly provide the corresponding setting.
func (c *Config) ApplySourceCacheEnvironment(env *Environment) error {
	if env == nil {
		return nil
	}
	if err := env.ValidationError(); err != nil {
		return err
	}
	if !c.sourceCacheDirectorySet && env.SourceCache.Directory != "" {
		c.SourceCache.Directory = env.SourceCache.Directory
	}
	if !c.sourceCacheMaxAgeSet && env.SourceCache.MaxAge > 0 {
		c.SourceCache.MaxAge = Duration(env.SourceCache.MaxAge)
	}
	if !c.sourceCacheMaxBytesSet && env.SourceCache.MaxBytes > 0 {
		c.SourceCache.MaxBytes = env.SourceCache.MaxBytes
	}
	return nil
}

// ApplySourceCacheFlags applies non-zero explicit CLI values last.
func (c *Config) ApplySourceCacheFlags(directory string, maxAge time.Duration, maxBytes int64) {
	if directory != "" {
		c.SourceCache.Directory = directory
	}
	if maxAge != 0 {
		c.SourceCache.MaxAge = Duration(maxAge)
	}
	if maxBytes != 0 {
		c.SourceCache.MaxBytes = maxBytes
	}
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
