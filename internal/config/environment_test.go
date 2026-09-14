package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadEnvironmentUsesCanonicalNamesOnly(t *testing.T) {
	env := LoadEnvironment([]string{
		"KESTRIX_WEBHOOK_SECRET=canonical-secret",
		"KESTRIX_MASTER_KEY_PATH=/run/secrets/kestrix_master_key",
		"DATABASE_URL=postgres://example",
		"UNRELATED=value",
	})

	if env.WebhookSecret != "canonical-secret" {
		t.Fatalf("typed webhook secret = %q", env.WebhookSecret)
	}
	if env.MasterKeyPath != "/run/secrets/kestrix_master_key" {
		t.Fatalf("typed master-key path = %q", env.MasterKeyPath)
	}
	if env.ListenAddr != "" {
		t.Fatalf("unexpected address from legacy environment: %q", env.ListenAddr)
	}
	if got := env.Prefixed(Prefix); !reflect.DeepEqual(got, []string{"KESTRIX_MASTER_KEY_PATH=/run/secrets/kestrix_master_key", "KESTRIX_WEBHOOK_SECRET=canonical-secret"}) {
		t.Fatalf("prefixed values = %#v", got)
	}
}

func TestLoadEnvironmentAllowsEmptyMasterKeyPathForStartupValidation(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_MASTER_KEY_PATH="})
	if env.MasterKeyPath != "" {
		t.Fatalf("master-key path = %q, want empty", env.MasterKeyPath)
	}
}

func TestLoadEnvironmentReadsJWTSigningKeyPath(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_JWT_SIGNING_KEY_PATH=/run/secrets/kestrix_jwt_key"})
	if env.JWTSigningKeyPath != "/run/secrets/kestrix_jwt_key" {
		t.Fatalf("typed JWT signing-key path = %q", env.JWTSigningKeyPath)
	}
}

func TestLoadEnvironmentAllowsEmptyJWTSigningKeyPathForStartupValidation(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_JWT_SIGNING_KEY_PATH="})
	if env.JWTSigningKeyPath != "" {
		t.Fatalf("JWT signing-key path = %q, want empty", env.JWTSigningKeyPath)
	}
}

func TestLoadEnvironmentIdentityCacheTTLDefault(t *testing.T) {
	env := LoadEnvironment(nil)
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.IdentityCacheTTL != 5*time.Second {
		t.Fatalf("IdentityCacheTTL = %v, want 5s default", env.IdentityCacheTTL)
	}
}

func TestLoadEnvironmentIdentityCacheTTLExplicitValue(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_IDENTITY_CACHE_TTL=30s"})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.IdentityCacheTTL != 30*time.Second {
		t.Fatalf("IdentityCacheTTL = %v, want 30s", env.IdentityCacheTTL)
	}
}

func TestLoadEnvironmentIdentityCacheTTLZeroDisablesCache(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_IDENTITY_CACHE_TTL=0s"})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.IdentityCacheTTL != 0 {
		t.Fatalf("IdentityCacheTTL = %v, want 0 (explicit disable)", env.IdentityCacheTTL)
	}
}

func TestLoadEnvironmentIdentityCacheTTLRejectsNegative(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_IDENTITY_CACHE_TTL=-5s"})
	if err := env.ValidationError(); err == nil {
		t.Fatal("ValidationError() = nil, want error for a negative TTL")
	}
}

func TestLoadEnvironmentLoginThrottleDefaults(t *testing.T) {
	env := LoadEnvironment(nil)
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.LoginMaxAttempts != 10 {
		t.Fatalf("LoginMaxAttempts = %d, want 10 default", env.LoginMaxAttempts)
	}
	if env.LoginWindow != time.Minute {
		t.Fatalf("LoginWindow = %v, want 1m default", env.LoginWindow)
	}
	if env.TrustProxyHeader {
		t.Fatal("TrustProxyHeader = true, want false default")
	}
}

func TestLoadEnvironmentLoginThrottleExplicitValues(t *testing.T) {
	env := LoadEnvironment([]string{
		"KESTRIX_LOGIN_MAX_ATTEMPTS=3",
		"KESTRIX_LOGIN_WINDOW=30s",
		"KESTRIX_TRUST_PROXY_HEADER=true",
	})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.LoginMaxAttempts != 3 {
		t.Fatalf("LoginMaxAttempts = %d, want 3", env.LoginMaxAttempts)
	}
	if env.LoginWindow != 30*time.Second {
		t.Fatalf("LoginWindow = %v, want 30s", env.LoginWindow)
	}
	if !env.TrustProxyHeader {
		t.Fatal("TrustProxyHeader = false, want true")
	}
}

