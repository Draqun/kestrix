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

func TestCompileTime_IdentityCache_InMemoryIdentityCache(t *testing.T) {
	var _ domain.IdentityCache = (*auth.InMemoryIdentityCache)(nil)
}
