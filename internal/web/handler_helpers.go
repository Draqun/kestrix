package web

import "net/http"

// actorFrom returns the authenticated account that made the request, as
// "user:<name>", or "" if the request carries no verified claims. It never
// reads the request body, a header, or a query parameter for the actor -
// only claims RequireAuth placed on the context - see run-attribution D5.
func actorFrom(r *http.Request) string {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		return ""
	}
	return "user:" + claims.UserName
}

// buildVarStore used to construct a variables.Store from a project's own
// declared variables, duplicating service.BuildVarStore. It never resolved a
// secret_ref, so it is gone; every caller now calls service.BuildVarStore
// with this server's credential resolver - see runtime-secrets design.md D3.
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
