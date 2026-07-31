package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFile(t *testing.T) {
	content := `
project "test-proj" {
  default_image = "golang:1.22"

  ci_repo "service_a" {
    path = "src/a"
    kind = "subpath"
  }

  ci_repo "service_b" {
    path = "src/b"
    kind = "git"
  }

  ci_task "build_a" {
    trigger    = "repo.service_a.changed"
    manual     = false
    depends_on = []

    execute {
      repo   = "service_a"
      script = "./build.sh"
      image  = "golang:1.22"
    }
  }

  ci_task "deploy" {
    trigger    = "repo.service_a.changed || repo.service_b.changed"
    manual     = true
    depends_on = ["build_a"]

    execute {
      repo   = "service_b"
      script = "./deploy.sh"
    }
  }
}
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.tf")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	proj, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if proj.Name != "test-proj" {
		t.Errorf("project name = %q, want %q", proj.Name, "test-proj")
	}
	if proj.DefaultImage != "golang:1.22" {
		t.Errorf("default_image = %q, want %q", proj.DefaultImage, "golang:1.22")
	}
	if len(proj.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(proj.Repos))
	}
	if proj.Repos[0].Name != "service_a" {
		t.Errorf("repo[0].name = %q, want %q", proj.Repos[0].Name, "service_a")
	}
	if proj.Repos[0].Path != "src/a" {
		t.Errorf("repo[0].path = %q, want %q", proj.Repos[0].Path, "src/a")
	}
	if proj.Repos[1].Kind != "git" {
		t.Errorf("repo[1].kind = %q, want %q", proj.Repos[1].Kind, "git")
	}
	if len(proj.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(proj.Tasks))
	}
	if proj.Tasks[0].Execute.Image != "golang:1.22" {
		t.Errorf("task[0].execute.image = %q, want %q", proj.Tasks[0].Execute.Image, "golang:1.22")
	}
	if !proj.Tasks[1].Manual {
		t.Error("expected task[1].manual to be true")
	}
}

func TestParseDir(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := `project "multi" {
	  variable "FROM_REPOS" {
	    value = "yes"
	  }
	  ci_repo "auth" {
    path = "src/auth"
    kind = "subpath"
  }
}`
	file2 := `project "multi" {
  default_image = "python:3.12"
  ci_repo "api" {
    path = "src/api"
    kind = "subpath"
  }
  ci_task "test" {
    trigger = "repo.auth.changed || repo.api.changed"
    execute {
      repo   = "auth"
      script = "./test.sh"
    }
  }
}`
	os.WriteFile(filepath.Join(tmpDir, "repos.tf"), []byte(file1), 0644)
	os.WriteFile(filepath.Join(tmpDir, "tasks.tf"), []byte(file2), 0644)

	proj, err := ParseDir(tmpDir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if proj.Name != "multi" {
		t.Errorf("name = %q, want %q", proj.Name, "multi")
	}
	if len(proj.Repos) != 2 {
		t.Errorf("repos = %d, want 2", len(proj.Repos))
	}
	if len(proj.Tasks) != 1 {
		t.Errorf("tasks = %d, want 1", len(proj.Tasks))
	}
	if proj.DefaultImage != "python:3.12" {
		t.Errorf("default_image = %q, want %q", proj.DefaultImage, "python:3.12")
	}
	if len(proj.Variables) != 1 || proj.Variables[0].Key != "FROM_REPOS" {
		t.Errorf("variables = %+v, want variable from every pipeline file", proj.Variables)
	}
}

func TestParseDirRejectsDifferentProjectNames(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "one.tf"), []byte(`project "one" {}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "two.tf"), []byte(`project "two" {}`), 0644)
	if _, err := ParseDir(tmpDir); err == nil {
		t.Fatal("expected different project names to be rejected")
	}
}

func TestParseFileNotFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/path.tf")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseDirEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	proj, err := ParseDir(tmpDir)
	if err != nil {
		t.Fatalf("ParseDir on empty dir: %v", err)
	}
	if proj.Name != "" {
		t.Errorf("expected empty project name, got %q", proj.Name)
	}
}

func TestParseSource(t *testing.T) {
	src := `project "inline" {
  ci_repo "svc" {
    path = "svc"
  }
  ci_task "build" {
    trigger = "repo.svc.changed"
    execute {
      repo   = "svc"
      script = "./build.sh"
    }
  }
}`
	proj, err := ParseSource([]byte(src), "inline.tf")
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	if proj.Name != "inline" {
		t.Errorf("name = %q, want %q", proj.Name, "inline")
	}
	if len(proj.Repos) != 1 || len(proj.Tasks) != 1 {
		t.Errorf("repos=%d tasks=%d, want 1,1", len(proj.Repos), len(proj.Tasks))
	}
}

