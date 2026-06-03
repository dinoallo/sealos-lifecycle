# Kind: HydratedBundle

## Status

Implemented generated file schema.

## Class

Generated bundle document.

## Owner

The hydration workflow writes this document. Humans review it but should not
hand-edit it.

## Normal Locations

- `out/hydrated/<cluster>/<revision>/bundle.yaml`
- `bundles/<cluster>/<revision>/hydrated-bundle.yaml`

## Purpose

`HydratedBundle` records the fully rendered desired state for a target cluster
and revision. It captures source provenance, local patch policy provenance,
local resources, Kubernetes objects, host paths, and component render output.

It is the bridge between declarative source documents and runtime apply.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: HydratedBundle
metadata:
  name: prod-01-v5.0.0
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `bomName` | Yes | BOM name used for hydration. |
| `revision` | Yes | BOM or release revision. |
| `channel` | No | Release channel used to resolve the revision. |
| `renderProvenance` | Yes | Source documents, digests, local repo revision, and package sources. |
| `sourcePreflight` | No | Source validation summary before render. |
| `executionTopology` | No | Execution plan shape for cluster or host targets. |
| `localPatchPolicySource` | No | Source of the local patch policy. |
| `localPatchPolicyScope` | No | Scope covered by the local patch policy. |
| `localPatchPolicyName` | No | Policy name. |
| `localPatchPolicyPath` | No | Policy path. |
| `localPatchPolicyDigest` | No | Policy digest. |
| `localResources` | No | Local resources created or consumed by hydration. |
| `trackedK8sObjects` | No | Kubernetes objects managed by this bundle. |
| `trackedHostPaths` | No | Host paths managed by this bundle. |
| `components` | Yes | Rendered component outputs. |

## Provenance Requirements

`renderProvenance` should include:

- `releaseChannelPath` and digest, when a channel was used;
- `distributionLine`;
- `bomPath` and digest;
- `localRepoPath` and local revision, when local mode was used;
- `localPatchRevision`;
- package source paths and digests.

This provenance must be enough to reproduce or audit the rendered desired
state.

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- `bomName` and `revision` must be set.
- Digests, when present, must use supported digest formats.
- Tracked object identities must be stable.
- Host paths must be normalized and must not be ambiguous.
- The bundle must not embed secret material.

## Lifecycle

1. Source documents and cluster intent are resolved.
2. Source preflight and local patch gates run.
3. Hydration renders package output into a bundle.
4. Apply consumes the bundle.
5. Runtime state records the applied bundle through `AppliedRevision`.

## Boundaries

- `HydratedBundle` is generated output, not source intent.
- `HydratedBundle` should not be edited by hand.
- `HydratedBundle` does not approve local patch gates.
- `HydratedBundle` is not the long-lived runtime status object.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: HydratedBundle
metadata:
  name: prod-01-v5.0.0
spec:
  bomName: sealos-v5.0.0
  revision: v5.0.0
  channel: stable
  renderProvenance:
    releaseChannelPath: channels/sealos/stable.yaml
    releaseChannelDigest: sha256:...
    bomPath: boms/sealos/v5.0.0/bom.yaml
    bomDigest: sha256:...
    localRepoPath: /var/lib/sealos/distribution/local-repo
    localRepoRevision: 2026-06-01T00-00-00Z
    packageSources:
      - component: kubernetes
        path: packages/core/kubernetes/v1.31.1
        digest: sha256:...
  localPatchPolicyPath: ownership/local-patch-policy.yaml
  localPatchPolicyDigest: sha256:...
  components:
    - name: kubernetes
      version: v1.31.1
```

## Related Kinds

- `BOM` supplies the selected packages.
- `ClusterTarget` and `ComponentInput` supply cluster-specific intent.
- `LocalPatchPolicy` controls local patch boundaries.
- `AppliedRevision` records the applied result.
- `AppliedInventory` may expand managed object inventory in the future.
