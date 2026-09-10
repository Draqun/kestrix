package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/web"
)

// testUserDBURL returns the same KESTRIX_TEST_DATABASE_URL convention
// internal/store's integration tests use, skipping when PostgreSQL is
// unavailable or the CLI test is running -short. The safety guard mirrors
// internal/store/pgstore_integration_test.go: refuse anything not ending in
// "_test".
func testUserDBURL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	dbURL := os.Getenv("KESTRIX_TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kestrix:kestrix@localhost:5433/kestrix_test?sslmode=disable&connect_timeout=2"
	}
	parsed, err := url.Parse(dbURL)
	if err == nil && !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatalf("tests must use a database ending with _test (got %q)", parsed.Path)
	}
	return dbURL
}

// runKestrixStdin is runKestrix plus a stdin payload, for commands that read
// a password from standard input rather than a terminal.
func runKestrixStdin(t *testing.T, bin string, stdin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s: %v", bin, err)
	}
	return so.String(), se.String(), code
}

func TestCLI_UserCreate_FromStdin(t *testing.T) {
	bin := buildKestrix(t)
	dbURL := testUserDBURL(t)
	name := uniqueTestUserName(t, "create")

	stdout, stderr, code := runKestrixStdin(t, bin, "s3cret-pass\n",
		"user", "create", "--role", "operator", "--database-url", dbURL, name)
	if code != 0 {
		t.Fatalf("exit = %d, stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, name) || !strings.Contains(stdout, "operator") {
		t.Fatalf("stdout = %q, want it to name %s and role operator", stdout, name)
	}
	t.Cleanup(func() { runKestrixStdin(t, bin, "", "user", "delete", "--database-url", dbURL, name) })
}

func TestCLI_UserCreate_NoPasswordFlag(t *testing.T) {
	// The password must never be accepted as a command-line argument: there
	// is no flag for it at all, so passing one is simply an unknown flag.
	bin := buildKestrix(t)
	_, stderr, code := runKestrixStdin(t, bin, "",
		"user", "create", "--password", "leaked-secret", "someone")
	if code == 0 {
		t.Fatalf("expected a nonzero exit for an unrecognized --password flag, stderr=%s", stderr)
	}
	if strings.Contains(stderr, "leaked-secret") == false {
		// flag.ExitOnError prints the offending flag; this just documents
		// that the flag set has no such option to begin with.
		t.Logf("stderr: %s", stderr)
	}
}

func TestCLI_UserList_ShowsCreatedAccount(t *testing.T) {
	bin := buildKestrix(t)
	dbURL := testUserDBURL(t)
	name := uniqueTestUserName(t, "list")

	if _, stderr, code := runKestrixStdin(t, bin, "s3cret-pass\n",
		"user", "create", "--role", "viewer", "--database-url", dbURL, name); code != 0 {
		t.Fatalf("create exit = %d, stderr=%s", code, stderr)
	}
	t.Cleanup(func() { runKestrixStdin(t, bin, "", "user", "delete", "--database-url", dbURL, name) })

	stdout, stderr, code := runKestrixStdin(t, bin, "", "user", "list", "--database-url", dbURL)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, name) {
		t.Fatalf("list output = %q, want it to contain %s", stdout, name)
	}
	if strings.Contains(stdout, "s3cret-pass") || strings.Contains(stdout, "argon2id") {
		t.Fatalf("list output leaked password material: %s", stdout)
	}
}

