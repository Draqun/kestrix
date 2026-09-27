package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Prefix is the canonical prefix for Kestrix-owned environment variables.
const Prefix = "KESTRIX_"

const unsupportedProjectPrefix = "META" + "CI_"

// Environment is an immutable snapshot of the process environment.
//
// Kestrix-owned variables are read only from the canonical KESTRIX_* names.
// Generic and provider-owned variables retain their original names.
type Environment struct {
	ListenAddr    string
	PipelineDir   string
	WebhookSecret string
	WebhookMethod string
	// WebhookHistoryLimit bounds retained webhook history. Zero means the
	// operator did not set it and the configured default applies.
	WebhookHistoryLimit int
	DispatchProject     string
	GitLabURL           string
	GitLabToken         string
	TestDatabaseURL     string
	DatabaseURL         string
	MasterKeyPath       string
	JWTSigningKeyPath   string
	// RunnerServer, RunnerToken, and RunnerMode back `kestrix runner`
	// (tasks.md 5.1): the Kestrix server base URL to poll, the runner's own
	// bearer token, and its execution backend ("local" or "docker"). All
	// three also take a --flag on that subcommand, which wins when set.
	RunnerServer string
	RunnerToken  string
	RunnerMode   string
	// IdentityCacheTTL bounds how long RequireAuth may serve an account's
	// role and token version from cache before resolving it from the store
	// again; see design.md D4. Zero disables the cache. Defaults to 5s.
	IdentityCacheTTL time.Duration
	// LoginMaxAttempts and LoginWindow bound the login throttle: this many
	// failed logins for one account or one client address within the
	// window trip a 429; see design.md D7. Defaults to 10 and 1m.
	LoginMaxAttempts int
	LoginWindow      time.Duration
	// TrustProxyHeader, when set, makes the login throttle key an address
	// on the first hop of X-Forwarded-For instead of the raw peer address;
	// see design.md D7. Defaults to false - reading that header
	// unconditionally would let any client forge its own throttle key.
	TrustProxyHeader bool
	// ContentSecurityPolicy overrides the bundled UI's default policy on
	// every route except /docs/*; see design.md D8. Defaults to that
	// bundled-UI policy.
	ContentSecurityPolicy string
	// CookieSecure marks the session cookie Secure; see session-cookie-auth
	// design.md D4. Defaults to true - false is a plain-HTTP-development
	// downgrade for a non-loopback deployment with no TLS in front.
	CookieSecure bool
	SourceCache  SourceCacheEnvironment
	// RunnerEnvPassthrough names host environment variables forwarded to
	// task processes verbatim, in addition to the fixed base set, CI_*
	// variables, and resolved pipeline variables; see design.md D2 in the
	// runner-execution-isolation change. LoadEnvironment refuses to start
	// when this list names a control-plane secret.
	RunnerEnvPassthrough []string
	// RunnerDockerPIDs, RunnerDockerMemory, and RunnerDockerCPUs bound a
	// Docker task's process count, memory, and CPU share. RunnerDockerUser
	// and RunnerDockerNetwork set its user and network; see design.md D3.
	RunnerDockerPIDs    int
	RunnerDockerMemory  string
	RunnerDockerCPUs    string
	RunnerDockerUser    string
	RunnerDockerNetwork string
	// RunnerCancelGrace bounds how long a cancelled task's process (or
	// container) gets to exit before being killed; see design.md D4.
	// Defaults to 10s.
	RunnerCancelGrace time.Duration
	// RunnerMaxConcurrentRuns, RunHistoryLimit, and RunnerMaxOutputBytes
	// bound in-flight runs, retained run history, and stored run output; see
	// run-concurrency-and-retention design.md. Defaults to 2, 1000, and
	// 10485760.
	RunnerMaxConcurrentRuns int
	RunHistoryLimit         int
	RunnerMaxOutputBytes    int
	// HTTPReadTimeout, HTTPWriteTimeout, and HTTPIdleTimeout bound the HTTP
	// listener's connection lifetime; see webhook-ingress-hardening design.md
	// D5. Defaults to 30s, 120s, and 120s.
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	HTTPIdleTimeout  time.Duration
	// HTTPMaxBodyBytes bounds request bodies on the /api subtree.
	// WebhookMaxBodyBytes bounds POST /webhook bodies independently, since a
	// push payload carries file lists for up to twenty commits; see
	// webhook-ingress-hardening design.md D5. Defaults to 1048576 (1 MiB) and
	// 8388608 (8 MiB).
	HTTPMaxBodyBytes    int64
	WebhookMaxBodyBytes int64
	// RunnerJobMaxBodyBytes bounds a runner-job request body (the payload a
	// runner PUTs to /complete, or PATCHes to /trace) independently of both
	// bounds above, since a job payload carries a source archive; see
	// remote-runner-registration design.md D4. RunnerJobMaxConcurrentPolls
	// caps how many GET /api/runner-jobs/next long-polls one runner token
	// may hold open at once. Defaults to 20971520 (20 MiB) and 2.
	RunnerJobMaxBodyBytes       int64
	RunnerJobMaxConcurrentPolls int
	// RunnerJobTimeout and RunnerJobMaxReclaims bound silent-runner
	// recovery (tasks.md 4.1): a "claimed"/"running" job silent for longer
	// than RunnerJobTimeout is returned to "pending" for another runner, up
	// to RunnerJobMaxReclaims times, after which it is failed naming the
	// reason - see design.md D5. Defaults to 120s and 3.
	RunnerJobTimeout     time.Duration
	RunnerJobMaxReclaims int
	// RunnerOfflineAfter is how long a runner holding no job may go without
	// a request before GET /api/runners reports it offline
	// (remote-runner-management). Defaults to 60s, three long-poll
	// intervals.
	RunnerOfflineAfter time.Duration
	// MetricsEnabled, MetricsToken, and LogLevel back the observability
	// settings; see observability design.md. MetricsEnabled defaults to
	// false, MetricsToken has no default (empty means unconfigured), and
	// LogLevel is left empty here - config.go's DefaultConfig supplies the
	// "info" default and validates the four accepted values once file, env,
	// and default are reconciled.
	MetricsEnabled bool
	MetricsToken   string
	LogLevel       string

	values        map[string]string
	originalEnvs  []string
	validationErr error
}

