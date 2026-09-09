package source

import "testing"

func TestSourceMatchKeyMergesTransports(t *testing.T) {
	for _, raw := range []string{
		"https://H/a/b.git",
		"HTTPS://H/a/b/",
		"ssh://git@H/a/b.git",
		"git@H:a/b.git",
		"  https://h/a/b  ",
		"https://H:443/a/b.git",
		"ssh://git@H:22/a/b.git",
		"https://H/a/b.GIT",
		"git@H:a/b.Git",
	} {
		key, err := SourceMatchKey(raw)
		if err != nil {
			t.Fatalf("SourceMatchKey(%q): %v", raw, err)
		}
		if key != "h/a/b" {
			t.Errorf("SourceMatchKey(%q) = %q, want h/a/b", raw, key)
		}
	}
}

func TestSourceMatchKeyKeepsDistinctRepositoriesApart(t *testing.T) {
	a, _ := SourceMatchKey("https://git.example.test/team/a.git")
	fork, _ := SourceMatchKey("https://git.example.test/team/a-fork.git")
	otherHost, _ := SourceMatchKey("https://other.example.test/team/a.git")
	withPort, _ := SourceMatchKey("https://git.example.test:8443/team/a.git")
	if a == fork || a == otherHost || a == withPort {
		t.Errorf("keys collide: %q %q %q %q", a, fork, otherHost, withPort)
	}
	local, _ := SourceMatchKey("/srv/git/remote.git")
	fileURL, _ := SourceMatchKey("file:///srv/git/remote.git")
	if local != fileURL || local != "/srv/git/remote" {
		t.Errorf("local forms: %q vs %q", local, fileURL)
	}
}

func TestSourceMatchKeyRejectsBadInput(t *testing.T) {
	for _, raw := range []string{
		"https://user:token@H/a/b.git",
		"https://user@H/a/b.git",
		"",
		"https://H",
		"http://[::1]:namedport/x",
	} {
		if key, err := SourceMatchKey(raw); err == nil {
			t.Errorf("SourceMatchKey(%q) = %q, want error", raw, key)
		}
	}
}
