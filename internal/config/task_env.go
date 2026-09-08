package config

import "sort"

// taskEnvBase lists the host variables a task process receives regardless of
// passthrough configuration - the minimum a shell needs to run `go build`,
// `npm`, or `git` without looking like a sandboxing bug. See design.md D1.
var taskEnvBase = []string{"PATH", "HOME", "LANG", "LC_ALL", "TMPDIR", "TERM", "USER"}

// TaskEnv builds the explicit environment a task process may observe: the
// fixed base set (when present in the host environment), the CI_* entries
// the caller supplies, the configured passthrough names, then vars last so
// pipeline-declared variables win on collision. It never includes the full
// process environment or the Kestrix-prefixed map; see the
// task-execution-isolation capability.
func (e *Environment) TaskEnv(ci map[string]string, vars map[string]string) []string {
	order := make([]string, 0, len(taskEnvBase)+len(ci)+len(vars))
	values := make(map[string]string, len(taskEnvBase)+len(ci)+len(vars))
	set := func(name, value string) {
		if _, exists := values[name]; !exists {
			order = append(order, name)
		}
		values[name] = value
	}

	if e != nil {
		for _, name := range taskEnvBase {
			if value, ok := e.values[name]; ok {
				set(name, value)
			}
		}
	}
	for _, name := range sortedKeys(ci) {
		set(name, ci[name])
	}
	if e != nil {
		for _, name := range e.RunnerEnvPassthrough {
			if value, ok := e.values[name]; ok {
				set(name, value)
			}
		}
	}
	for _, name := range sortedKeys(vars) {
		set(name, vars[name])
	}

	result := make([]string, 0, len(order))
	for _, name := range order {
		result = append(result, name+"="+values[name])
	}
	return result
}

// AllowlistNames returns the names (never values) of variables a task
// process may observe: the fixed base set, the CI_* wildcard Kestrix sets
// per task, and the configured passthrough names. It is printed once per
// run before the first task starts.
func (e *Environment) AllowlistNames() []string {
	names := make([]string, 0, len(taskEnvBase)+1)
	names = append(names, taskEnvBase...)
	names = append(names, "CI_*")
	if e != nil {
		names = append(names, e.RunnerEnvPassthrough...)
	}
	return names
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
