// Package config provides YAML-based configuration for Kestrix.
//
// Config file: ~/.kestrix/config.yaml (auto-discovered)
// CLI flags override file values.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"kestrix/internal/domain"
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
	Runner      RunnerConfig      `yaml:"runner"`
	SourceCache SourceCacheConfig `yaml:"source_cache"`
	Metrics     MetricsConfig     `yaml:"metrics"`
	Logging     LoggingConfig     `yaml:"logging"`

	// presence records which multi-form settings (multiFormSettingKeys) the
	// config file explicitly set, even to an empty/zero value. Only Load
	// writes it; ApplyEnvironment only reads it via isSet.
	presence map[string]bool

	sources map[string]string
}

func (c *Config) isSet(key string) bool {
	return c.presence[key]
}

func (c *Config) markSet(key string) {
	if c.presence == nil {
		c.presence = make(map[string]bool, len(multiFormSettingKeys))
	}
	c.presence[key] = true
}

// Multi-form setting keys: named constants so a typo at a call site is a
// compile error (undefined identifier) instead of a silently-ignored string
// mismatch. Exported so other packages (e.g. cmd/kestrix's precedence table)
// reference the same identifiers instead of hand-authoring the key strings.
// multiFormSettingKeys itself stays unexported: nothing outside this package
// needs the full ordered list, and exporting a mutable slice would let an
// importer's stray append/write corrupt it for every Config in the process.
const (
	KeyListenAddr              = "listen_addr"
	KeyDatabaseURL             = "database_url"
	KeyDefaultBranch           = "default_branch"
	KeyPipelineDir             = "pipeline.dir"
	KeyWebhookSecret           = "webhook.secret"
	KeyWebhookHistoryLimit     = "webhook.history_limit"
	KeyDispatchProjectID       = "dispatch.project_id"
	KeyDispatchGitLabURL       = "dispatch.gitlab_url"
	KeyDispatchGitLabToken     = "dispatch.gitlab_token"
	KeySourceCacheDirectory    = "source_cache.directory"
	KeySourceCacheMaxAge       = "source_cache.max_age"
	KeySourceCacheMaxBytes     = "source_cache.max_bytes"
	KeyRunnerDockerPIDs        = "runner.docker_pids"
	KeyRunnerDockerMemory      = "runner.docker_memory"
	KeyRunnerDockerCPUs        = "runner.docker_cpus"
	KeyRunnerDockerUser        = "runner.docker_user"
	KeyRunnerDockerNetwork     = "runner.docker_network"
	KeyRunnerCancelGrace       = "runner.cancel_grace"
	KeyRunnerMaxConcurrentRuns = "runner.max_concurrent_runs"
	KeyRunHistoryLimit         = "runner.run_history_limit"
	KeyRunnerMaxOutputBytes    = "runner.max_output_bytes"
	KeyMetricsEnabled          = "metrics.enabled"
	KeyMetricsToken            = "metrics.token"
	KeyLoggingLevel            = "logging.level"
)

// multiFormSettingKeys lists every setting with more than one configuration
// form, in SourceReport order. One list feeds DefaultConfig, SourceReport,
// and the count assertions so adding a setting touches a single literal.
var multiFormSettingKeys = []string{
	KeyListenAddr, KeyDatabaseURL, KeyDefaultBranch, KeyPipelineDir,
	KeyWebhookSecret, KeyWebhookHistoryLimit,
	KeyDispatchProjectID, KeyDispatchGitLabURL, KeyDispatchGitLabToken,
	KeySourceCacheDirectory, KeySourceCacheMaxAge, KeySourceCacheMaxBytes,
	KeyRunnerDockerPIDs, KeyRunnerDockerMemory, KeyRunnerDockerCPUs,
	KeyRunnerDockerUser, KeyRunnerDockerNetwork, KeyRunnerCancelGrace,
	KeyRunnerMaxConcurrentRuns, KeyRunHistoryLimit, KeyRunnerMaxOutputBytes,
	KeyMetricsEnabled, KeyMetricsToken, KeyLoggingLevel,
}

