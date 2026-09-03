---
name: "OPSX: Status"
description: "Repository-wide OpenSpec implementation progress audit"
allowed-tools: Bash(openspec:*), Bash(python3:*), Bash(rg:*), Bash(make:*), Bash(go:*), Read, Grep, Glob
category: "Workflow"
tags: ["workflow", "specs", "audit"]
---

Audit implementation progress across every OpenSpec spec in this repository.

Use the `openspec-implementation-status` skill and follow it exactly. If `$ARGUMENTS` names specific specs, scope the audit to those; otherwise inventory all specs first and state clearly which ones were fully audited versus only inventoried.

This is a read-only audit: report evidence, do not modify code, specs, or task files.
