# Kind: DistributionTarget

## Status

Implemented Kubernetes CRD.

## Class

Runtime reconciliation API.

## Owner

Cluster operators create or update this CRD. The distribution controller
reconciles it.

## Kubernetes API

- Group: `distribution.sealos.io`
- Version: `v1alpha1`
- Kind: `DistributionTarget`
- Scope: Namespaced
- Short name: `disttarget`

## Purpose

`DistributionTarget` tells the distribution controller which BOM or release
channel path should be reconciled for a runtime target. It is the Kubernetes API
counterpart to source-side target intent.

It is not the same as `ClusterTarget`. `ClusterTarget` is a proposed Git
document in a cluster configuration repository. `DistributionTarget` is the live
CRD consumed by the controller.

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `clusterName` | No | Logical cluster name. |
| `bomPath` | One of the supported target selectors | Direct path to a BOM. |
| `releaseChannelPath` | One of the supported target selectors | Path to a release channel document. |
| `releaseSource` | With `releaseLine` and `channel` | Release metadata source URL or directory. |
| `releaseLine` | With `releaseSource` and `channel` | Distribution line resolved from the release metadata source. |
| `channel` | With `releaseSource` and `releaseLine` | Release channel name. |
| `localRepoPath` | No | Local repository path for local source or artifact use. |
| `localPatchRevision` | No | Local patch revision to apply. |
| `packageSources` | No | Explicit package source paths and digests by component. |
| `cacheRoot` | No | Runtime cache root. |
| `kubeconfigPath` | No | Kubeconfig path, not contents. |
| `hostRoot` | No | Host root used by reconciliation. |
| `rolloutPolicyRef` | No | Reference to a `DistributionRolloutPolicy`. |
| `rolloutBatchSize` | No | Inline batch size override. |
| `requeueAfter` | No | Successful reconcile requeue interval. |
| `retryBackoff` | No | Failed reconcile backoff before retry. |

## Status Contract

Status records:

- `observedGeneration`
- `phase`
- `lastReconcileTime`
- `lastResult.clusterName`
- `lastResult.bomName`
- `lastResult.revision`
- `lastResult.channel`
- `lastResult.bundlePath`
- `lastResult.desiredStateDigest`
- `lastResult.appliedRevisionPath`
- `retryCount`
- `nextRetryTime`
- `holdReason`
- `lastDiagnostic`
- `conditions`

`phase` is one of:

- `Succeeded`
- `Retrying`
- `PartiallyFailed`
- `Paused`
- `RollbackHold`

## Validation Rules

- Exactly one target selector must be set: `bomPath`, `releaseChannelPath`, or
  `releaseSource` with `releaseLine` and `channel`.
- Paths must be meaningful to the controller runtime.
- `requeueAfter` and `retryBackoff` must not be negative.
- Secret values must not be embedded in the spec.
- Status is controller-owned.

## Lifecycle

1. A cluster operator creates or updates `DistributionTarget`.
2. The controller resolves the BOM or release channel.
3. The controller hydrates and applies the desired state.
4. Status records the latest reconcile result and applied revision path.

## Boundaries

- `DistributionTarget` does not define package contents.
- `DistributionTarget` does not replace Git source documents.
- `DistributionTarget` does not store long-form acceptance evidence.
- `DistributionTarget` should reference secret-bearing files only by path.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionTarget
metadata:
  name: prod-01
  namespace: distribution-system
spec:
  clusterName: prod-01
  releaseChannelPath: channels/sealos/stable.yaml
  localRepoPath: /var/lib/sealos/distribution/local-repo
  localPatchRevision: prod-01-2026-06-01
  cacheRoot: /var/cache/sealos/distribution
  kubeconfigPath: /etc/sealos/kubeconfig
  hostRoot: /
  rolloutPolicyRef:
    name: default-rollout
  requeueAfter: 5m
  retryBackoff: 30s
```

## Related Kinds

- `DistributionRolloutPolicy` controls rollout behavior.
- `ReleaseChannel` and `BOM` provide source selection.
- `HydratedBundle` is generated during reconciliation.
- `AppliedRevision` records applied runtime state.
