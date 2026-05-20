## Summary

- What changed:
- Why:

## Policy Change Type

- [ ] Widening
- [ ] Narrowing
- [ ] Refactor only

## Cluster-Local Use Case

- Affected component or object kind:
- Why this path must stay cluster-local:
- Why this should not become shared package/BOM baseline:

## Validation

- Commands run:
  - ``
- Expected positive case:
- Expected negative case:
- Rendered provenance checked:
  - [ ] `localPatchPolicySource`
  - [ ] `localPatchPolicyScope`
  - [ ] `localPatchPolicyName`
  - [ ] `localPatchPolicyPath`
  - [ ] `localPatchPolicyDigest`

## Risks / Notes

- Drift-classification impact:
- Existing `local-repo/patches/**` compatibility:
- Operator-facing `sync commit` / `promoteToLocalPatch` impact:

## Related

- Issue / design / discussion:

---

## LocalPatchPolicy Review Checklist

- [ ] Scope still fits `spec.scope: clusterLocal`
- [ ] Supported source model remains explicit (`localRepo` or `builtInDefault`)
- [ ] One concrete cluster-local use case is described
- [ ] The change does not open forbidden areas such as container `image`,
      selectors, `status`, or server-managed metadata
- [ ] Existing `local-repo/patches/**` compatibility was checked
- [ ] Drift-classification impact is described when relevant
      (`policyEligible`, `promoteToLocalPatch`, `sync commit`)
- [ ] Rendered policy provenance was checked
- [ ] At least one expected positive validation case is included
- [ ] At least one expected negative validation case is included