// SourceCacheEnvironment is the typed canonical environment representation.
type SourceCacheEnvironment struct {
	Directory string
	MaxAge    time.Duration
	MaxBytes  int64
}

var (
	environmentOnce sync.Once
	environmentLive *Environment
)

// parsePositiveIntEnv reads KESTRIX_<key> from values and parses it as a
// positive integer, returning def when the variable is unset. The returned
// error names the canonical variable, ready to become validationErr - see
// REVIEW.md R16, which asked for this instead of a fourth/fifth/sixth
// hand-copied Atoi-and-reject-non-positive block.
func parsePositiveIntEnv(values map[string]string, key string, def int) (int, error) {
	raw := values[Prefix+key]
	if raw == "" {
		return def, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return def, fmt.Errorf("%s%s must be a positive integer", Prefix, key)
	}
	return parsed, nil
}

// LoadEnvironment creates a deterministic, immutable snapshot from environ.
// It is pure and is intended for unit tests as well as process initialization.
func LoadEnvironment(environ []string) *Environment {
	raw := make(map[string]string, len(environ))
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		raw[name] = value
	}

	values := make(map[string]string, len(raw))
	for name, value := range raw {
		if strings.HasPrefix(name, unsupportedProjectPrefix) {
			continue
		}
		values[name] = value
	}
	normalized := make([]string, 0, len(values))
	for name, value := range values {
		normalized = append(normalized, name+"="+value)
	}
	sort.Strings(normalized)

	var validationErr error
	var maxAge time.Duration
	if raw := values[Prefix+"SOURCE_CACHE_MAX_AGE"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			validationErr = fmt.Errorf("KESTRIX_SOURCE_CACHE_MAX_AGE must be a positive duration")
		} else {
			maxAge = parsed
		}
	}
	var maxBytes int64
	if raw := values[Prefix+"SOURCE_CACHE_MAX_BYTES"]; raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_SOURCE_CACHE_MAX_BYTES must be a positive integer")
			}
		} else {
			maxBytes = parsed
		}
	}

	identityCacheTTL := 5 * time.Second
	if raw := values[Prefix+"IDENTITY_CACHE_TTL"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_IDENTITY_CACHE_TTL must be a non-negative duration")
			}
		} else {
			identityCacheTTL = parsed
		}
	}

	loginMaxAttempts := 10
	if raw := values[Prefix+"LOGIN_MAX_ATTEMPTS"]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_LOGIN_MAX_ATTEMPTS must be a positive integer")
			}
		} else {
			loginMaxAttempts = parsed
		}
	}

	loginWindow := time.Minute
	if raw := values[Prefix+"LOGIN_WINDOW"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_LOGIN_WINDOW must be a positive duration")
			}
		} else {
			loginWindow = parsed
		}
	}

	var trustProxyHeader bool
	if raw := values[Prefix+"TRUST_PROXY_HEADER"]; raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_TRUST_PROXY_HEADER must be a boolean")
			}
		} else {
			trustProxyHeader = parsed
		}
	}

	cookieSecure := true
	if raw := values[Prefix+"COOKIE_SECURE"]; raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_COOKIE_SECURE must be a boolean")
			}
		} else {
			cookieSecure = parsed
		}
	}

	var historyLimit int
	if raw := values[Prefix+"WEBHOOK_HISTORY_LIMIT"]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_WEBHOOK_HISTORY_LIMIT must be a positive integer")
			}
		} else {
			historyLimit = parsed
		}
	}

	deniedPassthrough := map[string]bool{
		Prefix + "MASTER_KEY_PATH":      true,
		Prefix + "JWT_SIGNING_KEY_PATH": true,
		"DATABASE_URL":                  true,
		Prefix + "GITLAB_TOKEN":         true,
		Prefix + "WEBHOOK_SECRET":       true,
	}
	var runnerEnvPassthrough []string
	if raw := values[Prefix+"RUNNER_ENV_PASSTHROUGH"]; raw != "" {
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if deniedPassthrough[name] {
				if validationErr == nil {
					validationErr = fmt.Errorf("KESTRIX_RUNNER_ENV_PASSTHROUGH must not include %s", name)
				}
				continue
			}
			runnerEnvPassthrough = append(runnerEnvPassthrough, name)
		}
	}

	runnerDockerPIDs := 256
	if raw := values[Prefix+"RUNNER_DOCKER_PIDS"]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_RUNNER_DOCKER_PIDS must be a positive integer")
			}
		} else {
			runnerDockerPIDs = parsed
		}
	}

	runnerDockerMemory := "2g"
	if raw := values[Prefix+"RUNNER_DOCKER_MEMORY"]; raw != "" {
		runnerDockerMemory = raw
	}

	runnerDockerCPUs := "2"
	if raw := values[Prefix+"RUNNER_DOCKER_CPUS"]; raw != "" {
		runnerDockerCPUs = raw
	}

	runnerDockerNetwork := "bridge"
	if raw := values[Prefix+"RUNNER_DOCKER_NETWORK"]; raw != "" {
		runnerDockerNetwork = raw
	}

	runnerCancelGrace := 10 * time.Second
	if raw := values[Prefix+"RUNNER_CANCEL_GRACE"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_RUNNER_CANCEL_GRACE must be a non-negative duration")
			}
		} else {
			runnerCancelGrace = parsed
		}
	}

	runnerMaxConcurrentRuns, err := parsePositiveIntEnv(values, "RUNNER_MAX_CONCURRENT_RUNS", 2)
	if err != nil && validationErr == nil {
		validationErr = err
	}

	runHistoryLimit, err := parsePositiveIntEnv(values, "RUN_HISTORY_LIMIT", 1000)
	if err != nil && validationErr == nil {
		validationErr = err
	}

	runnerMaxOutputBytes, err := parsePositiveIntEnv(values, "RUNNER_MAX_OUTPUT_BYTES", 10485760)
	if err != nil && validationErr == nil {
		validationErr = err
	}

	httpReadTimeout := 30 * time.Second
	if raw := values[Prefix+"HTTP_READ_TIMEOUT"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_HTTP_READ_TIMEOUT must be a positive duration")
			}
		} else {
			httpReadTimeout = parsed
		}
	}

	httpWriteTimeout := 120 * time.Second
	if raw := values[Prefix+"HTTP_WRITE_TIMEOUT"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_HTTP_WRITE_TIMEOUT must be a positive duration")
			}
		} else {
			httpWriteTimeout = parsed
		}
	}

	httpIdleTimeout := 120 * time.Second
	if raw := values[Prefix+"HTTP_IDLE_TIMEOUT"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_HTTP_IDLE_TIMEOUT must be a positive duration")
			}
		} else {
			httpIdleTimeout = parsed
		}
	}

	httpMaxBodyBytes := int64(1048576)
	if raw := values[Prefix+"HTTP_MAX_BODY_BYTES"]; raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_HTTP_MAX_BODY_BYTES must be a positive integer")
			}
		} else {
			httpMaxBodyBytes = parsed
		}
	}

	webhookMaxBodyBytes := int64(8388608)
	if raw := values[Prefix+"WEBHOOK_MAX_BODY_BYTES"]; raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_WEBHOOK_MAX_BODY_BYTES must be a positive integer")
			}
		} else {
			webhookMaxBodyBytes = parsed
		}
	}

	runnerJobMaxBodyBytes := int64(20971520)
	if raw := values[Prefix+"RUNNER_JOB_MAX_BODY_BYTES"]; raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_RUNNER_JOB_MAX_BODY_BYTES must be a positive integer")
			}
		} else {
			runnerJobMaxBodyBytes = parsed
		}
	}

	runnerJobMaxConcurrentPolls := 2
	if raw := values[Prefix+"RUNNER_JOB_MAX_CONCURRENT_POLLS"]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_RUNNER_JOB_MAX_CONCURRENT_POLLS must be a positive integer")
			}
		} else {
			runnerJobMaxConcurrentPolls = parsed
		}
	}

	runnerJobTimeout := 120 * time.Second
	if raw := values[Prefix+"RUNNER_JOB_TIMEOUT"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_RUNNER_JOB_TIMEOUT must be a positive duration")
			}
		} else {
			runnerJobTimeout = parsed
		}
	}

	runnerOfflineAfter := 60 * time.Second
	if raw := values[Prefix+"RUNNER_OFFLINE_AFTER"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_RUNNER_OFFLINE_AFTER must be a positive duration")
			}
		} else {
			runnerOfflineAfter = parsed
		}
	}

	runnerJobMaxReclaims := 3
	if raw := values[Prefix+"RUNNER_JOB_MAX_RECLAIMS"]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_RUNNER_JOB_MAX_RECLAIMS must be a positive integer")
			}
		} else {
			runnerJobMaxReclaims = parsed
		}
	}

	var metricsEnabled bool
	if raw := values[Prefix+"METRICS_ENABLED"]; raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			if validationErr == nil {
				validationErr = fmt.Errorf("KESTRIX_METRICS_ENABLED must be a boolean")
			}
		} else {
			metricsEnabled = parsed
		}
	}

	// logLevel defaults to "info" here, like the other string settings with
	// a baked-in default (runnerDockerMemory, runnerDockerCPUs, ...), so a
	// Config never touched by DefaultConfig still resolves to a valid level
	// through ApplyEnvironment alone.
	logLevel := DefaultLogLevel
	if raw := values[Prefix+"LOG_LEVEL"]; raw != "" {
		logLevel = raw
	}

	return &Environment{
		ListenAddr:            values[Prefix+"ADDR"],
		PipelineDir:           values[Prefix+"DIR"],
		WebhookSecret:         values[Prefix+"WEBHOOK_SECRET"],
		WebhookMethod:         values[Prefix+"WEBHOOK_METHOD"],
		WebhookHistoryLimit:   historyLimit,
		DispatchProject:       values[Prefix+"DISPATCH_PROJECT"],
		GitLabURL:             values[Prefix+"GITLAB_URL"],
		GitLabToken:           values[Prefix+"GITLAB_TOKEN"],
		RunnerServer:          values[Prefix+"RUNNER_SERVER"],
		RunnerToken:           values[Prefix+"RUNNER_TOKEN"],
		RunnerMode:            values[Prefix+"RUNNER_MODE"],
		TestDatabaseURL:       values[Prefix+"TEST_DATABASE_URL"],
		DatabaseURL:           values["DATABASE_URL"],
		MasterKeyPath:         values[Prefix+"MASTER_KEY_PATH"],
		JWTSigningKeyPath:     values[Prefix+"JWT_SIGNING_KEY_PATH"],
		IdentityCacheTTL:      identityCacheTTL,
		LoginMaxAttempts:      loginMaxAttempts,
		LoginWindow:           loginWindow,
		TrustProxyHeader:      trustProxyHeader,
		ContentSecurityPolicy: values[Prefix+"CONTENT_SECURITY_POLICY"],
		CookieSecure:          cookieSecure,
		SourceCache: SourceCacheEnvironment{
			Directory: values[Prefix+"SOURCE_CACHE_DIR"],
			MaxAge:    maxAge,
			MaxBytes:  maxBytes,
		},
		RunnerEnvPassthrough:        runnerEnvPassthrough,
		RunnerDockerPIDs:            runnerDockerPIDs,
		RunnerDockerMemory:          runnerDockerMemory,
		RunnerDockerCPUs:            runnerDockerCPUs,
		RunnerDockerUser:            values[Prefix+"RUNNER_DOCKER_USER"],
		RunnerDockerNetwork:         runnerDockerNetwork,
		RunnerCancelGrace:           runnerCancelGrace,
		RunnerMaxConcurrentRuns:     runnerMaxConcurrentRuns,
		RunHistoryLimit:             runHistoryLimit,
		RunnerMaxOutputBytes:        runnerMaxOutputBytes,
		HTTPReadTimeout:             httpReadTimeout,
		HTTPWriteTimeout:            httpWriteTimeout,
		HTTPIdleTimeout:             httpIdleTimeout,
		HTTPMaxBodyBytes:            httpMaxBodyBytes,
		WebhookMaxBodyBytes:         webhookMaxBodyBytes,
		RunnerJobMaxBodyBytes:       runnerJobMaxBodyBytes,
		RunnerJobMaxConcurrentPolls: runnerJobMaxConcurrentPolls,
		RunnerJobTimeout:            runnerJobTimeout,
		RunnerJobMaxReclaims:        runnerJobMaxReclaims,
		RunnerOfflineAfter:          runnerOfflineAfter,
		MetricsEnabled:              metricsEnabled,
		MetricsToken:                values[Prefix+"METRICS_TOKEN"],
		LogLevel:                    logLevel,
		values:                      values,
		originalEnvs:                normalized,
		validationErr:               validationErr,
	}
}

