package config

import (
	"reflect"
	"testing"
)

func TestLoadEnvironmentUsesCanonicalNamesOnly(t *testing.T) {
	env := LoadEnvironment([]string{
		"KESTRIX_WEBHOOK_SECRET=canonical-secret",
		"DATABASE_URL=postgres://example",
		"UNRELATED=value",
	})

	if env.WebhookSecret != "canonical-secret" {
		t.Fatalf("typed webhook secret = %q", env.WebhookSecret)
	}
	if env.ListenAddr != "" {
		t.Fatalf("unexpected address from legacy environment: %q", env.ListenAddr)
	}
	if got := env.Prefixed(Prefix); !reflect.DeepEqual(got, []string{"KESTRIX_WEBHOOK_SECRET=canonical-secret"}) {
		t.Fatalf("prefixed values = %#v", got)
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
