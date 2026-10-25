package parser

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"kestrix/internal/model"
)

func TestCheckVariableEligibility(t *testing.T) {
	tests := []struct {
		name      string
		v         model.Variable
		field     string
		wantErr   string
		isSuccess bool
	}{
		{
			name: "secret_ref with field",
			v: model.Variable{
				Key:       "TOKEN",
				SecretRef: "sec-1",
				Masked:    true,
			},
			field:   "image",
			wantErr: `variable "TOKEN" cannot be interpolated into image: secret variables cannot be interpolated`,
		},
		{
			name: "secret_ref without field",
			v: model.Variable{
				Key:       "TOKEN",
				SecretRef: "sec-1",
			},
			field:   "",
			wantErr: `variable "TOKEN": secret variables cannot be interpolated`,
		},
		{
			name: "masked variable",
			v: model.Variable{
				Key:    "API_KEY",
				Masked: true,
			},
			field:   "image",
			wantErr: `variable "API_KEY" cannot be interpolated into image: masked variables cannot be interpolated`,
		},
		{
			name: "protected variable",
			v: model.Variable{
				Key:       "PROT",
				Protected: true,
			},
			field:   "runner_tags",
			wantErr: `variable "PROT" cannot be interpolated into runner_tags: protected variables cannot be interpolated`,
		},
		{
			name: "job-scoped variable",
			v: model.Variable{
				Key:   "JOB_V",
				Scope: "job",
				Repo:  "r",
				Job:   "j",
				Value: "val",
			},
			field:   "ref",
			wantErr: `variable "JOB_V" cannot be interpolated into ref: job-scoped variables cannot be interpolated`,
		},
		{
			name: "repo-scoped variable",
			v: model.Variable{
				Key:   "REPO_V",
				Scope: "repo",
				Repo:  "r",
				Value: "val",
			},
			field:   "url",
			wantErr: `variable "REPO_V" cannot be interpolated into url: repo-scoped variables cannot be interpolated`,
		},
		{
			name: "custom-scoped variable",
			v: model.Variable{
				Key:   "CUSTOM_V",
				Scope: "stage",
				Value: "val",
			},
			field:   "default_image",
			wantErr: `variable "CUSTOM_V" cannot be interpolated into default_image: stage-scoped variables cannot be interpolated`,
		},
		{
			name: "empty value and default",
			v: model.Variable{
				Key: "EMPTY_V",
			},
			field:   "image",
			wantErr: `variable "EMPTY_V" cannot be interpolated into image: variable has no value or default`,
		},
		{
			name: "eligible with value",
			v: model.Variable{
				Key:   "NODE",
				Value: "22",
			},
			field:     "image",
			isSuccess: true,
		},
		{
			name: "eligible with default",
			v: model.Variable{
				Key:     "GO_VER",
				Default: "1.26",
			},
			field:     "default_image",
			isSuccess: true,
		},
		{
			name: "eligible with explicit project scope",
			v: model.Variable{
				Key:   "ARCH",
				Scope: "project",
				Value: "arm64",
			},
			field:     "runner_tags",
			isSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkVariableEligibility(tt.v, tt.field)
			if tt.isSuccess {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestBuildVarEvalContext(t *testing.T) {
	// Nil / empty variables
	vc := buildVarEvalContext(nil)
	if vc == nil || vc.evalCtx == nil {
		t.Fatal("expected non-nil context")
	}

	// Variables with mixed eligibility
	vars := []model.Variable{
		{Key: "NODE", Value: "22"},
		{Key: "ALPINE", Default: "3.20"},
		{Key: "SECRET", SecretRef: "sec-ref"},
	}
	vc = buildVarEvalContext(vars)

	varVal := vc.evalCtx.Variables["var"]
	if !varVal.Type().HasAttribute("NODE") {
		t.Fatalf("expected var to have NODE attribute")
	}
	if !varVal.Type().HasAttribute("ALPINE") {
		t.Fatalf("expected var to have ALPINE attribute")
	}
	if varVal.Type().HasAttribute("SECRET") {
		t.Fatalf("expected secret variable to NOT be in eval context")
	}
}

func TestInterpolation_AllowedFields(t *testing.T) {
	src := `project "test-proj" {
  default_image = "golang:${var.go_ver}"

  variable "go_ver" {
    value = "1.26"
  }

  variable "node" {
    value = "22"
  }

  variable "arch" {
    value = "arm64"
  }

  variable "repo_url" {
    value = "https://git.example.com/org/repo.git"
  }

  variable "release" {
    value = "v1.5.0"
  }

  ci_repo "remote" {
    url = var.repo_url
    ref = var.release
  }

  ci_task "build" {
    trigger     = "repo.remote.changed"
    runner_tags = ["${var.arch}", "gpu"]
    execute {
      repo   = "remote"
      script = "make build"
      image  = "node:${var.node}"
    }
  }

  ci_task "test" {
    trigger     = "repo.remote.changed"
    runner_tags = "${var.arch}"
    execute {
      repo   = "remote"
      script = "make test"
      image  = "node:${var.node}-alpine"
    }
  }
}`

	proj, err := ParseSource([]byte(src), "pipeline.tf")
	if err != nil {
		t.Fatalf("ParseSource failed: %v", err)
	}

	if proj.DefaultImage != "golang:1.26" {
		t.Errorf("default_image = %q, want golang:1.26", proj.DefaultImage)
	}

	if len(proj.Repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(proj.Repos))
	}
	repo := proj.Repos[0]
	if repo.URL != "https://git.example.com/org/repo.git" {
		t.Errorf("repo url = %q, want https://git.example.com/org/repo.git", repo.URL)
	}
	if repo.Ref != "v1.5.0" {
		t.Errorf("repo ref = %q, want v1.5.0", repo.Ref)
	}

	if len(proj.Tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(proj.Tasks))
	}
	task1 := proj.Tasks[0]
	if task1.Execute.Image != "node:22" {
		t.Errorf("task1 image = %q, want node:22", task1.Execute.Image)
	}
	if len(task1.RunnerTags) != 2 || task1.RunnerTags[0] != "arm64" || task1.RunnerTags[1] != "gpu" {
		t.Errorf("task1 runner_tags = %v, want [arm64 gpu]", task1.RunnerTags)
	}

	task2 := proj.Tasks[1]
	if task2.Execute.Image != "node:22-alpine" {
		t.Errorf("task2 image = %q, want node:22-alpine", task2.Execute.Image)
	}
	if len(task2.RunnerTags) != 1 || task2.RunnerTags[0] != "arm64" {
		t.Errorf("task2 runner_tags = %v, want [arm64]", task2.RunnerTags)
	}
}

func TestInterpolation_Rejections(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		wantSubstr []string
	}{
		{
			name: "unknown variable in image",
			src: `project "p" {
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = "node:${var.unknown}"
    }
  }
}`,
			wantSubstr: []string{"image", "unknown", "unknown variable"},
		},
		{
			name: "secret variable in image",
			src: `project "p" {
  variable "TOKEN" {
    secret_ref = "cred-1"
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = "registry.example/app:${var.TOKEN}"
    }
  }
}`,
			wantSubstr: []string{"image", "TOKEN", "secret variables cannot be interpolated"},
		},
		{
			name: "masked variable in image",
			src: `project "p" {
  variable "MASKED" {
    value  = "secret"
    masked = true
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = "${var.MASKED}"
    }
  }
}`,
			wantSubstr: []string{"image", "MASKED", "masked variables cannot be interpolated"},
		},
		{
			name: "protected variable in runner_tags",
			src: `project "p" {
  variable "PROT" {
    value     = "tag"
    protected = true
  }
  ci_task "t" {
    trigger     = "repo.x.changed"
    runner_tags = ["${var.PROT}"]
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"runner_tags", "PROT", "protected variables cannot be interpolated"},
		},
		{
			name: "job-scoped variable in image",
			src: `project "p" {
  variable "JOB_VAR" {
    value = "val"
    scope = "job"
    repo  = "r"
    job   = "j"
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = "node:${var.JOB_VAR}"
    }
  }
}`,
			wantSubstr: []string{"image", "JOB_VAR", "job-scoped variables cannot be interpolated"},
		},
		{
			name: "function call rejected in image",
			src: `project "p" {
  variable "node" {
    value = "22"
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = upper(var.node)
    }
  }
}`,
			wantSubstr: []string{"only variable references"},
		},
		{
			name: "function call inside template rejected",
			src: `project "p" {
  variable "node" {
    value = "22"
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = "node:${upper(var.node)}"
    }
  }
}`,
			wantSubstr: []string{"only variable references"},
		},
		{
			name: "interpolated URL failing embedded credentials validation",
			src: `project "p" {
  variable "repo_url" {
    value = "https://user:pass@example.com/repo.git"
  }
  ci_repo "r" {
    url = var.repo_url
    ref = "main"
  }
}`,
			wantSubstr: []string{"embedded credentials"},
		},
		{
			name: "interpolated runner_tags failing tag normalization",
			src: `project "p" {
  variable "bad_tag" {
    value = "invalid tag space"
  }
  ci_task "t" {
    trigger     = "repo.x.changed"
    runner_tags = ["${var.bad_tag}"]
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"invalid runner tag"},
		},
		{
			name: "interpolated runner_tags single string failure",
			src: `project "p" {
  variable "bad_tag" {
    value = "invalid tag space"
  }
  ci_task "t" {
    trigger     = "repo.x.changed"
    runner_tags = var.bad_tag
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"invalid runner tag"},
		},
		{
			name: "script field rejects template wrap and points to $VAR and $${VAR}",
			src: `project "p" {
  variable "node" {
    value = "22"
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "${var.node}"
    }
  }
}`,
			wantSubstr: []string{"script", "$VAR", "$${VAR}"},
		},
		{
			name: "script field rejects template and points to $VAR and $${VAR}",
			src: `project "p" {
  variable "node" {
    value = "22"
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "echo ${var.node}"
    }
  }
}`,
			wantSubstr: []string{"script", "$VAR", "$${VAR}"},
		},
		{
			name: "script field rejects scope traversal",
			src: `project "p" {
  variable "node" {
    value = "22"
  }
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = var.node
    }
  }
}`,
			wantSubstr: []string{"script", "$VAR"},
		},
		{
			name: "trigger field rejects template",
			src: `project "p" {
  variable "trig" {
    value = "repo.x.changed"
  }
  ci_task "t" {
    trigger = "${var.trig}"
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"trigger", "expression syntax"},
		},
		{
			name: "trigger field rejects multi-part template",
			src: `project "p" {
  variable "trig" {
    value = "repo.x.changed"
  }
  ci_task "t" {
    trigger = "foo ${var.trig}"
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"trigger", "expression syntax"},
		},
		{
			name: "non-allowlisted field rejects template wrap",
			src: `project "p" {
  variable "trig" {
    value = "true"
  }
  ci_task "t" {
    manual = "${var.trig}"
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"interpolated strings are not supported in pipeline config"},
		},
		{
			name: "non-allowlisted field rejects multi-part template",
			src: `project "p" {
  variable "trig" {
    value = "true"
  }
  ci_task "t" {
    manual = "val ${var.trig}"
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"interpolated strings are not supported in pipeline config"},
		},
		{
			name: "image rejects non-var scope traversal",
			src: `project "p" {
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = other.pkg
    }
  }
}`,
			wantSubstr: []string{"only variable references are supported"},
		},
		{
			name: "image template rejects non-var scope traversal",
			src: `project "p" {
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = "node:${other.pkg}"
    }
  }
}`,
			wantSubstr: []string{"only variable references are supported"},
		},
		{
			name: "artifacts rejects non-literal expression",
			src: `project "p" {
  variable "x" {
    value = "dist"
  }
  ci_task "t" {
    trigger   = "repo.x.changed"
    artifacts = "${var.x}"
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"interpolated strings are not supported"},
		},
		{
			name: "runner_tags single string eval error on missing var",
			src: `project "p" {
  ci_task "t" {
    trigger     = "repo.x.changed"
    runner_tags = var.missing_tag
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"runner_tags", "missing_tag", "unknown variable"},
		},
		{
			name: "project extra attribute literal error",
			src: `project "p" {
  extra_attr = "${var.x}"
}`,
			wantSubstr: []string{"interpolated strings are not supported"},
		},
		{
			name: "image bare non-string literal value",
			src: `project "p" {
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "make"
      image  = 123
    }
  }
}`,
			wantSubstr: []string{"unsupported cty type number"},
		},
		{
			name: "tuple in non-allowlisted field with invalid element",
			src: `project "p" {
  ci_task "t" {
    trigger    = "repo.x.changed"
    depends_on = ["${var.x}"]
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"interpolated strings are not supported"},
		},
		{
			name: "literal unsupported expression type",
			src: `project "p" {
  ci_task "t" {
    trigger = upper("repo.x.changed")
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"unsupported expression type"},
		},
		{
			name: "literal object cons expression in non-allowlisted field",
			src: `project "p" {
  ci_task "t" {
    trigger = { a = "1" }
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"object literals are not supported"},
		},
		{
			name: "trigger field rejects scope traversal",
			src: `project "p" {
  variable "trig" {
    value = "repo.x.changed"
  }
  ci_task "t" {
    trigger = var.trig
    execute {
      script = "make"
    }
  }
}`,
			wantSubstr: []string{"trigger", "expression syntax"},
		},
		{
			name: "default_image rejects unknown variable",
			src: `project "p" {
  default_image = var.missing
}`,
			wantSubstr: []string{"default_image", "missing", "unknown variable"},
		},
		{
			name: "repo url rejects unknown variable",
			src: `project "p" {
  ci_repo "r" {
    url = var.missing_url
    ref = "main"
  }
}`,
			wantSubstr: []string{"url", "missing_url", "unknown variable"},
		},
		{
			name: "repo ref rejects unknown variable",
			src: `project "p" {
  ci_repo "r" {
    url = "https://example.com/repo.git"
    ref = var.missing_ref
  }
}`,
			wantSubstr: []string{"ref", "missing_ref", "unknown variable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource([]byte(tt.src), "x.tf")
			if err == nil {
				t.Fatalf("expected error containing %v, got nil", tt.wantSubstr)
			}
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), sub)
				}
			}
		})
	}
}

