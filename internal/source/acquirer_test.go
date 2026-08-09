package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func createBareRepository(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	bare := filepath.Join(root, "repo.git")
	gitTest(t, root, "init", "--bare", bare)
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", ".")
	gitTest(t, work, "-c", "user.email=test@example.test", "-c", "user.name=Test", "commit", "-m", "one")
	first := gitTest(t, work, "rev-parse", "HEAD")
	gitTest(t, work, "remote", "add", "origin", bare)
	gitTest(t, work, "push", "origin", "main")
	gitTest(t, work, "tag", "v1")
	gitTest(t, work, "push", "origin", "v1")
	return bare, first, root
}

func TestGitAcquirerResolvesBranchTagAndCommit(t *testing.T) {
	bare, first, _ := createBareRepository(t)
	acquirer := &GitAcquirer{}
	for _, ref := range []string{"main", "v1", first} {
		t.Run(ref, func(t *testing.T) {
			result, err := acquirer.Acquire(context.Background(), domain.Repo{Name: "app", URL: bare, Ref: ref}, domain.AcquisitionOptions{Workspace: t.TempDir()})
			if err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			if result.ResolvedRevision != first || result.RequestedRef != ref {
				t.Fatalf("result = %+v", result)
			}
			if _, err := os.Stat(filepath.Join(result.Path, "README.md")); err != nil {
				t.Fatalf("acquired file: %v", err)
			}
		})
	}
}

func TestGitAcquirerMissingRefCleansWorkspace(t *testing.T) {
	bare, _, _ := createBareRepository(t)
	workspace := t.TempDir()
	_, err := (&GitAcquirer{}).Acquire(context.Background(), domain.Repo{Name: "app", URL: bare, Ref: "missing"}, domain.AcquisitionOptions{Workspace: workspace})
	if err == nil || !strings.Contains(err.Error(), "checkout") {
		t.Fatalf("err = %v, want checkout error", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "app")); !os.IsNotExist(statErr) {
		t.Fatalf("workspace still exists: %v", statErr)
	}
}

func TestGitAcquirerRetriesCloneFailureAndRedactsURL(t *testing.T) {
	workspace := t.TempDir()
	_, err := (&GitAcquirer{}).Acquire(context.Background(), domain.Repo{Name: "app", URL: "https://user:secret@example.test/repo.git"}, domain.AcquisitionOptions{Workspace: workspace, Retries: 1})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v, want redacted failure", err)
	}
}

func TestGitAcquirerCommandTimeout(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-git.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err := (&GitAcquirer{Command: script}).Acquire(ctx, domain.Repo{Name: "app", URL: "https://example.test/repo.git"}, domain.AcquisitionOptions{Workspace: t.TempDir(), Timeout: 20 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestLocalAcquirerResolvesMonorepoAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "services"), 0o700); err != nil {
		t.Fatal(err)
	}
	acquirer := &LocalAcquirer{}
	got, err := acquirer.Acquire(context.Background(), domain.Repo{Name: "api", Path: "services"}, domain.AcquisitionOptions{BaseDir: root})
	if err != nil || got.Path != filepath.Join(root, "services") {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := acquirer.Acquire(context.Background(), domain.Repo{Name: "bad", Path: "../outside"}, domain.AcquisitionOptions{BaseDir: root}); err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestCredentialMaterialIsRemovedOnCleanup(t *testing.T) {
	env, cleanup, err := credentialEnvironment(domain.Credential{Token: "secret"})
	if err != nil || len(env) == 0 {
		t.Fatalf("env=%v err=%v", env, err)
	}
	var path string
	for _, entry := range env {
		if strings.HasPrefix(entry, "GIT_ASKPASS=") {
			path = strings.TrimPrefix(entry, "GIT_ASKPASS=")
		}
	}
	if path == "" {
		t.Fatal("GIT_ASKPASS path missing")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("credential material missing: %v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("credential material still exists: %v", err)
	}
}

func TestCredentialMaterialDoesNotPutSecretInEnvironment(t *testing.T) {
	env, cleanup, err := credentialEnvironment(domain.Credential{Username: "git", Password: "secret", Type: "basic"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, entry := range env {
		if strings.HasSuffix(entry, "=secret") {
			t.Fatalf("secret leaked into child environment: %q", entry)
		}
	}
}

func TestSSHCredentialMaterialIsCleaned(t *testing.T) {
	env, cleanup, err := credentialEnvironment(domain.Credential{PrivateKey: "PRIVATE KEY", KnownHosts: "known host", Type: "ssh"})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, entry := range env {
		if strings.Contains(entry, "KESTRIX_GIT_KEY") || strings.Contains(entry, "UserKnownHostsFile") {
			paths = append(paths, entry)
		}
	}
	cleanup()
	if len(paths) == 0 {
		t.Fatal("SSH temporary material was not configured")
	}
}
