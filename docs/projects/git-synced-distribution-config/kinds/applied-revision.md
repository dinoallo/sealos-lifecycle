# Kind: AppliedRevision

## Status

Implemented runtime state file schema.

## Class

Runtime state document.

## Owner

The apply or reconciliation workflow writes this document.

## Normal Locations

- `state/<cluster>/applied-revision.yaml`
- `out/state/<cluster>/applied-revision.yaml`

## Purpose

`AppliedRevision` records the distribution revision currently observed or last
successfully applied to a cluster. It gives drift and audit workflows a compact
runtime state document without treating source documents as applied state.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedRevision
metadata:
  name: prod-01
spec: {}
status: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `clusterName` | Yes | Cluster name. |
| `bom.name` | Yes | Applied BOM name. |
| `bom.revision` | Yes | Applied BOM revision. |
| `bom.channel` | No | Channel that resolved the BOM. |
| `bom.digest` | No | Digest of the applied BOM. |
| `localRepoRevision` | No | Local repo revision used by source-first local mode. |
| `localPatchRevision` | No | Local patch revision used by the apply. |
| `desiredStateDigest` | Yes | Digest of the desired state that was applied. |

## Status Contract

| Field | Description |
| --- | --- |
| `state` | One of `Clean`, `Dirty`, `Orphan`, or `Degraded`. |
| `lastAppliedTime` | Last apply attempt time. |
| `lastSuccessfulRevision` | Last known successful revision snapshot, including BOM identity, target metadata, local revisions, and desired-state digest. |
| `successfulRevisions` | Bounded newest-first history of successful revision snapshots, used for cross-BOM audit and rollback context. |
| `observedSummary` | Counts or summary of observed resources. |
| `conditions` | Structured conditions for apply and drift state. |

## Validation Rules

- `clusterName`, BOM identity, and `desiredStateDigest` must be set.
- `state` must be one of the supported states.
- Digests, when present, must use supported digest formats.
- Runtime fields must be written by reconciliation, not by source authors.

## Lifecycle

1. Apply consumes a `HydratedBundle`.
2. Reconciliation writes the applied revision and desired state digest.
3. Drift detection updates status conditions.
4. Future applies update the same cluster state with a new revision and prepend
   a successful revision history entry.
5. Rollback uses the last successful revision snapshot, including its target
   metadata, even when the failed upgrade selected a different BOM line.

## Boundaries

- `AppliedRevision` is runtime state, not source intent.
- `AppliedRevision` does not contain the full object inventory.
- `AppliedRevision` does not replace health or acceptance evidence.
- `AppliedRevision` should not be used to select future releases.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedRevision
metadata:
  name: prod-01
spec:
  clusterName: prod-01
  bom:
    name: sealos-v5.0.0
    revision: v5.0.0
    channel: stable
    digest: sha256:...
  localRepoRevision: 2026-06-01T00-00-00Z
  localPatchRevision: prod-01-2026-06-01
  desiredStateDigest: sha256:...
status:
  state: Clean
  lastAppliedTime: "2026-06-01T00:00:00Z"
  lastSuccessfulRevision:
    bom:
      name: sealos-v5.0.0
      revision: v5.0.0
      channel: stable
      digest: sha256:...
    desiredStateDigest: sha256:...
  successfulRevisions:
    - bom:
        name: sealos-v5.0.0
        revision: v5.0.0
        channel: stable
        digest: sha256:...
      desiredStateDigest: sha256:...
```

## Related Kinds

- `HydratedBundle` provides the desired state being applied.
- `AppliedInventory` may provide detailed managed inventory.
- `DistributionTarget` status may reference applied revision paths.
- `PackageAcceptanceReport` records pre-release acceptance evidence.