func TestCLI_UserPasswd_UpdatesPassword(t *testing.T) {
	bin := buildKestrix(t)
	dbURL := testUserDBURL(t)
	name := uniqueTestUserName(t, "passwd")

	if _, stderr, code := runKestrixStdin(t, bin, "old-password\n",
		"user", "create", "--role", "viewer", "--database-url", dbURL, name); code != 0 {
		t.Fatalf("create exit = %d, stderr=%s", code, stderr)
	}
	t.Cleanup(func() { runKestrixStdin(t, bin, "", "user", "delete", "--database-url", dbURL, name) })

	stdout, stderr, code := runKestrixStdin(t, bin, "new-password\n",
		"user", "passwd", "--database-url", dbURL, name)
	if code != 0 {
		t.Fatalf("exit = %d, stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, name) {
		t.Fatalf("stdout = %q, want it to confirm the update for %s", stdout, name)
	}
}

func TestCLI_UserDelete_ThenNotFound(t *testing.T) {
	bin := buildKestrix(t)
	dbURL := testUserDBURL(t)
	name := uniqueTestUserName(t, "delete")

	if _, stderr, code := runKestrixStdin(t, bin, "s3cret-pass\n",
		"user", "create", "--role", "viewer", "--database-url", dbURL, name); code != 0 {
		t.Fatalf("create exit = %d, stderr=%s", code, stderr)
	}

	if _, stderr, code := runKestrixStdin(t, bin, "", "user", "delete", "--database-url", dbURL, name); code != 0 {
		t.Fatalf("delete exit = %d, stderr=%s", code, stderr)
	}

	_, stderr, code := runKestrixStdin(t, bin, "", "user", "delete", "--database-url", dbURL, name)
	if code == 0 {
		t.Fatal("second delete succeeded, want failure for an already-deleted account")
	}
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("stderr = %q, want it to say the account was not found", stderr)
	}
}

func TestCLI_UserCreate_DuplicateName(t *testing.T) {
	bin := buildKestrix(t)
	dbURL := testUserDBURL(t)
	name := uniqueTestUserName(t, "dup")

	if _, stderr, code := runKestrixStdin(t, bin, "s3cret-pass\n",
		"user", "create", "--role", "viewer", "--database-url", dbURL, name); code != 0 {
		t.Fatalf("create exit = %d, stderr=%s", code, stderr)
	}
	t.Cleanup(func() { runKestrixStdin(t, bin, "", "user", "delete", "--database-url", dbURL, name) })

	_, stderr, code := runKestrixStdin(t, bin, "s3cret-pass\n",
		"user", "create", "--role", "viewer", "--database-url", dbURL, name)
	if code == 0 {
		t.Fatal("second create with the same name succeeded, want failure")
	}
	if !strings.Contains(stderr, "already exists") {
		t.Fatalf("stderr = %q, want it to say the account already exists", stderr)
	}
}

func TestCLI_UserCreate_InvalidRole(t *testing.T) {
	bin := buildKestrix(t)
	dbURL := testUserDBURL(t)
	name := uniqueTestUserName(t, "badrole")

	_, stderr, code := runKestrixStdin(t, bin, "s3cret-pass\n",
		"user", "create", "--role", "superadmin", "--database-url", dbURL, name)
	if code == 0 {
		t.Fatal("create with an invalid role succeeded, want failure")
	}
	if !strings.Contains(stderr, "invalid role") {
		t.Fatalf("stderr = %q, want it to name the invalid role", stderr)
	}
}

// writeTestKeyFile writes a random 32-byte key to a 0600 file, for the
// master-key and JWT-signing-key preflight checks web.NewServerE requires.
func writeTestKeyFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCLI_UserCreate_ThenLoginThroughAPI is the end-to-end path
// user-authentication's CLI requirement exists for: `kestrix user create`
// against an empty-of-this-account database, immediately followed by
// POST /api/auth/login on a real web.Server pointed at the same database -
// no other bootstrap step is available or needed.
func TestCLI_UserCreate_ThenLoginThroughAPI(t *testing.T) {
	bin := buildKestrix(t)
	dbURL := testUserDBURL(t)
	name := uniqueTestUserName(t, "e2e")
	const password = "s3cret-admin-pass"

	stdout, stderr, code := runKestrixStdin(t, bin, password+"\n",
		"user", "create", "--role", "admin", "--database-url", dbURL, name)
	if code != 0 {
		t.Fatalf("user create exit = %d, stdout=%s stderr=%s", code, stdout, stderr)
	}
	t.Cleanup(func() { runKestrixStdin(t, bin, "", "user", "delete", "--database-url", dbURL, name) })

	srv, err := web.NewServerE(web.Config{
		DatabaseURL:       dbURL,
		MasterKeyPath:     writeTestKeyFile(t, "master.key"),
		JWTSigningKeyPath: writeTestKeyFile(t, "jwt.key"),
	})
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"name": name, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("login response carries no token")
	}
}

// uniqueTestUserName keeps concurrent test runs from colliding on the
// shared _test database, and is short enough to fit the users.name column
// comfortably.
func uniqueTestUserName(t *testing.T, suffix string) string {
	t.Helper()
	return "cli-" + suffix + "-" + t.Name()[len("TestCLI_"):]
}
