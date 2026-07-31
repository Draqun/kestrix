package config

import (
	"os"
	"sort"
	"strings"
	"sync"
)

// Prefix is the canonical prefix for Kestrix-owned environment variables.
const Prefix = "KESTRIX_"

const unsupportedProjectPrefix = "META" + "CI_"

// Environment is an immutable snapshot of the process environment.
//
// Kestrix-owned variables are read only from the canonical KESTRIX_* names.
// Generic and provider-owned variables retain their original names.
type Environment struct {
	ListenAddr      string
	PipelineDir     string
	WebhookSecret   string
	WebhookMethod   string
	DispatchProject string
	GitLabURL       string
	GitLabToken     string
	TestDatabaseURL string
	DatabaseURL     string
	Docker          DockerEnvironment

	values       map[string]string
	originalEnvs []string
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

	return &Environment{
		ListenAddr:      values[Prefix+"ADDR"],
		PipelineDir:     values[Prefix+"DIR"],
		WebhookSecret:   values[Prefix+"WEBHOOK_SECRET"],
		WebhookMethod:   values[Prefix+"WEBHOOK_METHOD"],
		DispatchProject: values[Prefix+"DISPATCH_PROJECT"],
		GitLabURL:       values[Prefix+"GITLAB_URL"],
		GitLabToken:     values[Prefix+"GITLAB_TOKEN"],
		TestDatabaseURL: values[Prefix+"TEST_DATABASE_URL"],
		DatabaseURL:     values["DATABASE_URL"],
		Docker: DockerEnvironment{
			Registry: values["DOCKER_REGISTRY"],
			Image:    values["DOCKER_IMAGE"],
			Username: values["DOCKER_USERNAME"],
			Password: values["DOCKER_PASSWORD"],
		},
		values:       values,
		originalEnvs: normalized,
	}
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
