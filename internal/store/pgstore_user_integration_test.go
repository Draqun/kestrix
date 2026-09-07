// PGStore user and project-grant integration tests. See
// pgstore_integration_test.go for the connection and safety-guard setup.
package store

import (
	"errors"
	"testing"

	"kestrix/internal/domain"
)

// ── UserStore ──────────────────────────────────────────────────────

func TestPG_CreateAndGetUser(t *testing.T) {
	pg := testPG(t)
	created, err := pg.CreateUser("alice", domain.RoleOperator, "hash-of-alices-password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.Name != "alice" || created.Role != domain.RoleOperator {
		t.Fatalf("created = %+v, want alice/operator", created)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero")
	}

	got, err := pg.GetUser("alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got != created {
		t.Fatalf("GetUser = %+v, want %+v", got, created)
	}
}

func TestPG_CreateUser_DuplicateNameRejected(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("bob", domain.RoleViewer, "hash-1"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	if _, err := pg.CreateUser("bob", domain.RoleAdmin, "hash-2"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("second CreateUser error = %v, want ErrUserExists", err)
	}

	// The existing account must be untouched.
	got, err := pg.GetUser("bob")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Role != domain.RoleViewer {
		t.Fatalf("existing account role = %s, want unchanged viewer", got.Role)
	}
}

func TestPG_GetUser_NotFound(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.GetUser("nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUser error = %v, want ErrUserNotFound", err)
	}
}

