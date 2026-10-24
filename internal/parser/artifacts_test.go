package parser

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseTask_Artifacts_Valid(t *testing.T) {
	hcl := `
project "app" {
  ci_repo "repo" {
    path = "repo"
  }
  ci_task "build" {
    trigger = "repo.repo.changed"
    artifacts = ["dist/", "out/bundle.tar.gz"]
    execute {
      repo = "repo"
      script = "echo ok"
    }
  }
}
`
	proj, err := ParseSource([]byte(hcl), "test.tf")
	if err != nil {
		t.Fatalf("ParseSource failed: %v", err)
	}
	if len(proj.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(proj.Tasks))
	}
	task := proj.Tasks[0]
	if len(task.Artifacts) != 2 || task.Artifacts[0] != "dist/" || task.Artifacts[1] != "out/bundle.tar.gz" {
		t.Fatalf("unexpected artifacts: %v", task.Artifacts)
	}
}

func TestParseTask_Artifacts_AbsolutePath(t *testing.T) {
	hcl := `
project "app" {
  ci_repo "repo" {
    path = "repo"
  }
  ci_task "build" {
    trigger = "repo.repo.changed"
    artifacts = ["/etc/passwd"]
  }
}
`
	_, err := ParseSource([]byte(hcl), "test.tf")
	if err == nil || !strings.Contains(err.Error(), "path must be relative") {
		t.Fatalf("expected error naming path must be relative, got %v", err)
	}
	if !strings.Contains(err.Error(), "build") || !strings.Contains(err.Error(), "/etc/passwd") {
		t.Fatalf("error must name task and path, got %v", err)
	}
}

func TestParseTask_Artifacts_DotDot(t *testing.T) {
	hcl := `
project "app" {
  ci_repo "repo" {
    path = "repo"
  }
  ci_task "build" {
    trigger = "repo.repo.changed"
    artifacts = ["../secrets"]
  }
}
`
	_, err := ParseSource([]byte(hcl), "test.tf")
	if err == nil || !strings.Contains(err.Error(), "must not contain '..' components") {
		t.Fatalf("expected error naming '..' components, got %v", err)
	}
	if !strings.Contains(err.Error(), "build") || !strings.Contains(err.Error(), "../secrets") {
		t.Fatalf("error must name task and path, got %v", err)
	}
}

func TestParseTask_Artifacts_EmptyEntry(t *testing.T) {
	hcl := `
project "app" {
  ci_repo "repo" {
    path = "repo"
  }
  ci_task "build" {
    trigger = "repo.repo.changed"
    artifacts = ["dist/", ""]
  }
}
`
	_, err := ParseSource([]byte(hcl), "test.tf")
	if err == nil || !strings.Contains(err.Error(), "artifact path cannot be empty") {
		t.Fatalf("expected error naming artifact path cannot be empty, got %v", err)
	}
	if !strings.Contains(err.Error(), "build") {
		t.Fatalf("error must name task, got %v", err)
	}
}

func TestParseTask_Artifacts_33Entries(t *testing.T) {
	var entries []string
	for i := 0; i < 33; i++ {
		entries = append(entries, fmt.Sprintf(`"file_%d"`, i))
	}
	hcl := fmt.Sprintf(`
project "app" {
  ci_repo "repo" {
    path = "repo"
  }
  ci_task "build" {
    trigger = "repo.repo.changed"
    artifacts = [%s]
  }
}
`, strings.Join(entries, ", "))

	_, err := ParseSource([]byte(hcl), "test.tf")
	if err == nil || !strings.Contains(err.Error(), "at most 32 entries allowed") {
		t.Fatalf("expected error naming at most 32 entries allowed, got %v", err)
	}
	if !strings.Contains(err.Error(), "build") {
		t.Fatalf("error must name task, got %v", err)
	}
}

func TestParseTask_Artifacts_SingleString(t *testing.T) {
	hcl := `
project "app" {
  ci_repo "repo" {
    path = "repo"
  }
  ci_task "build" {
    trigger = "repo.repo.changed"
    artifacts = "dist/"
  }
}
`
	proj, err := ParseSource([]byte(hcl), "test.tf")
	if err != nil {
		t.Fatalf("ParseSource failed: %v", err)
	}
	if len(proj.Tasks[0].Artifacts) != 1 || proj.Tasks[0].Artifacts[0] != "dist/" {
		t.Fatalf("unexpected artifacts: %v", proj.Tasks[0].Artifacts)
	}
}

func TestParseTask_Artifacts_NonLiteral(t *testing.T) {
	hcl := `
project "app" {
  ci_repo "repo" {
    path = "repo"
  }
  ci_task "build" {
    trigger = "repo.repo.changed"
    artifacts = ["${var.foo}"]
  }
}
`
	_, err := ParseSource([]byte(hcl), "test.tf")
	if err == nil || !strings.Contains(err.Error(), "artifacts") {
		t.Fatalf("expected error mentioning artifacts, got %v", err)
	}
}