// SourceReport names which source won for each multi-form setting without
// printing any value.
func (c *Config) SourceReport() []string {
	out := make([]string, 0, len(multiFormSettingKeys))
	for _, k := range multiFormSettingKeys {
		src := "default"
		if c.sources != nil {
			if s, ok := c.sources[k]; ok && s != "" {
				src = s
			}
		}
		out = append(out, k+" source="+src)
	}
	return out
}

func (c *Config) setSource(key, src string) {
	if c.sources == nil {
		c.sources = make(map[string]string, len(multiFormSettingKeys))
	}
	c.sources[key] = src
}

// FlagValues carries parsed CLI flag values for ApplyFlags. ApplyFlags
// applies only non-zero entries, so a flag wins over the file and the
// environment. Today only the `serve` subcommand populates it.
type FlagValues struct {
	ListenAddr    string
	PipelineDir   string
	Branch        string
	WebhookSecret string
	// DispatchProjectRaw holds the raw --dispatch value; ApplyFlags parses
	// it via parseDispatchProject, so only a positive integer enables
	// dispatch. The Raw suffix separates it from the reconciled int
	// Config.Dispatch.ProjectID.
	DispatchProjectRaw  string
	GitLabURL           string
	GitLabToken         string
	DatabaseURL         string
	SourceCacheDir      string
	SourceCacheMaxAge   time.Duration
	SourceCacheMaxBytes int64
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

// Validate rejects a runner.mode Kestrix cannot execute.
func (c RunnerConfig) Validate() error {
	if domain.IsValidMode(c.Mode) {
		return nil
	}
	return fmt.Errorf("runner.mode %q is not supported: use one of %s", c.Mode, strings.Join(domain.Modes(), ", "))
}

// RunnerConfig controls how pipeline jobs are executed.
type RunnerConfig struct {
	// Mode is the instance's default execution backend - "local" (bash on
	// the host, the default when unset), "docker", or "remote" - which every
	// project without a mode of its own takes.
	Mode string `yaml:"mode"`
	// DockerPIDs, DockerMemory, and DockerCPUs bound a Docker task's
	// process count, memory, and CPU share. DockerUser and DockerNetwork
	// set its user and network. See task-execution-isolation.
	DockerPIDs    int    `yaml:"docker_pids"`
	DockerMemory  string `yaml:"docker_memory"`
	DockerCPUs    string `yaml:"docker_cpus"`
	DockerUser    string `yaml:"docker_user"`
	DockerNetwork string `yaml:"docker_network"`
	// CancelGrace bounds how long a cancelled task gets to exit before
	// being killed.
	CancelGrace Duration `yaml:"cancel_grace"`
	// MaxConcurrentRuns bounds how many runs execute at once; a run accepted
	// beyond it is queued. RunHistoryLimit bounds how many terminal runs are
	// retained. MaxOutputBytes bounds stored run output. See
	// run-concurrency-and-retention design.md.
	MaxConcurrentRuns int `yaml:"max_concurrent_runs"`
	RunHistoryLimit   int `yaml:"run_history_limit"`
	MaxOutputBytes    int `yaml:"max_output_bytes"`
}

// PipelineConfig controls pipeline parsing behavior.
type PipelineConfig struct {
	Dir string `yaml:"dir"`
}

// MetricsConfig controls the GET /metrics endpoint; see observability
// spec.md. Enabled defaults to false - the endpoint does not exist at all
// until an operator opts in. Token is a secret: never echoed by
// SourceReport, a log line, or any API response.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// LoggingConfig controls the structured process log; see observability
// spec.md. Level must be one of debug, info, warn, error.
type LoggingConfig struct {
	Level string `yaml:"level"`
}

// DefaultLogLevel is the logging.level applied when neither the config file
// nor KESTRIX_LOG_LEVEL set one.
const DefaultLogLevel = "info"

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

// Defaults for the run-concurrency-and-retention settings; see design.md D5.
const (
	DefaultMaxConcurrentRuns = 2
	DefaultRunHistoryLimit   = 1000
	DefaultMaxOutputBytes    = 10485760
)

// parseDispatchProject strictly parses a --dispatch / KESTRIX_DISPATCH_PROJECT
// value: only a positive decimal integer enables dispatch. Prefixes like
// "222evil", zero, negatives, and non-numeric input are rejected so a typo
// can never silently enable outbound dispatch.
func parseDispatchProject(raw string) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
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
	cfg := &Config{
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
			Mode:              "local",
			DockerPIDs:        256,
			DockerMemory:      "2g",
			DockerCPUs:        "2",
			DockerNetwork:     "bridge",
			CancelGrace:       Duration(10 * time.Second),
			MaxConcurrentRuns: DefaultMaxConcurrentRuns,
			RunHistoryLimit:   DefaultRunHistoryLimit,
			MaxOutputBytes:    DefaultMaxOutputBytes,
		},
		SourceCache: SourceCacheConfig{
			Directory: filepath.Join(filepath.Dir(DefaultPath()), "cache", "git"),
			MaxAge:    Duration(30 * 24 * time.Hour),
			MaxBytes:  10 << 30,
		},
		Logging: LoggingConfig{
			Level: DefaultLogLevel,
		},
		sources: make(map[string]string, len(multiFormSettingKeys)),
	}
	for _, k := range multiFormSettingKeys {
		cfg.sources[k] = "default"
	}
	return cfg
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

