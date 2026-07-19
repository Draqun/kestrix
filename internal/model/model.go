// Package model defines the core domain entities of MetaCI.
//
// These types represent the in-memory representation of a MetaCI project:
// repositories, tasks, trigger conditions, and the project itself that ties them
// together. They are populated by the HCL parser and consumed by the DAG engine.
package model

// Repo represents a source repository (or a path within a monorepo) that
// participates in a MetaCI project. It is the unit of change detection.
type Repo struct {
	// Name is the local identifier used in trigger expressions, e.g. "auth_service".
	// It corresponds to the HCL label: ci_repo "auth_service" { ... }
	Name string `hcl:"name,label"`

	// Path is the clone URL (multi-repo) or the subdirectory within a monorepo.
	// Examples:
	//   multi-repo: "https://gitlab.com/org/auth.git"
	//   monorepo:   "src/auth"
	Path string `hcl:"path"`

	// Kind indicates how this repo is resolved.
	// "git"     — remote clone URL (multi-repo)
	// "subpath" — local subdirectory within a monorepo workspace
	Kind string `hcl:"kind,optional"`
}

// ExecuteSpec defines what a task runs and where.
type ExecuteSpec struct {
	// Repo is the name of the ci_repo whose scripts should be executed.
	// This is a reference, not an inline definition. It must resolve to a Repo
	// declared in the same project.
	Repo string `hcl:"repo"`

	// Script is the command to run inside the repo's working directory.
	Script string `hcl:"script"`

	// Image is the container image to pull for this job (per-job dynamic image).
	// If empty, the project-level default image is used.
	Image string `hcl:"image,optional"`
}

// Task is a unit of work in the pipeline. It has a trigger condition (the logic
// that decides whether it should run) and an execution specification.
type Task struct {
	// Name is the HCL label: ci_task "test_integration" { ... }
	Name string `hcl:"name,label"`

	// Trigger is a boolean expression string evaluated by the DAG engine.
	// Syntax examples:
	//   "repo.auth_service.changed && !repo.docs.changed"
	//   "repo.a.changed || repo.b.changed"
	Trigger string `hcl:"trigger"`

	// Execute defines what to run and in which repo context.
	Execute ExecuteSpec `hcl:"execute,block"`

	// DependsOn lists tasks that must complete before this one starts.
	// This is separate from the trigger — trigger decides *whether* to run,
	// DependsOn decides *ordering* within the DAG.
	DependsOn []string `hcl:"depends_on,optional"`

	// Manual, when true, requires a human to click "Run" in the UI before the
	// task executes, even if its trigger condition is met.
	Manual bool `hcl:"manual,optional"`
}

// Project is the top-level container. It groups repos and tasks into a logical
// unit that represents a deployable system or product.
type Project struct {
	// Name is the HCL label: project "system-bankowy" { ... }
	Name string `hcl:"name,label"`

	// Repos is the set of repositories tracked by this project.
	Repos []Repo `hcl:"ci_repo,block"`

	// Tasks is the set of tasks that form the pipeline.
	Tasks []Task `hcl:"ci_task,block"`

	// DefaultImage is the fallback container image for tasks without an explicit
	// image.
	DefaultImage string `hcl:"default_image,optional"`
}

// ProjectFile is the *root* of an HCL file. It wraps the "project" block so
// gohcl.DecodeBody can match the top-level block. The parser converts this
// into a model.Project.
type ProjectFile struct {
	Project Project `hcl:"project,block"`
}
