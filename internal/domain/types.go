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
	Name string `hcl:"name,label" json:"name"`
	Path string `hcl:"path" json:"path"`
	Kind string `hcl:"kind,optional" json:"kind"`
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

// TaskResult captures the outcome of a single task execution.
// It is the single source of truth for task results across the runner,
// store, and service layers.
type TaskResult struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"` // passed, failed, skipped, running, pending, manual
	Output   string        `json:"output"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration" swaggertype:"primitive,integer"`
}
