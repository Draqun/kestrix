package config

import (
	"reflect"
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
	if env.Docker.Image != "kestrix:test" {
		t.Fatalf("typed Docker image = %q", env.Docker.Image)
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

func TestNilEnvironmentValidationIsValid(t *testing.T) {
	var env *Environment
	if err := env.ValidationError(); err != nil {
		t.Fatalf("nil environment validation = %v", err)
	}
}