func TestLoadEnvironmentLoginMaxAttemptsRejectsNonPositive(t *testing.T) {
	for _, raw := range []string{"0", "-1", "not-a-number"} {
		env := LoadEnvironment([]string{"KESTRIX_LOGIN_MAX_ATTEMPTS=" + raw})
		if err := env.ValidationError(); err == nil {
			t.Fatalf("KESTRIX_LOGIN_MAX_ATTEMPTS=%q: ValidationError() = nil, want error", raw)
		}
	}
}

func TestLoadEnvironmentLoginWindowRejectsNonPositive(t *testing.T) {
	for _, raw := range []string{"0s", "-5s", "not-a-duration"} {
		env := LoadEnvironment([]string{"KESTRIX_LOGIN_WINDOW=" + raw})
		if err := env.ValidationError(); err == nil {
			t.Fatalf("KESTRIX_LOGIN_WINDOW=%q: ValidationError() = nil, want error", raw)
		}
	}
}

func TestLoadEnvironmentTrustProxyHeaderRejectsNonBoolean(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_TRUST_PROXY_HEADER=maybe"})
	if err := env.ValidationError(); err == nil {
		t.Fatal("ValidationError() = nil, want error for a non-boolean value")
	}
}

func TestLoadEnvironmentContentSecurityPolicyDefaultsEmpty(t *testing.T) {
	// Environment carries the raw operator value only; internal/web applies
	// the bundled-UI default when this is empty - see design.md D8.
	env := LoadEnvironment(nil)
	if env.ContentSecurityPolicy != "" {
		t.Fatalf("ContentSecurityPolicy = %q, want empty (default resolved downstream)", env.ContentSecurityPolicy)
	}
}

func TestLoadEnvironmentContentSecurityPolicyExplicitValueReplacesDefault(t *testing.T) {
	const custom = "default-src 'none'"
	env := LoadEnvironment([]string{"KESTRIX_CONTENT_SECURITY_POLICY=" + custom})
	if env.ContentSecurityPolicy != custom {
		t.Fatalf("ContentSecurityPolicy = %q, want %q", env.ContentSecurityPolicy, custom)
	}
}

func TestLoadEnvironmentCookieSecureDefaultsTrue(t *testing.T) {
	env := LoadEnvironment(nil)
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if !env.CookieSecure {
		t.Fatal("CookieSecure = false, want true default")
	}
}

func TestLoadEnvironmentCookieSecureExplicitFalse(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_COOKIE_SECURE=false"})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.CookieSecure {
		t.Fatal("CookieSecure = true, want false")
	}
}

func TestLoadEnvironmentCookieSecureRejectsNonBoolean(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_COOKIE_SECURE=maybe"})
	if err := env.ValidationError(); err == nil {
		t.Fatal("ValidationError() = nil, want error for a non-boolean value")
	}
}

func TestLoadEnvironmentParsesTypedSourceCacheSettings(t *testing.T) {
	env := LoadEnvironment([]string{
		"KESTRIX_SOURCE_CACHE_DIR=/var/cache/kestrix",
		"KESTRIX_SOURCE_CACHE_MAX_AGE=36h",
		"KESTRIX_SOURCE_CACHE_MAX_BYTES=4096",
	})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.SourceCache.Directory != "/var/cache/kestrix" || env.SourceCache.MaxAge != 36*time.Hour || env.SourceCache.MaxBytes != 4096 {
		t.Fatalf("source cache = %+v", env.SourceCache)
	}
}

func TestLoadEnvironmentRejectsInvalidSourceCacheSettings(t *testing.T) {
	for _, entry := range []string{
		"KESTRIX_SOURCE_CACHE_MAX_AGE=tomorrow",
		"KESTRIX_SOURCE_CACHE_MAX_BYTES=many",
		"KESTRIX_SOURCE_CACHE_MAX_AGE=-1h",
		"KESTRIX_SOURCE_CACHE_MAX_BYTES=-1",
	} {
		env := LoadEnvironment([]string{entry})
		if err := env.ValidationError(); err == nil {
			t.Fatalf("expected validation error for %q", entry)
		}
	}
}