func TestPG_GetUserCredential_CarriesHashNoOtherQueryDoes(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("carol", domain.RoleAdmin, "the-actual-hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	uc, err := pg.GetUserCredential("carol")
	if err != nil {
		t.Fatalf("GetUserCredential: %v", err)
	}
	if uc.PasswordHash != "the-actual-hash" {
		t.Fatalf("PasswordHash = %q, want the-actual-hash", uc.PasswordHash)
	}

	got, err := pg.GetUser("carol")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	// domain.User has no password-hash field at all - the type itself
	// proves GetUser cannot expose one.
	_ = got

	list, err := pg.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(list) != 1 || list[0].Name != "carol" {
		t.Fatalf("ListUsers = %+v, want one entry for carol", list)
	}
}

func TestPG_ListUsers_OrderedByName(t *testing.T) {
	pg := testPG(t)
	for _, name := range []string{"zed", "amy", "mia"} {
		if _, err := pg.CreateUser(name, domain.RoleViewer, "hash"); err != nil {
			t.Fatalf("CreateUser(%q): %v", name, err)
		}
	}
	list, err := pg.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(list) != 3 || list[0].Name != "amy" || list[1].Name != "mia" || list[2].Name != "zed" {
		t.Fatalf("ListUsers order = %+v, want amy, mia, zed", list)
	}
}

func TestPG_UpdateUserRole(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("dave", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := pg.UpdateUserRole("dave", domain.RoleAdmin); err != nil {
		t.Fatalf("UpdateUserRole: %v", err)
	}
	got, err := pg.GetUser("dave")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Role != domain.RoleAdmin {
		t.Fatalf("role = %s, want admin", got.Role)
	}
}

func TestPG_UpdateUserRole_NotFound(t *testing.T) {
	pg := testPG(t)
	if err := pg.UpdateUserRole("ghost", domain.RoleAdmin); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UpdateUserRole error = %v, want ErrUserNotFound", err)
	}
}

func TestPG_UpdateUserPassword(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("erin", domain.RoleOperator, "old-hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := pg.UpdateUserPassword("erin", "new-hash"); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	uc, err := pg.GetUserCredential("erin")
	if err != nil {
		t.Fatalf("GetUserCredential: %v", err)
	}
	if uc.PasswordHash != "new-hash" {
		t.Fatalf("PasswordHash = %q, want new-hash", uc.PasswordHash)
	}
}

func TestPG_DeleteUser(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("frank", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := pg.DeleteUser("frank"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := pg.GetUser("frank"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUser after delete error = %v, want ErrUserNotFound", err)
	}
}

func TestPG_DeleteUser_NotFound(t *testing.T) {
	pg := testPG(t)
	if err := pg.DeleteUser("ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("DeleteUser error = %v, want ErrUserNotFound", err)
	}
}

// ── GrantStore ─────────────────────────────────────────────────────

func TestPG_SetAndResolveGrant(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("gina", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	mustUpsert(t, pg, "alpha", "")

	if err := pg.SetProjectGrant("gina", "alpha", domain.RoleOperator); err != nil {
		t.Fatalf("SetProjectGrant: %v", err)
	}
	role, found, err := pg.ResolveGrant("gina", "alpha")
	if err != nil {
		t.Fatalf("ResolveGrant: %v", err)
	}
	if !found || role != domain.RoleOperator {
		t.Fatalf("ResolveGrant = (%s, %v), want (operator, true)", role, found)
	}
}

func TestPG_ResolveGrant_NotFoundDistinctFromZeroRole(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("hank", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	mustUpsert(t, pg, "beta", "")

	role, found, err := pg.ResolveGrant("hank", "beta")
	if err != nil {
		t.Fatalf("ResolveGrant: %v", err)
	}
	if found || role != "" {
		t.Fatalf("ResolveGrant = (%q, %v), want (\"\", false)", role, found)
	}
}

func TestPG_SetProjectGrant_Overwrites(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("iris", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	mustUpsert(t, pg, "gamma", "")

	if err := pg.SetProjectGrant("iris", "gamma", domain.RoleOperator); err != nil {
		t.Fatalf("first SetProjectGrant: %v", err)
	}
	if err := pg.SetProjectGrant("iris", "gamma", domain.RoleAdmin); err != nil {
		t.Fatalf("second SetProjectGrant: %v", err)
	}
	role, found, err := pg.ResolveGrant("iris", "gamma")
	if err != nil {
		t.Fatalf("ResolveGrant: %v", err)
	}
	if !found || role != domain.RoleAdmin {
		t.Fatalf("ResolveGrant = (%s, %v), want (admin, true)", role, found)
	}
}

func TestPG_RemoveProjectGrant(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("jack", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	mustUpsert(t, pg, "delta", "")
	if err := pg.SetProjectGrant("jack", "delta", domain.RoleOperator); err != nil {
		t.Fatalf("SetProjectGrant: %v", err)
	}
	if err := pg.RemoveProjectGrant("jack", "delta"); err != nil {
		t.Fatalf("RemoveProjectGrant: %v", err)
	}
	_, found, err := pg.ResolveGrant("jack", "delta")
	if err != nil {
		t.Fatalf("ResolveGrant: %v", err)
	}
	if found {
		t.Fatal("grant still resolves after removal")
	}
}

func TestPG_RemoveProjectGrant_NotFound(t *testing.T) {
	pg := testPG(t)
	if err := pg.RemoveProjectGrant("nobody", "nowhere"); !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("RemoveProjectGrant error = %v, want ErrGrantNotFound", err)
	}
}

func TestPG_ListGrantsForUserAndProject(t *testing.T) {
	pg := testPG(t)
	for _, name := range []string{"kim"} {
		if _, err := pg.CreateUser(name, domain.RoleViewer, "hash"); err != nil {
			t.Fatalf("CreateUser(%q): %v", name, err)
		}
	}
	mustUpsert(t, pg, "epsilon", "")
	mustUpsert(t, pg, "zeta", "")
	if err := pg.SetProjectGrant("kim", "epsilon", domain.RoleOperator); err != nil {
		t.Fatalf("SetProjectGrant: %v", err)
	}
	if err := pg.SetProjectGrant("kim", "zeta", domain.RoleAdmin); err != nil {
		t.Fatalf("SetProjectGrant: %v", err)
	}

	byUser, err := pg.ListGrantsForUser("kim")
	if err != nil {
		t.Fatalf("ListGrantsForUser: %v", err)
	}
	if len(byUser) != 2 {
		t.Fatalf("ListGrantsForUser = %+v, want 2 entries", byUser)
	}

	byProject, err := pg.ListGrantsForProject("epsilon")
	if err != nil {
		t.Fatalf("ListGrantsForProject: %v", err)
	}
	if len(byProject) != 1 || byProject[0].UserName != "kim" {
		t.Fatalf("ListGrantsForProject = %+v, want one entry for kim", byProject)
	}
}

func TestPG_DeletingProject_RemovesItsGrants(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("lena", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	mustUpsert(t, pg, "eta", "")
	if err := pg.SetProjectGrant("lena", "eta", domain.RoleOperator); err != nil {
		t.Fatalf("SetProjectGrant: %v", err)
	}

	if err := pg.Delete("eta"); err != nil {
		t.Fatalf("Delete project: %v", err)
	}

	_, found, err := pg.ResolveGrant("lena", "eta")
	if err != nil {
		t.Fatalf("ResolveGrant: %v", err)
	}
	if found {
		t.Fatal("grant survived its project's deletion")
	}
}

func TestPG_DeletingUser_RemovesTheirGrants(t *testing.T) {
	pg := testPG(t)
	if _, err := pg.CreateUser("morgan", domain.RoleViewer, "hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	mustUpsert(t, pg, "theta", "")
	if err := pg.SetProjectGrant("morgan", "theta", domain.RoleOperator); err != nil {
		t.Fatalf("SetProjectGrant: %v", err)
	}

	if err := pg.DeleteUser("morgan"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	grants, err := pg.ListGrantsForProject("theta")
	if err != nil {
		t.Fatalf("ListGrantsForProject: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("grants survived user deletion: %+v", grants)
	}
}
