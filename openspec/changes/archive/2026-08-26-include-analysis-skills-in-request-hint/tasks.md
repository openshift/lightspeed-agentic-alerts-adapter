## 1. Tests

- [x] 1.1 Add test: skill hint includes analysis-level skill paths when no shared skills are configured
- [x] 1.2 Add test: skill hint includes both shared and analysis skill paths when both are configured
- [x] 1.3 Verify existing test: no skill hint when neither shared nor analysis skills are configured

## 2. Implementation

- [x] 2.1 Change `buildRequest` to accept analysis skills in addition to shared skills and merge paths from both into `SkillPaths`
- [x] 2.2 Update `Build` to pass `tools.Analysis` to `buildRequest`

## 3. Verification

- [x] 3.1 Run full test suite and confirm all tests pass
- [x] 3.2 Run linter