func TestScript_DoubleDollarEscape(t *testing.T) {
	src := `project "p" {
  ci_task "t" {
    trigger = "repo.x.changed"
    execute {
      script = "echo $${FOO}"
    }
  }
}`
	proj, err := ParseSource([]byte(src), "x.tf")
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	if len(proj.Tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(proj.Tasks))
	}
	if proj.Tasks[0].Execute.Script != "echo ${FOO}" {
		t.Errorf("script = %q, want %q", proj.Tasks[0].Execute.Script, "echo ${FOO}")
	}
}

func TestExtractVarTraversal_EdgeCases(t *testing.T) {
	// len != 2
	_, err := extractVarTraversal(hcl.Traversal{
		hcl.TraverseRoot{Name: "var"},
	})
	if err == nil {
		t.Fatal("expected error for 1-element traversal")
	}

	// root != var
	_, err = extractVarTraversal(hcl.Traversal{
		hcl.TraverseRoot{Name: "other"},
		hcl.TraverseAttr{Name: "field"},
	})
	if err == nil {
		t.Fatal("expected error for non-var root")
	}

	// second step not TraverseAttr
	_, err = extractVarTraversal(hcl.Traversal{
		hcl.TraverseRoot{Name: "var"},
		hcl.TraverseIndex{Key: cty.Zero},
	})
	if err == nil {
		t.Fatal("expected error for index traversal step")
	}

	// valid
	name, err := extractVarTraversal(hcl.Traversal{
		hcl.TraverseRoot{Name: "var"},
		hcl.TraverseAttr{Name: "field"},
	})
	if err != nil || name != "field" {
		t.Fatalf("expected field, got %q, err: %v", name, err)
	}
}