func TestParseRemoteRepositoryDeclaration(t *testing.T) {
	src := `project "remote" {
  ci_repo "public" {
    url = "https://git.example.test/team/app.git"
    ref = "v2.1.0"
  }
  ci_repo "private" {
    url = "ssh://git@example.test/team/auth.git"
    ref = "main"
    credential_ref = "build-auth"
  }
}`
	proj, err := ParseSource([]byte(src), "remote.tf")
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	if len(proj.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(proj.Repos))
	}
	if proj.Repos[0].URL == "" || proj.Repos[0].Ref != "v2.1.0" {
		t.Fatalf("public repo = %+v", proj.Repos[0])
	}
	if proj.Repos[1].CredentialRef != "build-auth" {
		t.Fatalf("private repo = %+v", proj.Repos[1])
	}
}

func TestParseRepositoryRequiresExactlyOneSource(t *testing.T) {
	tests := []struct {
		name string
		repo string
	}{
		{name: "neither", repo: `ci_repo "repo" {}`},
		{name: "both", repo: "ci_repo \"repo\" {\n path = \"src\"\n url = \"https://git.example.test/repo.git\"\n ref = \"main\"\n}"},
		{name: "embedded credentials", repo: `ci_repo "repo" { url = "https://user:secret@git.example.test/repo.git" }`},
		{name: "malformed URL", repo: "ci_repo \"repo\" {\n url = \"https://%gh&\"\n ref = \"main\"\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte("project \"invalid\" {\n" + tt.repo + "\n}")
			if _, err := ParseSource(src, tt.name+".tf"); err == nil {
				t.Fatal("expected repository declaration error")
			}
		})
	}
}

func TestParseRepositoryRejectsEscapingLocalPath(t *testing.T) {
	for _, path := range []string{"/srv/repo", "../repo", "nested/../../repo"} {
		src := fmt.Sprintf(`project "p" {
  ci_repo "repo" {
    path = %q
  }
}`, path)
		if _, err := ParseSource([]byte(src), "pipeline.tf"); err == nil || !strings.Contains(err.Error(), "must be relative") {
			t.Fatalf("path %q: err = %v", path, err)
		}
	}
}

func TestParseRemoteRepositoryRequiresRef(t *testing.T) {
	for _, body := range []string{
		`ci_repo "remote" { url = "https://git.example/repo.git" }`,
		"ci_repo \"local\" {\n path = \"src\"\n ref = \"main\"\n}",
	} {
		if _, err := ParseSource([]byte("project \"invalid\" {\n"+body+"\n}"), "invalid.tf"); err == nil {
			t.Fatalf("expected required-ref error for %s", body)
		}
	}
}

func TestParseSourceInvalidHCL(t *testing.T) {
	src := `this is not valid HCL {{}}`
	_, err := ParseSource([]byte(src), "bad.tf")
	if err == nil {
		t.Fatal("expected error for invalid HCL")
	}
}

func TestParseDirIgnoresNonTF(t *testing.T) {
	tmpDir := t.TempDir()
	tfContent := `project "p" {
  ci_repo "r" {
    path = "r"
  }
}`
	os.WriteFile(filepath.Join(tmpDir, "pipeline.tf"), []byte(tfContent), 0644)
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Not a tf file"), 0644)

	proj, err := ParseDir(tmpDir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(proj.Repos) != 1 {
		t.Errorf("repos = %d, want 1 (should ignore .md files)", len(proj.Repos))
	}
}

// Exercise decodeProjectBlock's label-count guard: a project block
// with two labels must be rejected explicitly (the HCL parser itself
// would reject most other block shapes via syntax errors that get
// caught earlier in the pipeline).
func TestProjectBlockRejectsExtraLabels(t *testing.T) {
	src := `project "alpha" "beta" {
  ci_repo "r" { path = "r" }
}`
	_, err := ParseSource([]byte(src), "labels.tf")
	if err == nil {
		t.Fatal("expected ParseSource to reject project with two labels")
	}
}

// Exercise toString's "unsupported cty type" branch (L242): an
// attribute whose literal evaluates to a cty.Number (not string,
// not bool) hits the default error return inside toString.
func TestUnsupportedLiteralCtyType(t *testing.T) {
	src := `project "p" {
  ci_repo "r" { path = 42 }
}`
	_, err := ParseSource([]byte(src), "num.tf")
	if err == nil {
		t.Fatal("expected ParseSource to reject numeric path attribute")
	}
	if !strings.Contains(err.Error(), "unsupported cty type") {
		t.Errorf("err = %v, want it to mention unsupported cty type", err)
	}
}
