---
name: "OPSX: Audit"
description: "Evidence-based audit of one OpenSpec spec's implementation"
allowed-tools: Bash(openspec:*), Bash(python3:*), Bash(rg:*), Bash(make:*), Bash(go:*), Read, Grep, Glob
category: "Workflow"
tags: ["workflow", "specs", "audit"]
---

Audit the implementation of the OpenSpec spec named in `$ARGUMENTS`.

Use the `openspec-spec-audit` skill and follow it exactly. If `$ARGUMENTS` is empty, resolve the spec from conversation context, or run `openspec list --specs` and ask the user which spec to audit.

This is a read-only audit: report evidence, do not modify code, specs, or task files.