func TestEvalInterpolatedString_EdgeCases(t *testing.T) {
	vc := buildVarEvalContext(nil)

	// nil expr
	s, err := evalInterpolatedString(nil, vc, "image")
	if err != nil || s != "" {
		t.Fatalf("expected empty string and nil err, got %q, %v", s, err)
	}

	// unsupported expression (tuple when string expected)
	tc := &hclsyntax.TupleConsExpr{}
	_, err = evalInterpolatedString(tc, vc, "image")
	if err == nil {
		t.Fatal("expected error for TupleConsExpr in evalInterpolatedString")
	}

	// object literal in evalInterpolatedString
	obj := &hclsyntax.ObjectConsExpr{}
	_, err = evalInterpolatedString(obj, vc, "image")
	if err == nil || !strings.Contains(err.Error(), "object literals") {
		t.Fatalf("expected object literals error, got %v", err)
	}
}

func TestDecodeBlock_NilVctx(t *testing.T) {
	body := &hclsyntax.Body{
		Attributes: map[string]*hclsyntax.Attribute{
			"path": {
				Name: "path",
				Expr: &hclsyntax.LiteralValueExpr{Val: cty.StringVal("local")},
			},
		},
	}
	r, err := decodeRepoBlock(&hclsyntax.Block{Type: "ci_repo", Labels: []string{"r"}, Body: body})
	if err != nil {
		t.Fatalf("decodeRepoBlock without vctx failed: %v", err)
	}
	if r.Path != "local" {
		t.Errorf("r.Path = %q, want local", r.Path)
	}

	taskBody := &hclsyntax.Body{
		Attributes: map[string]*hclsyntax.Attribute{
			"trigger": {
				Name: "trigger",
				Expr: &hclsyntax.LiteralValueExpr{Val: cty.StringVal("manual")},
			},
		},
	}
	task, err := decodeTaskBlock(&hclsyntax.Block{Type: "ci_task", Labels: []string{"t"}, Body: taskBody})
	if err != nil {
		t.Fatalf("decodeTaskBlock without vctx failed: %v", err)
	}
	if task.Trigger != "manual" {
		t.Errorf("task.Trigger = %q, want manual", task.Trigger)
	}
}

