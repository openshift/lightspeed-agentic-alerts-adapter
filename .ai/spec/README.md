# Alerts Adapter - Specifications

A Go component with no persistent storage that polls OpenShift AlertManager and accepts authenticated HTTPS requests for local on-demand investigations. Both paths create `AgenticRun` CRs (`agentic.openshift.io/v1alpha1`) to trigger automated remediation via the Lightspeed Agentic operator. Single-replica, create-only design with independent polling and investigation flows.

## Structure

| Layer | Path | Purpose |
|---|---|---|
| **what/** | `.ai/spec/what/` | System contracts: polling, investigation API, configuration, deduplication, and multicluster support. |
| **how/** | `.ai/spec/how/` | Codebase map, call flow, key abstractions, and implementation limits. |
| **decisions/** | `.ai/spec/decisions/` | Architectural decisions and links to shared decisions. |

## Scope

Covers the alerts adapter's internal behavior. Cross-repo integration contracts (multicluster credential flow, hub-spoke interaction model) live in the parent spec at `ols/.ai/spec/what/alerts-adapter-multicluster.md`.

## Audience

AI agents. Content is optimized for precision and machine consumption.

## Quick Start

| Task | Start here |
|---|---|
| Understand the system | `what/system-overview.md` |
| Understand the reconcile loop | `what/poll-loop.md` |
| Understand the investigation API | [what/investigation-api.md](what/investigation-api.md) |
| Understand alert-to-CR translation | `what/agenticrun-building.md` |
| Understand AlertManager interaction | `what/alert-retrieval.md` |
| Understand runtime configuration | `what/configuration.md` |
| Understand multicluster support | `what/multicluster.md` |
| Find code locations | `how/project-structure.md` |
| Follow startup and reconciliation | `how/reconcile-flow.md` |

## Cross-Reference

| what/ | how/ |
|---|---|
| `what/system-overview.md` | `how/project-structure.md` |
| `what/poll-loop.md` | `how/reconcile-flow.md`, `how/project-structure.md` |
| `what/investigation-api.md` | `how/reconcile-flow.md`, `how/project-structure.md` |
| `what/agenticrun-building.md` | `how/reconcile-flow.md`, `how/project-structure.md` |
| `what/alert-retrieval.md` | `how/project-structure.md` (alertmanager package) |
| `what/configuration.md` | `how/reconcile-flow.md`, `how/project-structure.md` |
| `what/multicluster.md` | `how/reconcile-flow.md`, `how/project-structure.md` |

## Conventions

- **Rule numbering:** behavioral rules are numbered sequentially within each what/ file. Numbers are stable identifiers; do not renumber when a rule is removed (leave a gap) or inserted (use sub-numbers like 16a, 16b). This keeps external references (Jira comments, PR descriptions) valid.
- **Planned changes lifecycle:** unimplemented behavior is marked `[PLANNED]` or `[PLANNED: TICKET-XXXX]` inline next to the rule it affects. When implemented: update the rule text and change the marker to `[DONE: TICKET-XXXX]`. `[DONE]` markers are cleanup candidates.
- **Constraints:** component-specific constraints go in the relevant what/ file's Constraints section. Development conventions go in AGENTS.md.
- **Authority:** what/ specs define behavior and how/ describes the code. Rules marked `[PLANNED]` are requirements not yet implemented; nearby text explains current limits. Parent specs own cross-repo contracts; these child specs own internal behavior.
- **When to create a new file vs. extend an existing one:** if the concern has its own lifecycle, configuration surface, and can be understood independently, it gets its own file. If it's a capability added to an existing component, it goes in that component's file.

## Project Context

This repo is part of the OpenShift Lightspeed family. See `ols/.ai/spec/README.md` for the product-level spec index and `ols/.ai/spec/how/repo-map.md` for the cross-repo concern lookup table.

## Open Work

Known implementation gaps are recorded next to the affected rules: [cooldown after analysis-only completion](what/poll-loop.md#post-run-delay-cooldown), [name length](what/agenticrun-building.md#metadata-sanitization), [local history across mode changes](what/agenticrun-building.md#agenticrun-crud), [credential refresh](what/multicluster.md#credential-secrets), and [remote error redaction](what/alert-retrieval.md#remote-alertmanager-spoke-clusters).

The hub CA requirement and the shared spoke-identity contract remain open cross-repo issues. Their existing rules are retained pending a separate decision.

The local investigation API is implemented. Cluster-level verification of certificate trust, caller authorization/admission, auditing, concurrent credential isolation, and API/poller creation races remains pending; see [API constraints and verification](what/investigation-api.md#constraints-and-verification). Focused local tests do not establish those integration guarantees.

There is no separate severity filter in the current loop; current selection uses receiver routing. E2E tests exist in `test/e2e/`; check `openshift/release` for the current CI job and step-registry definitions. Future design options remain in [ARCHITECTURE.md](../../ARCHITECTURE.md#future-options).