// decodeStrict unmarshals YAML with unknown keys rejected. The returned
// error names the key but never echoes a value from the file.
func decodeStrict(data []byte, target any, path string) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return nil
}

// withRemovedKeyHint appends a migration pointer when startup fails on a key
// Kestrix used to accept but removed. Any other unknown key fails with just
// the key name and file path, never a value from the file.
func withRemovedKeyHint(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "field docker not found"):
		return fmt.Errorf("%w; the `docker:` block was removed: registry credentials reach tasks only via KESTRIX_RUNNER_ENV_PASSTHROUGH", err)
	case strings.Contains(msg, "field executor not found"):
		return fmt.Errorf("%w; `runner.executor` was removed and had no effect", err)
	default:
		return err
	}
}

// Load reads config from the given YAML file. Missing file → defaults.
// Unknown keys fail startup naming the key and file path, without echoing
// any value from the file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := decodeStrict(data, cfg, path); err != nil {
		return nil, withRemovedKeyHint(err)
	}
	// explicit decodes the same file a second time with pointer fields, so a
	// present-but-empty/zero YAML key is distinguishable from an absent one.
	// The markFile call sites below read explicit's fields and feed the
	// result into Config.presence via markSet. explicit and Config.presence
	// are named differently because they're different things - a decode-time
	// snapshot versus the map it populates - not because they're unrelated.
	var explicit struct {
		ListenAddr  *string `yaml:"listen_addr"`
		DatabaseURL *string `yaml:"database_url"`
		Branch      *string `yaml:"default_branch"`
		Pipeline    *struct {
			Dir *string `yaml:"dir"`
		} `yaml:"pipeline"`
		Webhook *struct {
			Secret       *string `yaml:"secret"`
			HistoryLimit *int    `yaml:"history_limit"`
		} `yaml:"webhook"`
		Dispatch *struct {
			ProjectID   *int    `yaml:"project_id"`
			GitLabURL   *string `yaml:"gitlab_url"`
			GitLabToken *string `yaml:"gitlab_token"`
		} `yaml:"dispatch"`
		SourceCache *struct {
			Directory *string   `yaml:"directory"`
			MaxAge    *Duration `yaml:"max_age"`
			MaxBytes  *int64    `yaml:"max_bytes"`
		} `yaml:"source_cache"`
		Runner *struct {
			DockerPIDs        *int      `yaml:"docker_pids"`
			DockerMemory      *string   `yaml:"docker_memory"`
			DockerCPUs        *string   `yaml:"docker_cpus"`
			DockerUser        *string   `yaml:"docker_user"`
			DockerNetwork     *string   `yaml:"docker_network"`
			CancelGrace       *Duration `yaml:"cancel_grace"`
			MaxConcurrentRuns *int      `yaml:"max_concurrent_runs"`
			RunHistoryLimit   *int      `yaml:"run_history_limit"`
			MaxOutputBytes    *int      `yaml:"max_output_bytes"`
		} `yaml:"runner"`
		Metrics *struct {
			Enabled *bool   `yaml:"enabled"`
			Token   *string `yaml:"token"`
		} `yaml:"metrics"`
		Logging *struct {
			Level *string `yaml:"level"`
		} `yaml:"logging"`
	}
	// Presence tracks only multi-form keys; other known keys (enabled,
	// mode, default_branch siblings) are ignored here, so this decode
	// stays non-strict while the main decode above rejects unknown keys.
	if err := unmarshalYAML(data, &explicit); err != nil {
		return nil, fmt.Errorf("decode explicit config keys in %s: %w", path, err)
	}
	// markFile records a key as file-set and its winning source in one step;
	// presence and sources always agree for file-set keys.
	markFile := func(key string, set bool) {
		if set {
			cfg.markSet(key)
			cfg.setSource(key, "file")
		}
	}
	markFile(KeyListenAddr, explicit.ListenAddr != nil)
	markFile(KeyDatabaseURL, explicit.DatabaseURL != nil)
	markFile(KeyDefaultBranch, explicit.Branch != nil)
	if explicit.Pipeline != nil {
		markFile(KeyPipelineDir, explicit.Pipeline.Dir != nil)
	}
	if explicit.Webhook != nil {
		markFile(KeyWebhookSecret, explicit.Webhook.Secret != nil)
		markFile(KeyWebhookHistoryLimit, explicit.Webhook.HistoryLimit != nil)
	}
	if explicit.Dispatch != nil {
		markFile(KeyDispatchProjectID, explicit.Dispatch.ProjectID != nil)
		markFile(KeyDispatchGitLabURL, explicit.Dispatch.GitLabURL != nil)
		markFile(KeyDispatchGitLabToken, explicit.Dispatch.GitLabToken != nil)
	}
	if explicit.SourceCache != nil {
		markFile(KeySourceCacheDirectory, explicit.SourceCache.Directory != nil)
		markFile(KeySourceCacheMaxAge, explicit.SourceCache.MaxAge != nil)
		markFile(KeySourceCacheMaxBytes, explicit.SourceCache.MaxBytes != nil)
	}
	if explicit.Runner != nil {
		markFile(KeyRunnerDockerPIDs, explicit.Runner.DockerPIDs != nil)
		markFile(KeyRunnerDockerMemory, explicit.Runner.DockerMemory != nil)
		markFile(KeyRunnerDockerCPUs, explicit.Runner.DockerCPUs != nil)
		markFile(KeyRunnerDockerUser, explicit.Runner.DockerUser != nil)
		markFile(KeyRunnerDockerNetwork, explicit.Runner.DockerNetwork != nil)
		markFile(KeyRunnerCancelGrace, explicit.Runner.CancelGrace != nil)
		markFile(KeyRunnerMaxConcurrentRuns, explicit.Runner.MaxConcurrentRuns != nil)
		markFile(KeyRunHistoryLimit, explicit.Runner.RunHistoryLimit != nil)
		markFile(KeyRunnerMaxOutputBytes, explicit.Runner.MaxOutputBytes != nil)
	}
	if explicit.Metrics != nil {
		markFile(KeyMetricsEnabled, explicit.Metrics.Enabled != nil)
		markFile(KeyMetricsToken, explicit.Metrics.Token != nil)
	}
	if explicit.Logging != nil {
		markFile(KeyLoggingLevel, explicit.Logging.Level != nil)
	}
	cfg.normalizeWebhookHistoryLimit()

	return cfg, nil
}