// ValidationError returns malformed canonical runtime settings without
// exposing their values.
func (e *Environment) ValidationError() error {
	if e == nil {
		return nil
	}
	return e.validationErr
}

// Initialize loads the process environment once and returns the live snapshot.
func Initialize() *Environment {
	environmentOnce.Do(func() {
		environmentLive = LoadEnvironment(os.Environ())
	})
	return environmentLive
}

// Current returns the process environment snapshot, initializing it lazily if
// the executable entry point has not initialized it explicitly yet.
func Current() *Environment {
	return Initialize()
}

// WithRunnerConfig returns a copy of the snapshot with the six runner
// settings taken from the reconciled Config.Runner. The receiver is not
// mutated; shared maps stay shared because snapshots are immutable.
func (e *Environment) WithRunnerConfig(c *Config) *Environment {
	derived := *e
	derived.RunnerDockerPIDs = c.Runner.DockerPIDs
	derived.RunnerDockerMemory = c.Runner.DockerMemory
	derived.RunnerDockerCPUs = c.Runner.DockerCPUs
	derived.RunnerDockerUser = c.Runner.DockerUser
	derived.RunnerDockerNetwork = c.Runner.DockerNetwork
	derived.RunnerCancelGrace = time.Duration(c.Runner.CancelGrace)
	return &derived
}

// ReplaceCurrent installs env as the process snapshot and returns the
// previous snapshot so tests can restore it via t.Cleanup. It MUST be called
// after Initialize (a call made first would be overwritten by the sync.Once
// body) and only once, during startup while still single-threaded.
func ReplaceCurrent(env *Environment) *Environment {
	previous := environmentLive
	environmentLive = env
	return previous
}

// Prefixed returns environment entries matching prefix, sorted by
// variable name. It is used when Kestrix forwards variables to child jobs.
func (e *Environment) Prefixed(prefix string) []string {
	entries := make([]string, 0)
	for name, value := range e.values {
		if strings.HasPrefix(name, prefix) {
			entries = append(entries, name+"="+value)
		}
	}
	sort.Strings(entries)
	return entries
}

// Environ returns a normalized copy suitable for child processes.
func (e *Environment) Environ() []string {
	return append([]string(nil), e.originalEnvs...)
}

// Lookup returns a value from the immutable environment snapshot.
func (e *Environment) Lookup(name string) (string, bool) {
	value, ok := e.values[name]
	return value, ok
}
