package config

import (
	"testing"
)

func TestTaskEnvWithholdsControlPlaneSecrets(t *testing.T) {
	env := LoadEnvironment([]string{
		"PATH=/usr/bin",
		"HOME=/home/kestrix",
		"KESTRIX_WEBHOOK_SECRET=super-secret",
		"KESTRIX_GITLAB_TOKEN=glpat-secret",
		"DATABASE_URL=postgres://example",
		"DOCKER_PASSWORD=hunter2",
		"DOCKER_REGISTRY=registry.example.com",
		"KESTRIX_RUNNER_ENV_PASSTHROUGH=DOCKER_REGISTRY",
	})
	if err := env.ValidationError(); err != nil {
		t.Fatal(err)
	}

	out := env.TaskEnv(map[string]string{"CI_PROJECT_DIR": "/workspace"}, nil)
	got := toMap(out)

	for _, denied := range []string{"KESTRIX_WEBHOOK_SECRET", "KESTRIX_GITLAB_TOKEN", "DATABASE_URL", "DOCKER_PASSWORD"} {
		if _, ok := got[denied]; ok {
			t.Errorf("TaskEnv leaked %s", denied)
		}
	}
	if got["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want /usr/bin", got["PATH"])
	}
	if got["CI_PROJECT_DIR"] != "/workspace" {
		t.Errorf("CI_PROJECT_DIR = %q, want /workspace", got["CI_PROJECT_DIR"])
	}
	if got["DOCKER_REGISTRY"] != "registry.example.com" {
		t.Errorf("DOCKER_REGISTRY = %q, want passthrough value", got["DOCKER_REGISTRY"])
	}
}

func TestTaskEnvPipelineVariableOverridesHostValue(t *testing.T) {
	env := LoadEnvironment([]string{"TERM=xterm-host"})
	out := env.TaskEnv(nil, map[string]string{"TERM": "dumb"})
	got := toMap(out)
	if got["TERM"] != "dumb" {
		t.Errorf("TERM = %q, want pipeline value dumb", got["TERM"])
	}
	count := 0
	for _, entry := range out {
		if entry == "TERM=dumb" || entry == "TERM=xterm-host" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("TERM appears %d times in %v, want exactly once", count, out)
	}
}

func TestTaskEnvNilEnvironmentIsSafe(t *testing.T) {
	var env *Environment
	out := env.TaskEnv(map[string]string{"CI_PROJECT_DIR": "/workspace"}, map[string]string{"KEY": "value"})
	got := toMap(out)
	if got["CI_PROJECT_DIR"] != "/workspace" || got["KEY"] != "value" {
		t.Fatalf("TaskEnv on nil environment = %v", out)
	}
}

func TestAllowlistNamesListsBaseCIAndPassthrough(t *testing.T) {
	env := LoadEnvironment([]string{"KESTRIX_RUNNER_ENV_PASSTHROUGH=DOCKER_REGISTRY,DEPLOY_HOST"})
	names := env.AllowlistNames()
	for _, want := range []string{"PATH", "HOME", "CI_*", "DOCKER_REGISTRY", "DEPLOY_HOST"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllowlistNames() = %v, missing %q", names, want)
		}
	}
}

func toMap(entries []string) map[string]string {
	m := make(map[string]string, len(entries))
	for _, entry := range entries {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				m[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	return m
}
