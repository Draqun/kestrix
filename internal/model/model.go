// Package model provides compatibility aliases for the domain entities.
// New code should import internal/domain directly.
package model

import "kestrix/internal/domain"

type Repo = domain.Repo
type ExecuteSpec = domain.ExecuteSpec
type Task = domain.Task
type Project = domain.Project
type ProjectFile = domain.ProjectFile
type Variable = domain.Variable
type AcquiredSource = domain.AcquiredSource
