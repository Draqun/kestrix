package web

import (
	"kestrix/internal/model"
	"kestrix/internal/variables"
)

func addEnvironmentVariables(vs *variables.Store, entries []variables.Variable) {
	for _, v := range entries {
		vs.SetProject(v.Key, v.Value)
	}
}

// buildVarStore constructs a variables.Store from a project's own declared
// variables only.
//
// It used to also seed every KESTRIX_* environment variable (prefix
// stripped) and the configured Docker registry credentials unconditionally,
// via variables.FromKestrix() and s.cfg.Docker. Those variables flow
// straight into a task's environment as pipeline variables - the highest-
// priority, always-included tier of config.Environment.TaskEnv - which
// bypassed the task-execution-isolation allowlist entirely: every task in
// every project could always read KESTRIX_MASTER_KEY_PATH,
// KESTRIX_JWT_SIGNING_KEY_PATH, KESTRIX_WEBHOOK_SECRET,
// KESTRIX_GITLAB_TOKEN, and the Docker registry password as plain pipeline
// variables, regardless of KESTRIX_RUNNER_ENV_PASSTHROUGH. An operator who
// needs a task to see a Docker credential or another host variable now
// lists it explicitly in KESTRIX_RUNNER_ENV_PASSTHROUGH instead.
func (s *Server) buildVarStore(proj *model.Project) *variables.Store {
	vs := variables.New()
	if proj != nil {
		for _, v := range proj.Variables {
			val := v.Value
			if val == "" {
				val = v.Default
			}
			opts := []variables.Option{}
			if v.Protected {
				opts = append(opts, variables.WithProtected(true))
			}
			if v.Masked {
				opts = append(opts, variables.WithMasked(true))
			}
			switch v.Scope {
			case "repo":
				vs.SetRepo(v.Repo, v.Key, val, opts...)
			case "job":
				vs.SetJob(v.Repo, v.Job, v.Key, val, opts...)
			default:
				vs.SetProject(v.Key, val, opts...)
			}
		}
	}
	return vs
}
