package web

import (
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/variables"
)

// buildVarStore constructs a variables.Store from a project's variables,
// METACI_* environment variables, and Docker config.
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
	for _, v := range variables.FromEnv("METACI_").List() {
		vs.SetProject(v.Key, v.Value)
	}
	if s.cfg.Docker.Registry != "" {
		vs.SetProject("DOCKER_REGISTRY", s.cfg.Docker.Registry)
	}
	if s.cfg.Docker.Image != "" {
		vs.SetProject("DOCKER_IMAGE", s.cfg.Docker.Image)
	}
	if s.cfg.Docker.Username != "" {
		vs.SetProject("DOCKER_USERNAME", s.cfg.Docker.Username)
	}
	if s.cfg.Docker.Password != "" {
		vs.SetProject("DOCKER_PASSWORD", s.cfg.Docker.Password)
	}
	return vs
}
