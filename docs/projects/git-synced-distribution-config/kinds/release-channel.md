# Kind: ReleaseChannel

## Status

Implemented file schema. `ReleaseChannel` is the preferred name for new
documents. The loader still accepts the legacy `DistributionChannel` kind.

## Class

Release source document.

## Owner

The release owner or promotion owner maintains release channel documents.

## Normal Locations

- `channels/<distribution>/<channel>.yaml`
- `releases/<distribution>/channels/<channel>.yaml`

## Purpose

`ReleaseChannel` points a named channel, such as `stable`, `beta`, or `alpha`,
to the target distribution revision and BOM path. It is the promotion pointer
that downstream cluster selection follows.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: sealos-stable
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `distribution` | Yes | Distribution line, such as `sealos`. |
| `channel` | Yes | Channel name, such as `stable`, `beta`, or `alpha`. |
| `targetRevision` | Yes | Target BOM revision. |
| `bomPath` | Yes | Repository-relative path to the target BOM. |
| `promotionHistory` | No | Historical promotions into this channel. |

Legacy documents may use `line` instead of `distribution`. New documents should
use `distribution`.

## Promotion History

Each promotion history entry records:

- source revision
- target revision
- BOM path
- promotion reason
- approver
- approval time
- optional health proof path, digest, and summary

Promotion history is append-only evidence. The current channel state is always
the `targetRevision` and `bomPath` pair.

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- `spec.distribution` or legacy `spec.line` must be set.
- `spec.channel`, `spec.targetRevision`, and `spec.bomPath` must be set.
- `bomPath` must be repository-relative.
- Promotion evidence should reference a `DistributionHealthProof`.

## Lifecycle

1. A release candidate `BOM` is produced.
2. Package acceptance and health checks produce evidence.
3. The promotion owner updates `ReleaseChannel.targetRevision` and `bomPath`.
4. Cluster targets that follow the channel reconcile toward the new revision.

## Boundaries

- `ReleaseChannel` does not define package content.
- `ReleaseChannel` does not target specific clusters.
- `ReleaseChannel` does not bypass local patch gates.
- `ReleaseChannel` is not runtime state.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: sealos-stable
spec:
  distribution: sealos
  channel: stable
  targetRevision: v5.0.0
  bomPath: boms/sealos/v5.0.0/bom.yaml
  promotionHistory:
    - fromRevision: v5.0.0-rc.1
      toRevision: v5.0.0
      bomPath: boms/sealos/v5.0.0/bom.yaml
      reason: package acceptance passed
      approvedBy: release-team
      approvedAt: "2026-06-01T00:00:00Z"
      healthProofPath: proofs/sealos/v5.0.0/stable.yaml
      healthProofDigest: sha256:...
      healthProofSummary: all required checks passed
```

## Related Kinds

- `BOM` provides the target package set.
- `DistributionHealthProof` provides promotion evidence.
- `ClusterTarget` may select a release by channel.
- `DistributionTarget` may reconcile from a channel path.