// normalizeWebhookHistoryLimit resets a non-positive history limit to the
// default. Load and ApplyEnvironment both call it: Load must self-normalize
// when called standalone, and ApplyEnvironment may still leave a non-positive
// value if neither the file nor the environment set one.
func (c *Config) normalizeWebhookHistoryLimit() {
	if c.Webhook.HistoryLimit <= 0 {
		c.Webhook.HistoryLimit = DefaultWebhookHistoryLimit
	}
}

// ApplyEnvironment fills only keys the config file did not set from the
// canonical environment. The file wins over the environment; flags win over
// both via ApplyFlags. An explicitly empty file value counts as set and is
// never filled from the environment.
func (c *Config) ApplyEnvironment(env *Environment) error {
	if env == nil {
		return nil
	}
	if err := env.ValidationError(); err != nil {
		return err
	}
	// Validate dispatch first: it is the only field here that can still fail
	// after this point, so validating it before any other field is mutated
	// keeps this function atomic - either everything applies or c is
	// untouched.
	if !c.isSet(KeyDispatchProjectID) && env.DispatchProject != "" {
		projectID, ok := parseDispatchProject(env.DispatchProject)
		if !ok {
			return fmt.Errorf("KESTRIX_DISPATCH_PROJECT must be a positive integer")
		}
		c.Dispatch.ProjectID = projectID
		c.Dispatch.Enabled = true
		c.setSource(KeyDispatchProjectID, "env")
	}
	if !c.isSet(KeyListenAddr) && env.ListenAddr != "" {
		c.ListenAddr = env.ListenAddr
		c.setSource(KeyListenAddr, "env")
	}
	if !c.isSet(KeyDatabaseURL) && env.DatabaseURL != "" {
		c.DatabaseURL = env.DatabaseURL
		c.setSource(KeyDatabaseURL, "env")
	}
	if !c.isSet(KeyPipelineDir) && env.PipelineDir != "" {
		c.Pipeline.Dir = env.PipelineDir
		c.setSource(KeyPipelineDir, "env")
	}
	if !c.isSet(KeyWebhookSecret) && env.WebhookSecret != "" {
		c.Webhook.Secret = env.WebhookSecret
		c.setSource(KeyWebhookSecret, "env")
	}
	if !c.isSet(KeyWebhookHistoryLimit) && env.WebhookHistoryLimit > 0 {
		c.Webhook.HistoryLimit = env.WebhookHistoryLimit
		c.setSource(KeyWebhookHistoryLimit, "env")
	}
	if !c.isSet(KeyDispatchGitLabURL) && env.GitLabURL != "" {
		c.Dispatch.GitLabURL = env.GitLabURL
		c.setSource(KeyDispatchGitLabURL, "env")
	}
	if !c.isSet(KeyDispatchGitLabToken) && env.GitLabToken != "" {
		c.Dispatch.GitLabToken = env.GitLabToken
		c.setSource(KeyDispatchGitLabToken, "env")
	}
	if !c.isSet(KeySourceCacheDirectory) && env.SourceCache.Directory != "" {
		c.SourceCache.Directory = env.SourceCache.Directory
		c.setSource(KeySourceCacheDirectory, "env")
	}
	if !c.isSet(KeySourceCacheMaxAge) && env.SourceCache.MaxAge > 0 {
		c.SourceCache.MaxAge = Duration(env.SourceCache.MaxAge)
		c.setSource(KeySourceCacheMaxAge, "env")
	}
	if !c.isSet(KeySourceCacheMaxBytes) && env.SourceCache.MaxBytes > 0 {
		c.SourceCache.MaxBytes = env.SourceCache.MaxBytes
		c.setSource(KeySourceCacheMaxBytes, "env")
	}
	if !c.isSet(KeyRunnerDockerPIDs) && env.RunnerDockerPIDs > 0 {
		c.Runner.DockerPIDs = env.RunnerDockerPIDs
		c.setSource(KeyRunnerDockerPIDs, "env")
	}
	if !c.isSet(KeyRunnerDockerMemory) && env.RunnerDockerMemory != "" {
		c.Runner.DockerMemory = env.RunnerDockerMemory
		c.setSource(KeyRunnerDockerMemory, "env")
	}
	if !c.isSet(KeyRunnerDockerCPUs) && env.RunnerDockerCPUs != "" {
		c.Runner.DockerCPUs = env.RunnerDockerCPUs
		c.setSource(KeyRunnerDockerCPUs, "env")
	}
	if !c.isSet(KeyRunnerDockerUser) && env.RunnerDockerUser != "" {
		c.Runner.DockerUser = env.RunnerDockerUser
		c.setSource(KeyRunnerDockerUser, "env")
	}
	if !c.isSet(KeyRunnerDockerNetwork) && env.RunnerDockerNetwork != "" {
		c.Runner.DockerNetwork = env.RunnerDockerNetwork
		c.setSource(KeyRunnerDockerNetwork, "env")
	}
	// No env.RunnerCancelGrace != zero guard, unlike the other branches: 0 is
	// a legitimate parsed value here (KESTRIX_RUNNER_CANCEL_GRACE=0s means an
	// explicit no-grace override; environment.go only rejects parsed < 0), so
	// zero can't serve as the "env var unset" sentinel the other branches use.
	// This branch therefore always applies env.RunnerCancelGrace when the
	// file didn't set it - which is either an explicit override or
	// environment.go's own 10s default - and marks the source "env" even
	// when the process environment never set the variable at all.
	if !c.isSet(KeyRunnerCancelGrace) {
		c.Runner.CancelGrace = Duration(env.RunnerCancelGrace)
		c.setSource(KeyRunnerCancelGrace, "env")
	}
	if !c.isSet(KeyRunnerMaxConcurrentRuns) && env.RunnerMaxConcurrentRuns > 0 {
		c.Runner.MaxConcurrentRuns = env.RunnerMaxConcurrentRuns
		c.setSource(KeyRunnerMaxConcurrentRuns, "env")
	}
	if !c.isSet(KeyRunHistoryLimit) && env.RunHistoryLimit > 0 {
		c.Runner.RunHistoryLimit = env.RunHistoryLimit
		c.setSource(KeyRunHistoryLimit, "env")
	}
	if !c.isSet(KeyRunnerMaxOutputBytes) && env.RunnerMaxOutputBytes > 0 {
		c.Runner.MaxOutputBytes = env.RunnerMaxOutputBytes
		c.setSource(KeyRunnerMaxOutputBytes, "env")
	}
	// KESTRIX_METRICS_ENABLED has no non-empty-string sentinel the way most
	// other env branches do: env.MetricsEnabled is already the parsed bool
	// (false when unset or explicitly false), so this branch, like
	// KeyRunnerCancelGrace above, always applies when the file did not set
	// the key - either an explicit env override or the false zero value -
	// and marks the source "env" even when the variable was never set.
	if !c.isSet(KeyMetricsEnabled) {
		c.Metrics.Enabled = env.MetricsEnabled
		c.setSource(KeyMetricsEnabled, "env")
	}
	if !c.isSet(KeyMetricsToken) && env.MetricsToken != "" {
		c.Metrics.Token = env.MetricsToken
		c.setSource(KeyMetricsToken, "env")
	}
	// env.LogLevel is never empty (LoadEnvironment defaults it to
	// DefaultLogLevel), so - like KeyRunnerCancelGrace and KeyMetricsEnabled
	// above - this always applies when the file did not set the key.
	if !c.isSet(KeyLoggingLevel) {
		c.Logging.Level = env.LogLevel
		c.setSource(KeyLoggingLevel, "env")
	}
	c.normalizeWebhookHistoryLimit()
	if err := c.validateRunnerBounds(); err != nil {
		return err
	}
	return c.validateObservabilitySettings()
}

