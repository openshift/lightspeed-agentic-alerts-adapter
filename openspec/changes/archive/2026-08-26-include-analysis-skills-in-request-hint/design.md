## Context

`buildRequest` receives only `sharedSkills []agenticv1alpha1.SkillsSource` and extracts paths from it to populate `SkillPaths` in the template data. The template renders a skill hint only when `SkillPaths` is non-empty. Skills configured at the analysis step level are mounted on the CR but invisible to the analysis agent's prompt.

## Goals / Non-Goals

**Goals:**
- The request prompt includes skill paths from both shared and analysis-level skills, so the analysis agent knows about all skills it can use.

**Non-Goals:**
- Changing the template syntax or structure beyond the existing conditional.
- Including execution or verification skills in the request (those steps have their own agents and are not prompted by the request field).

## Decisions

**Pass analysis skills into `buildRequest` alongside shared skills.**

`buildRequest` currently takes `sharedSkills []agenticv1alpha1.SkillsSource`. Change it to also accept `analysisSkills []agenticv1alpha1.SkillsSource` and merge the paths from both into `SkillPaths`.

Alternative considered: merging skills upstream in `Build` and passing a single combined slice. Rejected because `Build` still needs them separate for populating `spec.tools` (shared) vs `spec.analysis.tools` (per-step) on the CR. Merging only for the request is the caller's concern, and `buildRequest` is the right place.

## Risks / Trade-offs

- Duplicate paths: if the same skill path appears in both shared and analysis config, the hint will list it twice. This is harmless (the agent sees the same path mentioned twice) and not worth the complexity of deduplication.
