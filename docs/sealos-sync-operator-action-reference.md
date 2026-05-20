# Sealos Sync Operator Action Reference

## Status

Draft

## Summary

This guide is a compact operator-facing reference for the current
single-node `sync diff` and `sync status` summaries. It explains the meaning of
each `operatorAction`, whether the action allows direct `sync commit` or
`sync revert`, and when the current CLI still depends on
`bundleMatchesRecordedDesiredStateDigest`.

Use this file as a quick lookup table. For the deeper tracking and ownership
model behind these actions, read
[sealos-materialization-tracking-and-drift-detection-model.md](./sealos-materialization-tracking-and-drift-detection-model.md).

## Related Documents

- Tracking and drift model:
  [sealos-materialization-tracking-and-drift-detection-model.md](./sealos-materialization-tracking-and-drift-detection-model.md)
- Ownership and reconcile model:
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- Local repo layout and patch/input rules:
  [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md)
- Local patch policy authoring and review checklist:
  [sealos-local-patch-policy-authoring-and-review.md](./sealos-local-patch-policy-authoring-and-review.md)
- Current operator loop walkthrough:
  [sealos-sync-drift-walkthrough.md](./sealos-sync-drift-walkthrough.md)
- Example `sync diff` snapshot:
  [docs/examples/sync-drift-minimal/sync-diff.example.yaml](./examples/sync-drift-minimal/sync-diff.example.yaml)
- Example `sync status` snapshot:
  [docs/examples/sync-drift-minimal/sync-status.example.yaml](./examples/sync-drift-minimal/sync-status.example.yaml)

## When To Read `sync diff` vs `sync status`

- Use `sync diff` when you need the raw compare payload, including tracked
  objects, mismatches, and remediation details for the current rendered bundle.
- Use `sync status` when you want the summarized ownership view, mixed-ownership
  grouping, and the cluster's recorded observed state side by side.
- In both commands, `operatorAction` is the compact summary-level cue.
- `operatorActionMetadata` is the narrow capability view:
  - `allowsDirectCommit`
  - `allowsDirectRevert`
  - `requiresBundleMatch`
- `operatorActionSummary` is the top-level count view for the current drift set:
  - `directCommitEligible`
  - `directRevertEligible`
  - `bundleMatchRequired`
- `headline` is the shortest reusable operator summary. It is intended to be
  stable enough for alert titles, ticket subjects, or dashboard labels.
- `localPatchPolicy` is the top-level provenance block for the effective
  rendered local-patch policy artifact. When you are judging whether a drift
  can become `promoteToLocalPatch`, this tells you which policy source, scope,
  name, and digest the current bundle actually carried.

## Where To See The Fields In Practice

If you want concrete YAML rather than abstract field names, use these two
fixtures together:

- [docs/examples/sync-drift-minimal/sync-diff.example.yaml](./examples/sync-drift-minimal/sync-diff.example.yaml)
- [docs/examples/sync-drift-minimal/sync-status.example.yaml](./examples/sync-drift-minimal/sync-status.example.yaml)

They are shortened, schema-aligned snapshots that show:

- where `headline` sits
- where `operatorActionSummary` sits
- where `policyEligibleOrphanObjects` sits
- where `localPatchPolicy` sits
- how `operatorAction`, `operatorActionMetadata`, and remediation blocks line up
  under object and host-path issues

## Current `operatorAction` Matrix

| `operatorAction` | Typical Drift Surface | Meaning | Direct `sync commit` | Direct `sync revert` | Requires Bundle Match | Typical Next Command Path |
| --- | --- | --- | --- | --- | --- | --- |
| `commitOrReapplyLocalOverlay` | local-owned object drift | The live object differs from the local overlay already tracked by Sealos. | yes | yes | yes | `sync commit` or `sync revert` |
| `promoteToLocalPatch` | package-owned object drift whose changed paths already fit `LocalPatchPolicy` | The drift is still global-owned right now, but the supported long-term fix can be captured as a local repo patch. | no | no | no | author or adjust `local-repo/patches`, then `sync render` and `sync apply` |
| `revertOrUpdateGlobalBaseline` | package-owned object drift or direct global host-file drift | The drift belongs to the selected distribution global baseline. | no | yes | yes | `sync revert`, or change the package/BOM global baseline |
| `commitOrReapplyLocalInput` | local-owned direct host-file drift | The drift belongs to an already tracked local input-backed file. | yes | yes | yes | `sync commit` or `sync revert` |
| `updateLocalInputAndRerender` | generated drift driven by local bootstrap input | The live projection should be fixed by changing the local input and regenerating desired state. | no | no | no | update local input, then `sync render` and `sync apply` |
| `rerenderOrUpdateGlobalBaseline` | generated drift driven by package/BOM global baseline | The generated projection should be fixed by changing the selected global baseline and regenerating desired state. | no | no | no | update package/BOM global baseline, then `sync render` and `sync apply` |
| `manualReview` | semantic parse failure or unsupported generated drift | Sealos cannot safely route this through an automated ownership path yet. | no | no | no | inspect manually before changing desired or live state |

