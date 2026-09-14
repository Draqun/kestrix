package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// routeRoleCases enumerates every protected /api route with the minimum
// role access-control's spec assigns it (see server.go buildRouter): reads
// need viewer, run/pipeline writes need operator, and account/credential/
// project-lifecycle administration needs admin. It is the single source the
// tests below walk, so a route added to buildRouter without an entry here
// fails loudly instead of silently going unchecked.
var routeRoleCases = []struct {
	method  string
	path    string
	minRole domain.Role
}{
	{"GET", "/api/dashboard", domain.RoleViewer},
	{"GET", "/api/pipeline", domain.RoleViewer},
	{"GET", "/api/pipeline/raw", domain.RoleViewer},
	{"GET", "/api/projects", domain.RoleViewer},
	{"GET", "/api/projects/demo", domain.RoleViewer},
	{"GET", "/api/projects/demo/definition", domain.RoleViewer},
	{"GET", "/api/projects/demo/pipeline", domain.RoleViewer},
	{"POST", "/api/projects/demo/resolve", domain.RoleViewer},
	{"GET", "/api/projects/demo/runs", domain.RoleViewer},
	{"GET", "/api/projects/demo/runs/1", domain.RoleViewer},
	{"GET", "/api/runs", domain.RoleViewer},
	{"POST", "/api/resolve", domain.RoleViewer},
	{"POST", "/api/generate", domain.RoleViewer},
	{"GET", "/api/validate", domain.RoleViewer},
	{"GET", "/api/logs", domain.RoleViewer},
	{"GET", "/api/version", domain.RoleViewer},

	{"POST", "/api/projects/demo/pipeline", domain.RoleOperator},
	{"POST", "/api/projects/demo/run", domain.RoleOperator},
	{"POST", "/api/projects/demo/tasks/build/run", domain.RoleOperator},
	{"POST", "/api/projects/demo/runs/1/cancel", domain.RoleOperator},
	{"POST", "/api/projects/demo/runs/1/tasks/build/run", domain.RoleOperator},

	{"GET", "/api/credentials", domain.RoleAdmin},
	{"POST", "/api/credentials", domain.RoleAdmin},
	{"GET", "/api/credentials/ref", domain.RoleAdmin},
	{"PUT", "/api/credentials/ref", domain.RoleAdmin},
	{"POST", "/api/credentials/ref/revoke", domain.RoleAdmin},
	{"POST", "/api/projects", domain.RoleAdmin},
	{"PUT", "/api/projects/demo", domain.RoleAdmin},
	{"DELETE", "/api/projects/demo", domain.RoleAdmin},
	{"GET", "/api/users", domain.RoleAdmin},
	{"POST", "/api/users", domain.RoleAdmin},
	{"PUT", "/api/users/alice/role", domain.RoleAdmin},
	{"PUT", "/api/users/alice/grants", domain.RoleAdmin},
	{"DELETE", "/api/users/alice", domain.RoleAdmin},
}

func TestRouteRoles_RejectMissingToken(t *testing.T) {
	srv := NewServer(Config{})
	for _, tc := range routeRoleCases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 without a token", w.Code)
			}
		})
	}
}

func TestRouteRoles_EnforceMinimumRole(t *testing.T) {
	srv := NewServer(Config{})
	roles := []domain.Role{domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin}
	for _, tc := range routeRoleCases {
		for _, callerRole := range roles {
			t.Run(tc.method+" "+tc.path+"/as-"+string(callerRole), func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				req.Header.Set("Authorization", authToken(t, srv, callerRole))
				w := httptest.NewRecorder()
				srv.router.ServeHTTP(w, req)
				if callerRole.AtLeast(tc.minRole) {
					if w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
						t.Fatalf("status = %d, want role %s to pass the %s minimum (body=%s)", w.Code, callerRole, tc.minRole, w.Body.String())
					}
				} else {
					if w.Code != http.StatusForbidden {
						t.Fatalf("status = %d, want 403 for role %s below the %s minimum", w.Code, callerRole, tc.minRole)
					}
				}
			})
		}
	}
}

// TestRouteRoles_ProjectGrantDoesNotAuthorizeAccountRoutes pins the fix for a
// privilege-escalation finding: account routes (/api/users/{user}/...) and
// project routes (/api/projects/{name}/...) both had a URL segment RequireRole
// read to resolve a grant. When both segments were literally named "name",
// an admin-level grant on a project happening to share a name with some
// user account silently authorized admin-only account management for that
// account. The routes now use a distinct "{user}" segment so no project
// grant is ever consulted for them.
func TestRouteRoles_ProjectGrantDoesNotAuthorizeAccountRoutes(t *testing.T) {
	srv := NewServer(Config{})
	const collidingName = "shared-name"
	srv.grantStore.(*mocks.MockGrantStore).Grants[[2]string{"caller", collidingName}] = domain.RoleAdmin

	user := ensureTestUser(t, srv, "caller", domain.RoleViewer)
	token, _, err := srv.tokenIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/users/"+collidingName+"/role", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (a project grant must never authorize an account route, even one sharing the project's name)", w.Code)
	}
}

func TestRouteRoles_HealthAndLoginArePublic(t *testing.T) {
	srv := NewServer(Config{})
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/health"},
		{"POST", "/api/auth/login"},
		{"GET", "/api/setup"},
		{"POST", "/api/setup"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)
		if w.Code == http.StatusUnauthorized {
			t.Fatalf("%s %s: status = 401, want reachable without a token", tc.method, tc.path)
		}
	}
}
