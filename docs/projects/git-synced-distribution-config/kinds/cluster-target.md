# Kind: ClusterTarget

## Status

Proposed file schema. This is a cluster configuration repository document, not
an implemented Kubernetes CRD.

## Class

Cluster intent document.

## Owner

The cluster configuration owner maintains `ClusterTarget` documents.

## Normal Locations

- `clusters/<cluster>/target.yaml`
- `targets/<cluster>.yaml`

## Purpose

`ClusterTarget` declares which distribution line, channel, profile, and delivery
mode a cluster should follow. It belongs in a separate cluster configuration
repository so distribution source configuration and cluster-specific intent can
evolve independently.

`ClusterTarget` is the source-of-truth input for cluster selection. It is not
the runtime reconciliation object. Runtime reconciliation is represented by the
`DistributionTarget` CRD.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ClusterTarget
metadata:
  name: prod-01
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `distribution` | Yes | Distribution line selected by the cluster. |
| `channel` | Yes | Release channel followed by the cluster. |
| `profile` | No | Cluster profile, such as `default`, `prod`, or `edge`. |
| `delivery.mode` | Yes | Delivery mode. Expected values are `sourceFirstLocalBuild` or `nonLocalBuild`. |
| `distributionRef.name` | No | Named distribution repository reference. |
| `distributionRef.ref` | No | Git ref, tag, or revision for the distribution repository. |
| `localPatchRevision` | No | Selected local patch revision. |
| `inputs` | No | References to `ComponentInput` documents. |
| `patches` | No | References to cluster-owned patch files. |
| `secrets` | No | References to external secret locations by path or name, never inline secret values. |

## Delivery Modes

`sourceFirstLocalBuild` means the cluster workflow can build packages from
repository source facts when artifacts are missing or intentionally not used.

`nonLocalBuild` means the cluster workflow only consumes published artifacts
referenced by the selected `BOM`.

Both modes should share the same distribution package model. The mode changes
execution policy, not the meaning of package identity.

## Validation Rules

- `distribution`, `channel`, and `delivery.mode` must be set.
- `delivery.mode` must be one of the supported modes.
- `inputs[*].path`, `patches[*].path`, and referenced files must be
  repository-relative.
- `secrets` must reference secret locations and must not embed secret values.
- A `localPatchRevision` should be pinned when cluster-owned patches are
  present.

## Lifecycle

1. The cluster owner selects a release channel and delivery mode.
2. The cluster owner pins local patch and input references.
3. A controller or materializer resolves the target against distribution source
   documents.
4. Runtime reconciliation is created or updated through `DistributionTarget`.

## Boundaries

- `ClusterTarget` does not define package source or package content.
- `ClusterTarget` does not implement rollout execution.
- `ClusterTarget` does not store runtime status.
- `ClusterTarget` must not contain secret material.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ClusterTarget
metadata:
  name: prod-01
spec:
  distribution: sealos
  channel: stable
  profile: prod
  delivery:
    mode: sourceFirstLocalBuild
  distributionRef:
    name: sealos-distribution
    ref: main
  localPatchRevision: prod-01-2026-06-01
  inputs:
    - component: kubernetes
      path: inputs/kubernetes.yaml
  patches:
    - path: patches/kubernetes/
  secrets:
    - path: external-secrets/prod-01.yaml
```

## Related Kinds

- `ReleaseChannel` resolves the selected channel.
- `BOM` resolves the selected revision.
- `ComponentInput` provides non-secret cluster values.
- `DistributionTarget` is the runtime CRD derived from this intent.
- `AppliedRevision` records the applied result.
