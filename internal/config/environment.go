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
	Docker                DockerEnvironment
	SourceCache           SourceCacheEnvironment

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

// DockerEnvironment contains Docker settings read from the process
// environment. Its fields are immutable after startup.
type DockerEnvironment struct {
	Registry string
	Image    string
	Username string
	Password string
}

var (
	environmentOnce sync.Once
	environmentLive *Environment
)

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

	return &Environment{
		ListenAddr:            values[Prefix+"ADDR"],
		PipelineDir:           values[Prefix+"DIR"],
		WebhookSecret:         values[Prefix+"WEBHOOK_SECRET"],
		WebhookMethod:         values[Prefix+"WEBHOOK_METHOD"],
		WebhookHistoryLimit:   historyLimit,
		DispatchProject:       values[Prefix+"DISPATCH_PROJECT"],
		GitLabURL:             values[Prefix+"GITLAB_URL"],
		GitLabToken:           values[Prefix+"GITLAB_TOKEN"],
		TestDatabaseURL:       values[Prefix+"TEST_DATABASE_URL"],
		DatabaseURL:           values["DATABASE_URL"],
		MasterKeyPath:         values[Prefix+"MASTER_KEY_PATH"],
		JWTSigningKeyPath:     values[Prefix+"JWT_SIGNING_KEY_PATH"],
		IdentityCacheTTL:      identityCacheTTL,
		LoginMaxAttempts:      loginMaxAttempts,
		LoginWindow:           loginWindow,
		TrustProxyHeader:      trustProxyHeader,
		ContentSecurityPolicy: values[Prefix+"CONTENT_SECURITY_POLICY"],
		Docker: DockerEnvironment{
			Registry: values["DOCKER_REGISTRY"],
			Image:    values["DOCKER_IMAGE"],
			Username: values["DOCKER_USERNAME"],
			Password: values["DOCKER_PASSWORD"],
		},
		SourceCache: SourceCacheEnvironment{
			Directory: values[Prefix+"SOURCE_CACHE_DIR"],
			MaxAge:    maxAge,
			MaxBytes:  maxBytes,
		},
		values:        values,
		originalEnvs:  normalized,
		validationErr: validationErr,
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
