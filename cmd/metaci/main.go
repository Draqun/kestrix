// Package main is the metaci CLI entry point.
//
// Usage:
//
//	metaci resolve <path-to-pipeline.tf> --changed auth_service,docs
//	metaci validate <path-to-pipeline.tf>
//
// "resolve" reads the HCL pipeline, evaluates trigger expressions against the
// given changed-repo set, and prints which tasks should run.
//
// "validate" parses the HCL and checks the DAG for cycles but does not require
// any changed-set input.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/parser"
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
  metaci resolve  [--changed repo1,repo2] <file.tf>
  metaci validate  <file.tf>
  metaci serve     [--addr :8080] [--dir ./examples]`)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address (e.g. :8080, 0.0.0.0:9000)")
	dir := fs.String("dir", "./examples", "pipeline directory containing .tf files")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	cfg := web.Config{
		ListenAddr:  *addr,
		PipelineDir: *dir,
	}
	server := web.NewServer(cfg)
	if err := server.Run(); err != nil {
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

	// Build the changed-repos map: every declared repo is present; changed ones set to true.
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
		if len(n.Task.
			DependsOn) > 0 {
			fmt.Printf("    depends_on: %s\n", strings.Join(n.Task.DependsOn, ", "))
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
