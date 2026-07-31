package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// repoRoot returns the repository root by walking up from the test's
// working directory (internal/web/) until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find go.mod walking up from " + mustGetwd())
	return ""
}

func mustGetwd() string {
	dir, _ := os.Getwd()
	return dir
}

func TestHandlerCoverageAbove85(t *testing.T) {
	if os.Getenv("KESTRIX_COV_SUB") == "1" {
		t.Skip("skipping recursive coverage check in subprocess")
	}

	root := repoRoot(t)
	profile := t.TempDir() + "/coverage.out"

	cmd := exec.Command("go", "test", "-coverprofile="+profile, "-count=1", "./internal/web/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "KESTRIX_COV_SUB=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	cmd2 := exec.Command("go", "tool", "cover", "-func", profile)
	cmd2.Dir = root
	funcOut, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("go tool cover failed: %v\n%s", err, funcOut)
	}

	re := regexp.MustCompile(`total:\s+\(statements\)\s+([\d.]+)%`)
	m := re.FindSubmatch(funcOut)
	if m == nil {
		t.Fatalf("could not parse coverage from:\n%s", funcOut)
	}

	pct, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil {
		t.Fatalf("could not parse coverage percentage %q: %v", m[1], err)
	}

	if pct < 85.0 {
		t.Errorf("handler coverage %.1f%% is below 85%% threshold\n%s", pct, funcOut)
	} else {
		t.Logf("handler coverage %.1f%% (threshold 85%%)", pct)
	}
}