func TestLoadEnvironmentPreservesGenericVariables(t *testing.T) {
	env := LoadEnvironment([]string{
		"KESTRIX_ADDR=:9090",
		"DOCKER_IMAGE=kestrix:test",
		"EMPTY=",
	})

	if env.ListenAddr != ":9090" {
		t.Fatalf("address = %q", env.ListenAddr)
	}
	// DOCKER_* has no typed field: it reaches tasks only through
	// KESTRIX_RUNNER_ENV_PASSTHROUGH, but the raw value stays in the snapshot.
	if got, ok := env.Lookup("DOCKER_IMAGE"); !ok || got != "kestrix:test" {
		t.Fatalf("DOCKER_IMAGE lookup = %q, %v", got, ok)
	}
	if got := env.Prefixed(Prefix); !reflect.DeepEqual(got, []string{"KESTRIX_ADDR=:9090"}) {
		t.Fatalf("prefixed values = %#v", got)
	}
}

func TestEnvironmentEnvironIsIndependent(t *testing.T) {
	legacyName := "META" + "CI_ADDR"
	env := LoadEnvironment([]string{legacyName + "=:7070", "KESTRIX_ADDR=:8080", "KESTRIX_MODE=local"})
	got := env.Environ()
	got[0] = "changed"

	want := []string{"KESTRIX_ADDR=:8080", "KESTRIX_MODE=local"}
	if actual := env.Environ(); !reflect.DeepEqual(actual, want) {
		t.Fatalf("environ = %#v, want %#v", actual, want)
	}
}

func TestInitializeReturnsTheSameSnapshot(t *testing.T) {
	first := Initialize()
	if second := Initialize(); second != first {
		t.Fatal("Initialize returned different snapshots")
	}
	if Current() != first {
		t.Fatal("Current returned a different snapshot")
	}
}

func TestLoadEnvironmentSkipsMalformedEntries(t *testing.T) {
	env := LoadEnvironment([]string{"not-an-environment-entry", "=missing-name", "KESTRIX_ADDR=:8080"})
	if env.ListenAddr != ":8080" {
		t.Fatalf("address = %q", env.ListenAddr)
	}
}

func TestEnvironmentPrefixed(t *testing.T) {
	e := LoadEnvironment([]string{"KESTRIX_ONE=1", "KESTRIX_TWO=2", "OTHER=3"})
	got := e.Prefixed("KESTRIX_")
	if len(got) != 2 || got[0] != "KESTRIX_ONE=1" || got[1] != "KESTRIX_TWO=2" {
		t.Fatalf("prefixed=%v", got)
	}
	if value, ok := e.Lookup("KESTRIX_ONE"); !ok || value != "1" {
		t.Fatalf("lookup=%q ok=%v", value, ok)
	}
}

func TestLoadEnvironmentRunnerEnvPassthroughEmpty(t *testing.T) {
	env := LoadEnvironment(nil)
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if len(env.RunnerEnvPassthrough) != 0 {
		t.Fatalf("RunnerEnvPassthrough = %v, want empty", env.RunnerEnvPassthrough)
	}
}

func TestLoadEnvironmentRunnerEnvPassthroughValidList(t *testing.T) {
	env := LoadEnvironment([]string{
		"KESTRIX_RUNNER_ENV_PASSTHROUGH= DOCKER_REGISTRY, DEPLOY_HOST ,,DOCKER_USERNAME",
	})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	want := []string{"DOCKER_REGISTRY", "DEPLOY_HOST", "DOCKER_USERNAME"}
	if !reflect.DeepEqual(env.RunnerEnvPassthrough, want) {
		t.Fatalf("RunnerEnvPassthrough = %v, want %v", env.RunnerEnvPassthrough, want)
	}
}

func TestLoadEnvironmentRunnerEnvPassthroughRejectsDeniedNames(t *testing.T) {
	for _, denied := range []string{
		"KESTRIX_MASTER_KEY_PATH",
		"KESTRIX_JWT_SIGNING_KEY_PATH",
		"DATABASE_URL",
		"KESTRIX_GITLAB_TOKEN",
		"KESTRIX_WEBHOOK_SECRET",
	} {
		env := LoadEnvironment([]string{"KESTRIX_RUNNER_ENV_PASSTHROUGH=" + denied})
		err := env.ValidationError()
		if err == nil {
			t.Fatalf("%s: ValidationError() = nil, want error", denied)
		}
		if !strings.Contains(err.Error(), denied) {
			t.Errorf("%s: error = %v, want it to name the variable", denied, err)
		}
		if strings.Contains(err.Error(), "=") {
			t.Errorf("%s: error = %v, must not expose a value", denied, err)
		}
	}
}