func TestExistingExamplePipelinesParse(t *testing.T) {
	examplePaths := []string{
		"../../examples/demo-mono/pipeline.tf",
		"../../examples/enterprise-saas/pipeline.tf",
		"../../examples/multi-service/pipeline.tf",
		"../../examples/remote-repository/pipeline.tf",
		"../../examples/simple-webservice/pipeline.tf",
	}
	for _, p := range examplePaths {
		t.Run(filepath.Base(filepath.Dir(p)), func(t *testing.T) {
			proj, err := ParseFile(p)
			if err != nil {
				t.Fatalf("ParseFile(%s) failed: %v", p, err)
			}
			if proj.Name == "" {
				t.Fatalf("ParseFile(%s) returned empty project name", p)
			}
		})
	}
}

func TestInterpolation_PipelineDefRunSnapshot(t *testing.T) {
	// First run with alpine = "3.20"
	srcRun1 := `project "my-proj" {
  variable "alpine" {
    value = "3.20"
  }
  ci_task "test" {
    trigger = "repo.app.changed"
    execute {
      image  = "alpine:${var.alpine}"
      script = "echo ok"
    }
  }
}`
	proj1, err := ParseSource([]byte(srcRun1), "pipeline.tf")
	if err != nil {
		t.Fatalf("ParseSource run1: %v", err)
	}
	if proj1.Tasks[0].Execute.Image != "alpine:3.20" {
		t.Fatalf("run1 image = %q, want alpine:3.20", proj1.Tasks[0].Execute.Image)
	}

	// Verify serialized snapshot (pipeline_def JSONB)
	snap1, err := json.Marshal(proj1)
	if err != nil {
		t.Fatalf("marshal proj1: %v", err)
	}
	if !strings.Contains(string(snap1), `"image":"alpine:3.20"`) {
		t.Fatalf("snapshot1 does not contain resolved image: %s", snap1)
	}

	// Second run after changing variable to "3.21"
	srcRun2 := `project "my-proj" {
  variable "alpine" {
    value = "3.21"
  }
  ci_task "test" {
    trigger = "repo.app.changed"
    execute {
      image  = "alpine:${var.alpine}"
      script = "echo ok"
    }
  }
}`
	proj2, err := ParseSource([]byte(srcRun2), "pipeline.tf")
	if err != nil {
		t.Fatalf("ParseSource run2: %v", err)
	}
	if proj2.Tasks[0].Execute.Image != "alpine:3.21" {
		t.Fatalf("run2 image = %q, want alpine:3.21", proj2.Tasks[0].Execute.Image)
	}

	snap2, err := json.Marshal(proj2)
	if err != nil {
		t.Fatalf("marshal proj2: %v", err)
	}
	if !strings.Contains(string(snap2), `"image":"alpine:3.21"`) {
		t.Fatalf("snapshot2 does not contain resolved image: %s", snap2)
	}
}
