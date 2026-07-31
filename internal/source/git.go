package source

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/domain"
)

var commandContext = exec.CommandContext

func (a *GitAcquirer) acquireOnce(parent context.Context, command string, repo domain.Repo, destination string, timeout time.Duration, credential domain.Credential) (domain.AcquiredSource, error) {
	if err := os.RemoveAll(destination); err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("reset destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("create destination parent: %w", err)
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	env, cleanup, err := credentialEnvironment(credential)
	if err != nil {
		return domain.AcquiredSource{}, err
	}
	defer cleanup()

	clone := commandContext(ctx, command, "clone", "--quiet", repo.URL, destination)
	clone.Env = append(config.Current().Environ(), env...)
	if output, err := clone.CombinedOutput(); err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("git clone failed: %s", redactGitOutput(string(output)))
	}

	if repo.Ref != "" {
		checkout := commandContext(ctx, command, "-C", destination, "checkout", "--quiet", "--force", repo.Ref)
		checkout.Env = append(config.Current().Environ(), env...)
		if output, err := checkout.CombinedOutput(); err != nil {
			return domain.AcquiredSource{}, fmt.Errorf("git checkout %q failed: %s", repo.Ref, redactGitOutput(string(output)))
		}
	}

	resolve := commandContext(ctx, command, "-C", destination, "rev-parse", "HEAD")
	resolve.Env = append(config.Current().Environ(), env...)
	output, err := resolve.Output()
	if err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("resolve checked-out revision failed: %w", err)
	}
	sha := strings.TrimSpace(string(output))
	if len(sha) < 7 {
		return domain.AcquiredSource{}, fmt.Errorf("resolve checked-out revision returned invalid SHA")
	}
	return domain.AcquiredSource{RepoName: repo.Name, Path: destination, URL: repo.URL, RequestedRef: repo.Ref, ResolvedRevision: sha}, nil
}

func credentialEnvironment(credential domain.Credential) ([]string, func(), error) {
	if credential.Username == "" && credential.Password == "" && credential.Token == "" && credential.PrivateKey == "" {
		return nil, func() {}, nil
	}
	var files []string
	writeSecret := func(prefix, value string, mode os.FileMode) (string, error) {
		file, err := os.CreateTemp("", prefix)
		if err != nil {
			return "", err
		}
		path := file.Name()
		files = append(files, path)
		if err := file.Chmod(mode); err != nil {
			file.Close()
			return "", err
		}
		if _, err := file.WriteString(value); err != nil {
			file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return path, nil
	}
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if credential.Password != "" || credential.Token != "" {
		secret := credential.Password
		if secret == "" {
			secret = credential.Token
		}
		secretPath, err := writeSecret("kestrix-git-secret-", secret, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("create temporary credential material: %w", err)
		}
		userPath, err := writeSecret("kestrix-git-user-", credential.Username, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("create temporary credential material: %w", err)
		}
		askpass, err := writeSecret("kestrix-git-askpass-", "#!/bin/sh\ncase \"$1\" in\n  *sername*) cat \"$KESTRIX_GIT_USERNAME_FILE\" ;;\n  *) cat \"$KESTRIX_GIT_SECRET_FILE\" ;;\nesac\n", 0o700)
		if err != nil {
			return nil, nil, fmt.Errorf("create askpass helper: %w", err)
		}
		env = append(env, "GIT_ASKPASS="+askpass, "KESTRIX_GIT_USERNAME_FILE="+userPath, "KESTRIX_GIT_SECRET_FILE="+secretPath)
	}
	if credential.PrivateKey != "" {
		keyPath, err := writeSecret("kestrix-git-key-", credential.PrivateKey, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("create SSH key material: %w", err)
		}
		knownHostsPath, err := writeSecret("kestrix-git-known-hosts-", credential.KnownHosts, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("create known-hosts material: %w", err)
		}
		ssh := "ssh -i " + shellQuote(keyPath) + " -o IdentitiesOnly=yes -o StrictHostKeyChecking="
		if credential.KnownHosts == "" {
			ssh += "accept-new"
		} else {
			ssh += "yes -o UserKnownHostsFile=" + shellQuote(knownHostsPath)
		}
		env = append(env, "GIT_SSH_COMMAND="+ssh)
	}
	return env, func() {
		for _, path := range files {
			_ = os.Remove(path)
		}
	}, nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func hasEmbeddedCredentials(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return false
	}
	if parsed.Scheme == "ssh" && parsed.User.Username() != "" {
		_, password := parsed.User.Password()
		return password
	}
	return true
}

func redactGitOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "git command returned an error"
	}
	if parsed, err := url.Parse(output); err == nil && parsed.User != nil {
		parsed.User = nil
		output = parsed.String()
	}
	return strings.ReplaceAll(output, "KESTRIX_GIT_SECRET_FILE", "[REDACTED]")
}
