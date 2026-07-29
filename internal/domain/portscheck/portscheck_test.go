// Package portscheck verifies at compile time that each adapter implements
// its port interface. Lives in an external test package to avoid import
// cycles (store imports domain; domain tests can't import store).
package portscheck

import (
	"os/exec"
	"strings"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/runner"
	"gitlab.com/lunaris-bot/metaci/internal/store"
)

func TestCompileTime_ProjectStore_FileStore(t *testing.T) {
	var _ domain.ProjectStore = (*store.FileStore)(nil)
}

func TestDomainHasNoInternalImports(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", "gitlab.com/lunaris-bot/metaci/internal/domain").Output()
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

func TestCompileTime_ProjectStore_PGStore(t *testing.T) {
	var _ domain.ProjectStore = (*store.PGStore)(nil)
}

func TestCompileTime_RunStore_PGStore(t *testing.T) {
	var _ domain.RunStore = (*store.PGStore)(nil)
}
