# Design

## Context

Docs-only change: two new root Markdown files (`SECURITY.md`, `CONTRIBUTING.md`)
and a README link. See proposal.md - Why. No code, no service, no data model,
no dependency is touched, so there is no architecture to diagram and no
migration to plan.

## Goals / Non-Goals

**Goals:**
- State where and how a vulnerability gets reported, and what a contributor is
  expected to do before opening an MR.

**Non-Goals:**
- No design-level decisions beyond content and placement of the two files —
  see proposal.md's own Non-goals for the product-level exclusions
  (CODE_OF_CONDUCT.md, CLA tooling, CVE process, etc.).

## Decisions

- **Two separate files, not one combined `CONTRIBUTING.md`.** Security
  reporting and contribution workflow are read by different audiences at
  different moments (a finder reporting a bug vs. a contributor opening an
  MR) and GitHub/GitLab both special-case `SECURITY.md` — bots and the UI
  surface it from a fixed filename, so folding it into `CONTRIBUTING.md`
  would lose that surfacing.
- **No architecture diagram.** There is no runtime component, data flow, or
  interface being introduced; the artifact is prose describing a process.
  A diagram would have nothing to depict.
- **Security considerations kept in `SECURITY.md` itself, not duplicated
  here.** The design has no security surface of its own (no code path), so
  the content — private reporting channel, response time, the finding
  classes that matter for a CI control plane storing credentials — lives in
  the shipped file rather than being restated in this design doc.

## Risks / Trade-offs

- [Two files instead of one] → Mitigation: `CONTRIBUTING.md` links to
  `SECURITY.md` for the reporting path, so a contributor starting from either
  file finds the other.
- [Reporting address/response-time commitment in `SECURITY.md` becomes stale]
  → Mitigation: none automated; it is prose, reviewed like any other doc
  change when it needs updating.
