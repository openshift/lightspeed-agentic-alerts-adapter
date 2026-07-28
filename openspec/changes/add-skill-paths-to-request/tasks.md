## 1. Template and request builder

- [ ] 1.1 Add `SkillPaths []string` field to `requestData` struct in `build.go`
- [ ] 1.2 Update `buildRequest` to accept shared skills, collect all paths with `/app` prefix, and populate `SkillPaths`
- [ ] 1.3 Update `request.tmpl` to conditionally render skill paths on a single space-separated line
- [ ] 1.4 Remove the Labels block from `request.tmpl`

## 2. Tests

- [ ] 2.1 Add `TestBuildRequestWithSkillPaths` covering: no shared skills, single source with multiple paths, multiple sources
- [ ] 2.2 Update `TestBuildRequest` to remove the labels assertion
