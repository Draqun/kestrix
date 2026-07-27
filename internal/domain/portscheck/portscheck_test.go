// Package portscheck verifies at compile time that each adapter implements
// its port interface. Lives in an external test package to avoid import
// cycles (store imports domain; domain tests can't import store).
package portscheck

import (
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/store"
)

func TestCompileTime_ProjectStore_FileStore(t *testing.T) {
	var _ domain.ProjectStore = (*store.FileStore)(nil)
}

func TestCompileTime_ProjectStore_PGStore(t *testing.T) {
	var _ domain.ProjectStore = (*store.PGStore)(nil)
}

func TestCompileTime_RunStore_PGStore(t *testing.T) {
	var _ domain.RunStore = (*store.PGStore)(nil)
}