## Quick Interpretation Rules

### If `allowsDirectCommit: true`

- The current single-node MVP already supports a direct `sync commit` path for
  this drift class.
- Today this mainly means:
  - local object overlays
  - local input-backed direct host files

### If `allowsDirectRevert: true`

- The current single-node MVP already supports a direct `sync revert` path for
  this drift class.
- Today this typically covers:
  - supported local overlay drift
  - supported local input-backed host-file drift
  - direct global baseline drift that should be pulled back to desired state

### If `requiresBundleMatch: true`

- The direct command path is still guarded by
  `bundleMatchesRecordedDesiredStateDigest`.
- In practice, this means the rendered bundle you are inspecting must still
  match the cluster's recorded desired-state digest before Sealos will treat the
  direct command path as safe.

### If all three flags are `false`

- The current CLI does not support a direct `commit` or `revert` path for that
  action.
- The fix must go through changing desired state inputs first:
  - local patch authoring
  - local input updates
  - package/BOM global-baseline changes
  - or manual review

## How To Read `operatorActionSummary`

- `directCommitEligible` counts dirty/orphan issues whose current summarized
  action supports a direct `sync commit` path.
- `directRevertEligible` counts dirty/orphan issues whose current summarized
  action supports a direct `sync revert` path.
- `bundleMatchRequired` counts dirty/orphan issues whose direct path is still
  guarded by `bundleMatchesRecordedDesiredStateDigest`.

These counters are intentionally derived from the main summarized issue lists.
They do not double-count the narrower `policyEligibleOrphanObjects` subset.

The same counts are also reflected in the current `Observed` condition message,
which gives operators a compact sentence-level summary even before they inspect
the structured snapshot fields.

## Common Operator Patterns

### 1. Local overlay drift

- Look for:
  - `operatorAction: commitOrReapplyLocalOverlay`
- Typical path:
  - use `sync commit` if the current live change is intended and should be
    written back to local repo overlay state
  - use `sync revert` if the live change should be discarded

### 2. Policy-eligible orphan drift

- Look for:
  - `operatorAction: promoteToLocalPatch`
  - usually under `policyEligibleOrphanObjects`
- Typical path:
  - do not treat this as a direct `commit` or `revert` workflow
  - instead, author or refine a local repo patch, then rerender and reapply

### 3. Direct global baseline drift

- Look for:
  - `operatorAction: revertOrUpdateGlobalBaseline`
- Typical path:
  - use `sync revert` if the selected distribution global baseline is still
    correct
  - otherwise change the package or BOM global baseline and rerender desired
    state

### 4. Generated drift from local bootstrap input

- Look for:
  - `operatorAction: updateLocalInputAndRerender`
- Typical path:
  - update the local bootstrap input that feeds the rendered generator input
  - run `sync render`
  - run `sync apply`

### 5. Generated drift from package or BOM global baseline

- Look for:
  - `operatorAction: rerenderOrUpdateGlobalBaseline`
- Typical path:
  - change the selected global baseline
  - rerender desired state
  - reapply the bundle

### 6. Manual-review drift

- Look for:
  - `operatorAction: manualReview`
- Typical path:
  - inspect the generated projection or unsupported drift manually before
    changing desired or live state

## Current Scope Boundary

This reference describes the current single-node MVP only.

It does not imply:

- multi-node target resolution
- controller-driven continuous reconciliation
- direct generated-projection revert support
- a fully externalized ownership-policy object

Those remain future work.
