# Kind: AppliedInventory

## Status

Proposed file schema.

## Class

Runtime inventory document.

## Owner

The apply or inventory workflow writes this document.

## Normal Locations

- `state/<cluster>/applied-inventory.yaml`
- `out/state/<cluster>/applied-inventory.yaml`

## Purpose

`AppliedInventory` expands `AppliedRevision` with the concrete Kubernetes
objects, host paths, local resources, and package-owned resources that are
expected to exist after apply. It provides a stronger basis for drift detection
and orphan cleanup.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedInventory
metadata:
  name: prod-01-v5.0.0
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `clusterName` | Yes | Cluster name. |
| `revision` | Yes | Applied distribution revision. |
| `desiredStateDigest` | Yes | Desired state digest matching `AppliedRevision`. |
| `bundleDigest` | No | Digest of the hydrated bundle used to produce the inventory. |
| `k8sObjects` | No | Managed Kubernetes object identities. |
| `hostPaths` | No | Managed host path identities and ownership metadata. |
| `localResources` | No | Local resources created or consumed by the apply. |
| `components` | No | Component-level inventory grouping. |

## Inventory Identity

Inventory entries should include stable identifiers:

- Kubernetes group, version, kind, namespace, and name;
- host path plus target node or target selector;
- component and package ownership;
- hash or digest when available.

## Validation Rules

- `clusterName`, `revision`, and `desiredStateDigest` must be set.
- Inventory identities must be normalized.
- Duplicate managed object identities are invalid.
- Entries must identify an owning component or package when possible.
- Secret data must not be embedded.

## Lifecycle

1. Hydration predicts managed resources.
2. Apply confirms what was created, updated, or retained.
3. Inventory workflow writes the applied inventory.
4. Drift detection compares observed state with inventory.
5. Cleanup uses inventory to distinguish managed orphans from unmanaged
   resources.

## Boundaries

- `AppliedInventory` is generated runtime inventory, not source intent.
- `AppliedInventory` does not replace `AppliedRevision`.
- `AppliedInventory` should not include secret contents.
- `AppliedInventory` should not be hand-edited.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedInventory
metadata:
  name: prod-01-v5.0.0
spec:
  clusterName: prod-01
  revision: v5.0.0
  desiredStateDigest: sha256:...
  k8sObjects:
    - group: ""
      version: v1
      kind: ConfigMap
      namespace: kube-system
      name: kubeadm-config
      component: kubernetes
  hostPaths:
    - path: /etc/kubernetes/manifests/kube-apiserver.yaml
      target: allMasters
      component: kubernetes
```

## Related Kinds

- `AppliedRevision` records compact apply state.
- `HydratedBundle` predicts the desired inventory.
- `DistributionTarget` status may link to generated inventory.