// validateObservabilitySettings fails startup naming logging.level when it
// is not one of the four accepted values - see observability spec.md
// "Unknown level fails startup". metrics.enabled cannot itself be invalid
// here: LoadEnvironment already rejects a malformed KESTRIX_METRICS_ENABLED
// before ApplyEnvironment runs, and a YAML metrics.enabled that is not a
// bool fails the strict decode in Load.
func (c *Config) validateObservabilitySettings() error {
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("logging.level must be one of debug, info, warn, error")
	}
}

// validateRunnerBounds fails startup naming the first invalid
// run-concurrency-and-retention setting, without revealing the other two -
// see runtime-configuration spec.md "Invalid value". Unlike
// normalizeWebhookHistoryLimit, these settings fail rather than silently
// reset to their default.
func (c *Config) validateRunnerBounds() error {
	if c.Runner.MaxConcurrentRuns <= 0 {
		return fmt.Errorf("runner.max_concurrent_runs must be a positive integer")
	}
	if c.Runner.RunHistoryLimit <= 0 {
		return fmt.Errorf("runner.run_history_limit must be a positive integer")
	}
	if c.Runner.MaxOutputBytes <= 0 {
		return fmt.Errorf("runner.max_output_bytes must be a positive integer")
	}
	return nil
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

// ApplyFlags overrides config values with non-zero CLI flags. Flags win
// over both the file and the environment. An invalid flag value fails
// startup with an error instead of being silently ignored.
func (c *Config) ApplyFlags(f FlagValues) error {
	// Validate dispatch first: it is the only field here that can still fail
	// after this point, so validating it before any other field is mutated
	// keeps this function atomic - either everything applies or c is
	// untouched.
	if f.DispatchProjectRaw != "" {
		projectID, ok := parseDispatchProject(f.DispatchProjectRaw)
		if !ok {
			return fmt.Errorf("--dispatch must be a positive integer")
		}
		c.Dispatch.ProjectID = projectID
		c.Dispatch.Enabled = true
		c.setSource(KeyDispatchProjectID, "flag")
	}
	if f.ListenAddr != "" {
		c.ListenAddr = f.ListenAddr
		c.setSource(KeyListenAddr, "flag")
	}
	if f.PipelineDir != "" {
		c.Pipeline.Dir = f.PipelineDir
		c.setSource(KeyPipelineDir, "flag")
	}
	if f.Branch != "" {
		c.Branch = f.Branch
		c.setSource(KeyDefaultBranch, "flag")
	}
	if f.WebhookSecret != "" {
		c.Webhook.Secret = f.WebhookSecret
		c.setSource(KeyWebhookSecret, "flag")
	}
	if f.GitLabURL != "" {
		c.Dispatch.GitLabURL = f.GitLabURL
		c.setSource(KeyDispatchGitLabURL, "flag")
	}
	if f.GitLabToken != "" {
		c.Dispatch.GitLabToken = f.GitLabToken
		c.setSource(KeyDispatchGitLabToken, "flag")
	}
	if f.DatabaseURL != "" {
		c.DatabaseURL = f.DatabaseURL
		c.setSource(KeyDatabaseURL, "flag")
	}
	if f.SourceCacheDir != "" {
		c.SourceCache.Directory = f.SourceCacheDir
		c.setSource(KeySourceCacheDirectory, "flag")
	}
	if f.SourceCacheMaxAge != 0 {
		c.SourceCache.MaxAge = Duration(f.SourceCacheMaxAge)
		c.setSource(KeySourceCacheMaxAge, "flag")
	}
	if f.SourceCacheMaxBytes != 0 {
		c.SourceCache.MaxBytes = f.SourceCacheMaxBytes
		c.setSource(KeySourceCacheMaxBytes, "flag")
	}
	return nil
}
