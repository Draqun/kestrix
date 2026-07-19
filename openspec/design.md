# Design: MetaCI Architecture

## Overview

MetaCI is a Go-based control plane that sits between git repositories and CI runners. It receives webhook events, evaluates trigger conditions against a pipeline definition, and dispatches execution to GitLab Runners.

## Architecture

```
┌──────────────┐     webhook     ┌────────────────┐     dispatch    ┌──────────────┐
│  Git Repo    │ ──────────────▶ │  MetaCI        │ ──────────────▶ │  GitLab      │
│  (push)      │                 │  Control Plane │                 │  Runner      │
└──────────────┘                 │                │                 └──────────────┘
                                 │  ┌──────────┐  │
                                 │  │ HCL      │  │
                                 │  │ Parser   │  │
                                 │  └──────────┘  │
                                 │  ┌──────────┐  │
                                 │  │ DAG      │  │
                                 │  │ Engine   │  │
                                 │  └──────────┘  │
                                 │  ┌──────────┐  │
                                 │  │ Webhook  │  │
                                 │  │ Receiver │  │
                                 │  └──────────┘  │
                                 │  ┌──────────┐  │
                                 │  │ CI       │  │
                                 │  │ Generator│  │
                                 │  └──────────┘  │
                                 └────────────────┘
```

## Components

### 1. Domain Model (`internal/model/`)

Core types: `Project`, `Repo`, `Task`, `ExecuteSpec`. These are pure data structures with no logic — they represent the parsed HCL configuration.

### 2. HCL Parser (`internal/parser/`)

Reads `.tf` files using `hashicorp/hcl/v2` with manual body decoding (no `gohcl` dependency). Produces `model.Project` values. Supports merging multiple files via `ParseDir`.

### 3. DAG Engine (`internal/dag/`)

Evaluates boolean trigger expressions using a hand-written recursive descent parser. Supports `&&`, `||`, `!`, parentheses, and `repo.<name>.changed` predicates. Also handles cycle detection and validation.

### 4. Webhook Receiver (`internal/web/`)

HTTP server built on Chi framework. Parses GitLab push payloads, classifies files (code vs docs), loads pipeline definitions, evaluates the DAG, and returns results including generated `gitlab-ci.yml`.

### 5. CI Generator (`internal/web/generator.go`)

Produces GitLab CI YAML from resolved DAG nodes. Maps dependencies to `needs`, derives stages, handles manual approval gates and per-job images.

## Data Flow

1. Developer pushes code to a repository
2. GitLab sends a webhook to MetaCI's `/webhook` endpoint
3. Webhook receiver parses the payload and extracts changed files
4. File classifier determines which repos have code changes
5. DAG engine evaluates trigger expressions against the changed repos
6. CI generator produces `gitlab-ci.yml` from the resolved task graph
7. Response is returned (future: dispatch to GitLab Runner API)

## Design Decisions

### HCL over YAML/JSON for pipeline definitions

HCL provides a familiar syntax for infrastructure engineers (same as Terraform), supports block-based structure natively, and has a well-maintained Go library. The trade-off is that HCL expressions in trigger strings need our own parser, but this gives us full control over the evaluation semantics.

### Manual expression parsing over gohcl

`gohcl` requires an `EvalContext` even for literal values, which adds complexity without benefit. Manual parsing via `hclsyntax.Body` attributes is simpler, more predictable, and has no hidden behavior.

### Chi over net/http

Chi provides middleware (logging, recovery, request ID), routing, and `Heartbeat` endpoint out of the box, with stdlib compatibility and no runtime dependencies.

### File classification with configurable patterns

Instead of hardcoding "docs don't trigger builds", we use a `FileClassifier` with configurable code patterns and exclude patterns. This allows different projects to have different definitions of what constitutes a "code change".

## Security Considerations

- Webhook payloads are untrusted — the parser rejects malformed JSON and non-push events
- Pipeline definitions are trusted — they are managed by the project owner
- No secrets are stored in the pipeline definition
- Future: JWT/auth tokens for webhook verification