func TestLoadEnvironmentRunnerDockerDefaults(t *testing.T) {
	env := LoadEnvironment(nil)
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.RunnerDockerPIDs != 256 {
		t.Errorf("RunnerDockerPIDs = %d, want 256", env.RunnerDockerPIDs)
	}
	if env.RunnerDockerMemory != "2g" {
		t.Errorf("RunnerDockerMemory = %q, want 2g", env.RunnerDockerMemory)
	}
	if env.RunnerDockerCPUs != "2" {
		t.Errorf("RunnerDockerCPUs = %q, want 2", env.RunnerDockerCPUs)
	}
	if env.RunnerDockerUser != "" {
		t.Errorf("RunnerDockerUser = %q, want empty", env.RunnerDockerUser)
	}
	if env.RunnerDockerNetwork != "bridge" {
		t.Errorf("RunnerDockerNetwork = %q, want bridge", env.RunnerDockerNetwork)
	}
	if env.RunnerCancelGrace != 10*time.Second {
		t.Errorf("RunnerCancelGrace = %v, want 10s", env.RunnerCancelGrace)
	}
}

func TestLoadEnvironmentRunnerDockerExplicitValues(t *testing.T) {
	env := LoadEnvironment([]string{
		"KESTRIX_RUNNER_DOCKER_PIDS=64",
		"KESTRIX_RUNNER_DOCKER_MEMORY=512m",
		"KESTRIX_RUNNER_DOCKER_CPUS=1.5",
		"KESTRIX_RUNNER_DOCKER_USER=1000:1000",
		"KESTRIX_RUNNER_DOCKER_NETWORK=none",
		"KESTRIX_RUNNER_CANCEL_GRACE=30s",
	})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.RunnerDockerPIDs != 64 {
		t.Errorf("RunnerDockerPIDs = %d, want 64", env.RunnerDockerPIDs)
	}
	if env.RunnerDockerMemory != "512m" {
		t.Errorf("RunnerDockerMemory = %q, want 512m", env.RunnerDockerMemory)
	}
	if env.RunnerDockerCPUs != "1.5" {
		t.Errorf("RunnerDockerCPUs = %q, want 1.5", env.RunnerDockerCPUs)
	}
	if env.RunnerDockerUser != "1000:1000" {
		t.Errorf("RunnerDockerUser = %q, want 1000:1000", env.RunnerDockerUser)
	}
	if env.RunnerDockerNetwork != "none" {
		t.Errorf("RunnerDockerNetwork = %q, want none", env.RunnerDockerNetwork)
	}
	if env.RunnerCancelGrace != 30*time.Second {
		t.Errorf("RunnerCancelGrace = %v, want 30s", env.RunnerCancelGrace)
	}
}

func TestLoadEnvironmentRunnerDockerPIDsRejectsNonPositive(t *testing.T) {
	for _, raw := range []string{"0", "-1", "not-a-number"} {
		env := LoadEnvironment([]string{"KESTRIX_RUNNER_DOCKER_PIDS=" + raw})
		if err := env.ValidationError(); err == nil {
			t.Fatalf("KESTRIX_RUNNER_DOCKER_PIDS=%q: ValidationError() = nil, want error", raw)
		}
	}
}

func TestLoadEnvironmentRunnerCancelGraceRejectsNegative(t *testing.T) {
	for _, raw := range []string{"-1s", "not-a-duration"} {
		env := LoadEnvironment([]string{"KESTRIX_RUNNER_CANCEL_GRACE=" + raw})
		if err := env.ValidationError(); err == nil {
			t.Fatalf("KESTRIX_RUNNER_CANCEL_GRACE=%q: ValidationError() = nil, want error", raw)
		}
	}
}

func TestLoadEnvironmentRunnerCancelGraceAllowsZero(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_RUNNER_CANCEL_GRACE=0s"})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}
	if env.RunnerCancelGrace != 0 {
		t.Fatalf("RunnerCancelGrace = %v, want 0", env.RunnerCancelGrace)
	}
}

func TestNilEnvironmentValidationIsValid(t *testing.T) {
	var env *Environment
	if err := env.ValidationError(); err != nil {
		t.Fatalf("nil environment validation = %v", err)
	}
}

