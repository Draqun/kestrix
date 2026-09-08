// Package domain defines the ports (interfaces) and shared types for the
// Kestrix CI/CD control plane. It is the innermost layer of the hexagonal
// architecture: it imports no other internal package and depends only on
// the Go standard library.
package domain

import (
	"time"
)

// Repo represents a source repository participating in a project.
type Repo struct {
	Name          string `hcl:"name,label" json:"name"`
	Path          string `hcl:"path,optional" json:"path,omitempty"`
	Kind          string `hcl:"kind,optional" json:"kind,omitempty"`
	URL           string `hcl:"url,optional" json:"url,omitempty"`
	Ref           string `hcl:"ref,optional" json:"ref,omitempty"`
	CredentialRef string `hcl:"credential_ref,optional" json:"credential_ref,omitempty"`
}

// ExecuteSpec defines what a task runs and where.
type ExecuteSpec struct {
	Repo   string `hcl:"repo" json:"repo"`
	Script string `hcl:"script" json:"script"`
	Image  string `hcl:"image,optional" json:"image,omitempty"`
}

// Task is a unit of pipeline work.
type Task struct {
	Name      string      `hcl:"name,label" json:"name"`
	Trigger   string      `hcl:"trigger" json:"trigger"`
	Execute   ExecuteSpec `hcl:"execute,block" json:"execute"`
	DependsOn []string    `hcl:"depends_on,optional" json:"depends_on,omitempty"`
	Manual    bool        `hcl:"manual,optional" json:"manual"`
}

// Variable is a pipeline variable with optional repository/job scope.
type Variable struct {
	Key       string `hcl:"key,label" json:"key"`
	Value     string `hcl:"value,optional" json:"value,omitempty"`
	Default   string `hcl:"default,optional" json:"default,omitempty"`
	Scope     string `hcl:"scope,optional" json:"scope,omitempty"`
	Repo      string `hcl:"repo,optional" json:"repo,omitempty"`
	Job       string `hcl:"job,optional" json:"job,omitempty"`
	Protected bool   `hcl:"protected,optional" json:"protected"`
	Masked    bool   `hcl:"masked,optional" json:"masked"`
	Env       string `hcl:"env,optional" json:"env,omitempty"`
}

// Project is the aggregate parsed from a pipeline definition.
type Project struct {
	Name         string     `hcl:"name,label" json:"name"`
	Repos        []Repo     `hcl:"ci_repo,block" json:"repos"`
	Tasks        []Task     `hcl:"ci_task,block" json:"tasks"`
	DefaultImage string     `hcl:"default_image,optional" json:"default_image"`
	Variables    []Variable `hcl:"variable,block" json:"variables,omitempty"`
}

// ProjectFile is the root HCL project block.
type ProjectFile struct {
	Project Project `hcl:"project,block"`
}

// Role is a Kestrix permission level. Roles are ordered: a higher role
// includes every permission of the roles below it.
type Role string

// Global and per-project roles, from least to most privileged.
const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

// roleRank orders the known roles. A role absent from this map (including
// the zero value) ranks below every known role.
var roleRank = map[Role]int{
	RoleViewer:   1,
	RoleOperator: 2,
	RoleAdmin:    3,
}

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	_, ok := roleRank[r]
	return ok
}

// AtLeast reports whether r grants at least the permissions of other. An
// unrecognized role satisfies no requirement, including itself.
func (r Role) AtLeast(other Role) bool {
	rr, ok := roleRank[r]
	if !ok {
		return false
	}
	return rr >= roleRank[other]
}

// HigherRole returns whichever of a and b ranks higher. An unrecognized role
// never outranks a known one.
func HigherRole(a, b Role) Role {
	if roleRank[a] >= roleRank[b] {
		return a
	}
	return b
}

// User is a Kestrix account. It is safe to return from any API response or
// CLI output: it never carries a password hash.
type User struct {
	Name string `json:"name"`
	Role Role   `json:"role"`
	// TokenVersion is compared against a token's ver claim on every request;
	// see UserStore.BumpTokenVersion. It is internal bookkeeping and is not
	// serialized to API responses or CLI output.
	TokenVersion int       `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserCredential pairs a user's identity with its stored password hash. It
// must never be serialized to an API response or CLI output.
type UserCredential struct {
	User
	PasswordHash string
}

// ProjectGrant assigns a user a role scoped to one project. It never lowers
// the permissions the user's global role already provides; see
// domain.HigherRole.
type ProjectGrant struct {
	UserName    string `json:"user_name"`
	ProjectName string `json:"project_name"`
	Role        Role   `json:"role"`
}

// IdentityCacheEntry is what IdentityCache stores for one account name: just
// enough to decide a request without a store round trip. Exists is false for
// a cached negative result (an account name that does not exist), which is
// cached too so a token for a deleted account does not become a free store
// query per request until it expires.
type IdentityCacheEntry struct {
	Exists       bool
	Role         Role
	TokenVersion int
}

// Claims is the verified identity carried by an authentication token,
// produced only by TokenIssuer.Verify. Role reflects the account's stored
// role at the moment RequireAuth resolved it, not necessarily the role the
// token was issued with; see design.md D1.
type Claims struct {
	UserName     string
	Role         Role
	TokenVersion int
	ExpiresAt    time.Time
}

// TaskResult captures the outcome of a single task execution.
// It is the single source of truth for task results across the runner,
// store, and service layers.
type TaskResult struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"` // passed, failed, skipped, running, pending, manual
	Output   string        `json:"output"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration" swaggertype:"primitive,integer"`
	// ApprovedBy is the actor that approved this manual task, "user:<name>".
	// Empty for a task never approved - see run-attribution D4.
	ApprovedBy string `json:"approved_by,omitempty"`
	// ApprovedAt is when ApprovedBy approved the task. nil for a task never
	// approved.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
}
