# Sync Operations Runbook

## Status

Current operations contract

## Purpose

This runbook turns `sealos sync diff`, `sealos sync status`, and
`operatorAction` fields into the normal operations entrypoint. Use it when a
cluster has drift, a controller target reports `Degraded`, or an alert needs a
stable summary, ticket shape, and first remediation command.

## Triage Loop

1. Capture the compact state:

   ```bash
   sealos sync status \
     --cluster <cluster> \
     --bundle-dir <bundle> \
     --kubeconfig <kubeconfig> \
     --host-root /
   ```

2. Copy `headline` into the alert title, ticket subject, or dashboard row.
3. Copy `summary`, `operatorActionSummary`, `localPatchPolicy`, and
   `topologyStatus` into the ticket body.
4. Open `sync diff` for the raw mismatch paths:

   ```bash
   sealos sync diff \
     --cluster <cluster> \
     --bundle-dir <bundle> \
     --kubeconfig <kubeconfig> \
     --host-root /
   ```

5. Follow the first matching `operatorAction` path below.
6. Re-run `sync status` after any `commit`, `revert`, `render`, or `apply`.

## Alert Headline

Use the top-level `headline` field as the alert title. It is designed to be
short, stable, and count-based:

```text
state=Orphan; dirtyObjects=0; orphanObjects=2; dirtyHostPaths=8; orphanHostPaths=48; directCommitEligible=8; directRevertEligible=57; bundleMatchRequired=57
```

If the alerting system needs a deterministic severity, derive it from the
structured fields instead of parsing free-form remediation text:

| Condition | Suggested Severity | Reason |
| --- | --- | --- |
| `currentState=Clean` | clear | Desired and live state match. |
| `currentState=Dirty` with `directCommitEligible>0` or `directRevertEligible>0` | warning | Operator action is available, but live state differs from recorded desired state. |
| `currentState=Orphan` with `orphanObjects>0` or `orphanHostPaths>0` | warning | Global baseline or unrecorded live state needs review. |
| `operatorAction=manualReview` | critical | The CLI cannot safely classify or repair the drift. |
| `topologyStatus.state!=matched` | critical | The bundle was rendered for a different cluster topology. |
| `renderInputStatus.state!=matched` | critical | The inspected bundle no longer matches recorded render inputs. |

## Dashboard Summary

Use one row per cluster and keep these columns stable:

| Column | Source Field |
| --- | --- |
| Cluster | `clusterName` |
| State | `currentState` |
| Headline | `headline` |
| Revision | `bomName`, `revision`, `channel` |
| Desired digest | `desiredStateDigest` |
| Local repo revision | `localRepoRevision` |
| Drift counts | `summary.clean`, `summary.dirty`, `summary.orphan`, `summary.total` |
| Direct actions | `operatorActionSummary.directCommitEligible`, `directRevertEligible`, `bundleMatchRequired` |
| Local patch policy | `localPatchPolicy.source`, `scope`, `name`, `digest` |
| Topology | `topologyStatus.state` |
| Render inputs | `renderInputStatus.state` |
| Last observation | `recordedObservedSummary.lastObservedTime` |

Do not put Secret object data, kubeconfig contents, or local repo resource bytes
into dashboards. Object names, kinds, namespaces, paths, digests, and counts are
safe to display.

## Ticket Fields

Every ticket should carry enough structured context to reproduce the same
decision:

| Ticket Field | Required Value |
| --- | --- |
| `cluster` | `clusterName` |
| `state` | `currentState` |
| `headline` | top-level `headline` |
| `bom` | `bomName`, `revision`, `channel`, and `desiredStateDigest` |
| `localRepoRevision` | top-level `localRepoRevision` |
| `topologyStatus` | state plus mismatch message when not matched |
| `renderInputStatus` | state plus mismatch message when not matched |
| `operatorActionSummary` | direct commit, direct revert, and bundle-match counts |
| `primaryOperatorAction` | first issue's `operatorAction` by severity |
| `primaryRemediation` | first issue's `remediation.action` and `nextSteps[]` |
| `affectedObjects` | kind, namespace, name, and changed paths |
| `affectedHostPaths` | host, path, component, ownership, and projection class |
| `evidence` | command, timestamp, operator, and artifact paths for captured `sync status` and `sync diff` output |

Attach the raw `sync status -o yaml` and `sync diff -o yaml` outputs as
artifacts. Redact Secret values if an external system expands object payloads.
The current `sync` summaries should already avoid copying Secret bytes.

## Common Repair Paths

| `operatorAction` | First Response | Escalate When |
| --- | --- | --- |
| `commitOrReapplyLocalOverlay` | If the live local object change is intended, run `sync commit`; otherwise run `sync revert`. | The object contains data-plane state or Secret bytes that require owner approval. |
| `commitOrReapplyLocalInput` | If the live host file change is intended, run `sync commit`; otherwise run `sync revert`. On multi-node drift, pass `--host` when the same input differs by host. | Hosts disagree and there is no host-scoped input binding. |
| `promoteToLocalPatch` | Add or update a cluster-local patch, rerender, then apply. | The changed path is not covered by current `LocalPatchPolicy`. |
| `revertOrUpdateGlobalBaseline` | Run `sync revert` when the selected baseline is correct; otherwise update the package or BOM baseline and rerender. | The path is owned by a package owner outside the on-call team. |
| `updateLocalInputAndRerender` | Update the local bootstrap input, run `sync render`, then `sync apply`. | The generated projection is data-plane sensitive or has no owner-approved input model. |
| `rerenderOrUpdateGlobalBaseline` | Update the package or BOM baseline, run `sync render`, then `sync apply`. | The package revision must go through release/promotion approval. |
| `manualReview` | Stop automated repair and inspect the live projection, package source, and local repo owner. | Always escalate before mutation. |

## Command Guardrails

- Do not run `sync commit`, `sync revert`, or `sync apply` when
  `topologyStatus.state` is not `matched`.
- Do not run direct commands when the relevant `commandGuidance` entry is
  `blocked`.
- Treat `requiresBundleMatch=true` as a hard precondition: the inspected bundle
  must match `desiredStateDigest`.
- Use `--scope local` when you only intend to touch cluster-local overlays or
  inputs.
- Use `--kind`, `--namespace`, `--name`, `--host-path`, `--host`, and
  `--component` to narrow destructive actions.
- Never use `sync commit` for package-owned global baseline drift; update the
  package or BOM through the release path instead.

## Evidence To Keep

For every incident or maintenance ticket, keep:

- `sync status -o yaml`
- `sync diff -o yaml`
- the command that changed state, if any
- the post-change `sync status -o yaml`
- referenced local repo patch/input paths
- referenced package or BOM revision paths
- rollout or controller target events when a controller initiated the reconcile

This evidence is also the input for promotion gates and post-incident review:
it proves which revision was selected, what drift existed, which owner path was
used, and whether the cluster returned to the expected state.