// TestLoadEnvironmentHTTPDurationSettings covers each of the three
// KESTRIX_HTTP_*_TIMEOUT settings: default, explicit value, 0, negative, and
// an unparseable string. See webhook-ingress-hardening design.md D5.
func TestLoadEnvironmentHTTPDurationSettings(t *testing.T) {
	cases := []struct {
		name         string
		want         time.Duration
		envGetter    func(*Environment) time.Duration
		defaultValue time.Duration
	}{
		{"KESTRIX_HTTP_READ_TIMEOUT", 30 * time.Second, func(e *Environment) time.Duration { return e.HTTPReadTimeout }, 30 * time.Second},
		{"KESTRIX_HTTP_WRITE_TIMEOUT", 120 * time.Second, func(e *Environment) time.Duration { return e.HTTPWriteTimeout }, 120 * time.Second},
		{"KESTRIX_HTTP_IDLE_TIMEOUT", 120 * time.Second, func(e *Environment) time.Duration { return e.HTTPIdleTimeout }, 120 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/default", func(t *testing.T) {
			env := LoadEnvironment(nil)
			if err := env.ValidationError(); err != nil {
				t.Fatal(err)
			}
			if got := tc.envGetter(env); got != tc.defaultValue {
				t.Fatalf("%s default = %v, want %v", tc.name, got, tc.defaultValue)
			}
		})
		t.Run(tc.name+"/explicit", func(t *testing.T) {
			env := LoadEnvironment([]string{tc.name + "=45s"})
			if err := env.ValidationError(); err != nil {
				t.Fatal(err)
			}
			if got := tc.envGetter(env); got != 45*time.Second {
				t.Fatalf("%s = %v, want 45s", tc.name, got)
			}
		})
		for _, bad := range []string{"0s", "-5s", "not-a-duration"} {
			t.Run(tc.name+"/"+bad, func(t *testing.T) {
				env := LoadEnvironment([]string{tc.name + "=" + bad})
				if err := env.ValidationError(); err == nil {
					t.Fatalf("%s=%q: ValidationError() = nil, want error naming the setting", tc.name, bad)
				} else if !strings.Contains(err.Error(), tc.name) {
					t.Fatalf("%s=%q: error = %v, want it to name %s", tc.name, bad, err, tc.name)
				}
			})
		}
	}
}

// TestLoadEnvironmentHTTPBodyByteSettings covers each of the two
// KESTRIX_*_MAX_BODY_BYTES settings: default, explicit value, 0, negative,
// and garbage. See webhook-ingress-hardening design.md D5.
func TestLoadEnvironmentHTTPBodyByteSettings(t *testing.T) {
	cases := []struct {
		name         string
		getter       func(*Environment) int64
		defaultValue int64
	}{
		{"KESTRIX_HTTP_MAX_BODY_BYTES", func(e *Environment) int64 { return e.HTTPMaxBodyBytes }, 1048576},
		{"KESTRIX_WEBHOOK_MAX_BODY_BYTES", func(e *Environment) int64 { return e.WebhookMaxBodyBytes }, 8388608},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/default", func(t *testing.T) {
			env := LoadEnvironment(nil)
			if err := env.ValidationError(); err != nil {
				t.Fatal(err)
			}
			if got := tc.getter(env); got != tc.defaultValue {
				t.Fatalf("%s default = %d, want %d", tc.name, got, tc.defaultValue)
			}
		})
		t.Run(tc.name+"/explicit", func(t *testing.T) {
			env := LoadEnvironment([]string{tc.name + "=2048"})
			if err := env.ValidationError(); err != nil {
				t.Fatal(err)
			}
			if got := tc.getter(env); got != 2048 {
				t.Fatalf("%s = %d, want 2048", tc.name, got)
			}
		})
		for _, bad := range []string{"0", "-1", "garbage"} {
			t.Run(tc.name+"/"+bad, func(t *testing.T) {
				env := LoadEnvironment([]string{tc.name + "=" + bad})
				if err := env.ValidationError(); err == nil {
					t.Fatalf("%s=%q: ValidationError() = nil, want error naming the setting", tc.name, bad)
				} else if !strings.Contains(err.Error(), tc.name) {
					t.Fatalf("%s=%q: error = %v, want it to name %s", tc.name, bad, err, tc.name)
				}
			})
		}
	}
}
