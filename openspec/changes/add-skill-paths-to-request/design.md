## Context

The alerts adapter builds an AgenticRun CR for each firing alert. The `spec.request` field is a natural-language prompt rendered from `request.tmpl` that tells the analysis agent what to investigate. Skills (OCI images with tooling) are already mounted into the agent container via `spec.tools.skills`, but the agent has no way to know the filesystem paths where those skills are available.

Skills are mounted under `/app` in the agent container, so a skill with path `/skills/prometheus` in the OCI image is available at `/app/skills/prometheus` at runtime.

This approach is a workaround: the host (Agentic Lightspeed) does not currently load skills automatically from mounted volumes. Until the operator supports automatic skill discovery, the adapter must embed skill paths in the request text so the agent knows where to look.

## Goals / Non-Goals

**Goals:**

- Include shared skill paths in the request text so the agent can discover and use them.
- Prepend `/app` to each skill path to reflect the actual mount location in the agent container.
- Remove the raw Labels block from the template (alert identity is already conveyed by the structured fields).

**Non-Goals:**

- Per-step skill paths in the request. The request is consumed by the analysis agent; per-step skills are handled by the operator.
- Validation of skill paths beyond what `config.LoadFromFile` already does.

## Decisions

**Skill paths are collected from all shared skill sources and flattened into a single list.**
Each `SkillsSource` can have multiple paths and there can be multiple sources. All paths are collected, each prefixed with `/app`, and passed to the template as a flat `[]string`. This keeps the template simple.

**Paths are rendered on a single line, space-separated.**
A single "Investigate using the skill at" line followed by all paths. This is concise and sufficient for the agent to parse.

**The `/app` prefix is applied at render time, not stored in config.**
The config stores OCI-image-relative paths (e.g., `/skills/prometheus`). The `/app` prefix is an agent container concern and is applied when building the request.

## Risks / Trade-offs

**Mount prefix is hardcoded.** If the operator changes the mount point from `/app`, the request will contain wrong paths. This is acceptable because the mount point is an operator convention unlikely to change, and the request is advisory text for the agent, not a hard contract.

**This is a workaround.** Once Agentic Lightspeed supports automatic skill loading, this hint in the request text should be removed and the skill paths section of the template deleted.
