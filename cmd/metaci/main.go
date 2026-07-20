// Package main is the metaci CLI entry point.
//
// MetaCI — Repo-agnostic CI/CD Control Plane
//
// @title           MetaCI API
// @version         0.2.0
// @description     Repo-agnostic CI/CD orchestrator with HCL-based DAG pipeline definitions.
// @description     Supports OR/AND/NOT trigger logic for cross-repo conditional builds.
//
// @contact.name   MetaCI Team
// @contact.email  damian.giebas@gmail.com
//
// @host           localhost:8080
// @BasePath       /
//
// @securityDefinitions.apikey  WebhookToken
// @in                          header
// @name                        X-Gitlab-Token
// @description                 GitLab webhook secret token for signature verification
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
	"gitlab.com/lunaris-bot/metaci/internal/variables"
	"gitlab.com/lunaris-bot/metaci/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "resolve":
		cmdResolve(os.Args[2:])
	case "validate":
		cmdValidate(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "variables":
		cmdVariables(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `metaci — repo-agnostic CI/CD orchestrator

Usage:
  metaci resolve    [--changed repo1,repo2] <file.tf>
  metaci validate   <file.tf>
  metaci serve      [--addr :8080] [--dir ./examples] [--secret TOKEN] [--dispatch PROJECT_ID]
  metaci variables  <file.tf> [--repo auth_service] [--job deploy] [--env staging]`)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address (e.g. :8080, 0.0.0.0:9000)")
	dir := fs.String("dir", "./examples", "pipeline directory containing .tf files")
	secret := fs.String("secret", "", "webhook verification secret (env: METACI_WEBHOOK_SECRET)")
	method := fs.String("method", "gitlab", "verification method: gitlab or hmac")
	dispatchFlag := fs.String("dispatch", "", "GitLab project ID to dispatch pipelines to (env: METACI_DISPATCH_PROJECT)")
	gitlabURL := fs.String("gitlab-url", "https://gitlab.com", "GitLab instance URL (env: METACI_GITLAB_URL)")
	gitlabToken := fs.String("gitlab-token", "", "GitLab API token (env: METACI_GITLAB_TOKEN)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	// Resolve secrets from env if not set via flags.
	if *secret == "" {
		*secret = os.Getenv("METACI_WEBHOOK_SECRET")
	}
	if *dispatchFlag == "" {
		*dispatchFlag = os.Getenv("METACI_DISPATCH_PROJECT")
	}
	if *gitlabToken == "" {
		*gitlabToken = os.Getenv("METACI_GITLAB_TOKEN")
	}

	cfg := web.Config{
		ListenAddr:  *addr,
		PipelineDir: *dir,
		Signature: web.SignatureConfig{
			Enabled: *secret != "",
			Secret:  *secret,
			Method:  *method,
		},
	}

	// Setup dispatch if configured.
	if *dispatchFlag != "" && *gitlabToken != "" {
		cfg.Dispatch = web.DispatchConfig{
			Enabled:     true,
			ProjectID:   *dispatchFlag,
			GitLabURL:   *gitlabURL,
			GitLabToken: *gitlabToken,
		}
	}

	server := web.NewServer(cfg)
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func cmdResolve(args []string) {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	changedFlag := fs.String("changed", "", "comma-separated list of changed repo names")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: pipeline file required")
		os.Exit(2)
	}
	path := fs.Arg(0)

	proj, err := parser.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	changed := make(map[string]bool)
	for _, r := range proj.Repos {
		changed[r.Name] = false
	}
	if *changedFlag != "" {
		for _, name := range strings.Split(*changedFlag, ",") {
			name = strings.TrimSpace(name)
			if _, ok := changed[name]; !ok {
				fmt.Fprintf(os.Stderr, "warning: changed repo %q is not declared in pipeline\n", name)
			}
			changed[name] = true
		}
	}

	engine := dag.New(proj)
	nodes, err := engine.Resolve(changed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Project: %s\n", proj.Name)
	fmt.Printf("Changed repos: %s\n", changedSummary(changed))
	fmt.Printf("Tasks triggered (%d):\n", len(nodes))
	for _, n := range nodes {
		tag := "AUTO"
		if n.Task.Manual {
			tag = "MANUAL"
		}
		fmt.Printf("  [%s] %s\n", tag, n.Task.Name)
		fmt.Printf("    trigger: %s\n", n.Task.Trigger)
		fmt.Printf("    repo: %s | script: %s\n", n.Task.Execute.Repo, n.Task.Execute.Script)
		fmt.Printf("    reason: %s\n", n.TriggeredReason)
		if len(n.Task.DependsOn) > 0 {
			fmt.Printf("    depends_on: %s\n", strings.Join(n.Task.DependsOn, ", "))
		}
	}

	if len(proj.Variables) > 0 {
		fmt.Printf("\nVariables (%d):\n", len(proj.Variables))
		for _, v := range proj.Variables {
			scope := "project"
			if v.Scope != "" {
				scope = v.Scope
			}
			extra := ""
			if v.Repo != "" {
				extra += " repo=" + v.Repo
			}
			if v.Job != "" {
				extra += " job=" + v.Job
			}
			if v.Masked {
				extra += " [masked]"
			}
			if v.Protected {
				extra += " [protected]"
			}
			fmt.Printf("  [%s] %s = %s%s\n", scope, v.Key, v.Value, extra)
		}
	}
}

func cmdValidate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: pipeline file required")
		os.Exit(2)
	}
	path := fs.Arg(0)

	proj, err := parser.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	engine := dag.New(proj)
	if err := engine.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("OK — project %q validated\n", proj.Name)
	fmt.Printf("  repos: %d\n", len(proj.Repos))
	fmt.Printf("  tasks: %d\n", len(proj.Tasks))
	if len(proj.Variables) > 0 {
		fmt.Printf("  variables: %d\n", len(proj.Variables))
	}
}

func cmdVariables(args []string) {
	fs := flag.NewFlagSet("variables", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "show variables for this repo scope")
	jobFlag := fs.String("job", "", "show variables for this job scope")
	envFlag := fs.String("env", "", "filter by environment")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: pipeline file required")
		os.Exit(2)
	}
	path := fs.Arg(0)

	proj, err := parser.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	store := variables.New()
	for _, v := range proj.Variables {
		opts := []variables.Option{}
		if v.Protected {
			opts = append(opts, variables.WithProtected(true))
		}
		if v.Masked {
			opts = append(opts, variables.WithMasked(true))
		}
		val := v.Value
		if val == "" {
			val = v.Default
		}
		switch v.Scope {
		case "repo":
			store.SetRepo(v.Repo, v.Key, val, opts...)
		case "job":
			store.SetJob(v.Repo, v.Job, v.Key, val, opts...)
		default:
			store.SetProject(v.Key, val, opts...)
		}
		if v.Env != "" {
			store.SetEnvFilter(v.Key, v.Env)
		}
	}

	resolved := store.Get(*repoFlag, *jobFlag, *envFlag)

	fmt.Printf("Project: %s\n", proj.Name)
	context := "project-wide"
	if *repoFlag != "" {
		context = "repo=" + *repoFlag
	}
	if *jobFlag != "" {
		context += " job=" + *jobFlag
	}
	if *envFlag != "" {
		context += " env=" + *envFlag
	}
	fmt.Printf("Context: %s\n", context)
	fmt.Printf("Resolved variables (%d):\n", len(resolved))
	for k, v := range resolved {
		fmt.Printf("  %s = %s\n", k, v)
	}
}

func changedSummary(changed map[string]bool) string {
	var parts []string
	for name, v := range changed {
		if v {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}
