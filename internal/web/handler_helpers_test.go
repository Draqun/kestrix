package web

import (
	"net/http/httptest"
	"testing"

	"kestrix/internal/domain"
)

func TestActorFrom_AuthenticatedRequest(t *testing.T) {
	r := requestWithClaims(domain.Claims{UserName: "alice", Role: domain.RoleOperator})
	if got := actorFrom(r); got != "user:alice" {
		t.Errorf("actorFrom = %q, want user:alice", got)
	}
}

func TestActorFrom_NoClaims(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	if got := actorFrom(r); got != "" {
		t.Errorf("actorFrom = %q, want empty string without claims", got)
	}
}
