## 1. AlertManager client: switch to groups endpoint

- [ ] 1.1 Change `GetAlerts` to `GetAlertGroups` in `internal/alertmanager/client.go`, calling `c.api.Alertgroup.GetAlertGroups()` with the same filter params (active=true, silenced=false, inhibited=false); return type changes to `models.AlertGroups`
- [ ] 1.2 Update `internal/alertmanager/client_test.go` for the new return type

## 2. Proposal building: group support

- [ ] 2.1 Add `GroupHash` function to `internal/proposal/build.go` that computes `sha256(sorted "key=value" pairs)[:8]` from a `models.LabelSet`
- [ ] 2.2 Add `BuildFromGroup` function that takes `*models.AlertGroup` and builds a Proposal with: group-hash-based naming, `alert-group-hash` label, `alert-severity` label (highest), `alert-names` annotation (JSON array), `alert-starts-at` annotation (earliest), `alert-summary` annotation, and `spec.targetNamespaces` collected from all alerts' `namespace` labels
- [ ] 2.3 Update `Build` (single alert) to use the new `alert-group-hash` label (using `fingerprint[:8]` as hash) instead of `alert-fingerprint`, for the fallback path
- [ ] 2.4 Update `request.tmpl` to a multi-alert template that lists group labels and all alerts with their individual details
- [ ] 2.5 Add tests for `GroupHash`, `BuildFromGroup` (multi-alert group, single-alert group, mixed namespaces, no namespace, severity ranking, alert-names annotation format)
- [ ] 2.6 Update existing `Build` tests for the new label scheme

## 3. Adapter: group-based reconcile loop

- [ ] 3.1 Change `AlertSource` interface from `GetAlerts(ctx) (models.GettableAlerts, error)` to `GetAlertGroups(ctx) (models.AlertGroups, error)`
- [ ] 3.2 Rewrite `reconcile` to iterate over groups: for each group, check if group labels are empty (fallback to per-alert) or non-empty (grouped path)
- [ ] 3.3 Implement group-level severity filtering: filter out `none`/`info` alerts from the group, skip group if empty after filtering
- [ ] 3.4 Implement group-level initial delay: pass if any alert in the group exceeds the threshold
- [ ] 3.5 Update `hasActiveProposal` and `inCooldown` to match by `alert-group-hash` label instead of `alert-fingerprint`
- [ ] 3.6 Update `internal/adapter/adapter_test.go` with group-based test cases: grouped creation, empty-label fallback, severity filtering within group, initial delay with mixed ages, dedup by group hash, cooldown by group hash

## 4. Cleanup

- [ ] 4.1 Remove `FingerprintLen` constant and `fingerprintPrefix` helper from `internal/proposal/build.go` (replaced by `GroupHash`; the adapter no longer references fingerprint length directly)
- [ ] 4.2 Remove old label constants (`labelFingerprint`, `labelAlertName`, `labelSeverity`) that are replaced by the new scheme
- [ ] 4.3 Run `make lint` and `make test` to verify all changes
