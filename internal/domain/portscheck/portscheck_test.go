// Package portscheck verifies at compile time that each adapter implements
// its port interface. Lives in an external test package to avoid import
// cycles (store imports domain; domain tests can't import store).
package portscheck

import (
	"os/exec"
	"strings"
	"testing"

	"kestrix/internal/auth"
	"kestrix/internal/domain"
	"kestrix/internal/runner"
	"kestrix/internal/store"
)

func TestDomainHasNoInternalImports(t *testing.T) {
	modulePath, err := exec.Command("go", "list", "-m").Output()
	if err != nil {
		t.Fatalf("list module: %v", err)
	}
	packagePath := strings.TrimSpace(string(modulePath)) + "/internal/domain"
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", packagePath).Output()
	if err != nil {
		t.Fatalf("list domain imports: %v", err)
	}
	for _, imported := range strings.Fields(string(out)) {
		if strings.Contains(imported, "/internal/") {
			t.Fatalf("domain imports internal package %q", imported)
		}
	}
}

func TestNoPackageImportsLegacyModelShim(t *testing.T) {
	modulePathBytes, err := exec.Command("go", "list", "-m").Output()
	if err != nil {
		t.Fatalf("list module: %v", err)
	}
	mod := strings.TrimSpace(string(modulePathBytes))
	forbidden := mod + "/internal/model"

	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}: {{join .Imports \" \"}} {{join .TestImports \" \"}} {{join .XTestImports \" \"}}", mod+"/...").Output()
	if err != nil {
		t.Fatalf("list module packages: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		pkg := strings.TrimSpace(parts[0])
		if pkg == forbidden {
			t.Fatalf("legacy shim package %q still exists", forbidden)
		}
		if len(parts) < 2 {
			continue
		}
		for _, imp := range strings.Fields(parts[1]) {
			if imp == forbidden {
				t.Fatalf("package %q imports forbidden legacy shim %q", pkg, forbidden)
			}
		}
	}
}

func TestCompileTime_PipelineRunner(t *testing.T) {
	var _ domain.PipelineRunner = (*runner.PipelineExecutor)(nil)
}

func TestCompileTime_TaskRunner(t *testing.T) {
	var _ domain.TaskRunner = (*runner.PipelineExecutor)(nil)
}

func TestCompileTime_ProjectStore_PGStore(t *testing.T) {
	var _ domain.ProjectStore = (*store.PGStore)(nil)
}

func TestCompileTime_RunStore_PGStore(t *testing.T) {
	var _ domain.RunStore = (*store.PGStore)(nil)
}

func TestCompileTime_UserStore_PGStore(t *testing.T) {
	var _ domain.UserStore = (*store.PGStore)(nil)
}

func TestCompileTime_GrantStore_PGStore(t *testing.T) {
	var _ domain.GrantStore = (*store.PGStore)(nil)
}

func TestCompileTime_RemoteJobStore_PGStore(t *testing.T) {
	var _ domain.RemoteJobStore = (*store.PGStore)(nil)
}

func TestCompileTime_RunnerStore_PGStore(t *testing.T) {
	var _ domain.RunnerStore = (*store.PGStore)(nil)
}

func TestCompileTime_IdentityCache_InMemoryIdentityCache(t *testing.T) {
	var _ domain.IdentityCache = (*auth.InMemoryIdentityCache)(nil)
}
