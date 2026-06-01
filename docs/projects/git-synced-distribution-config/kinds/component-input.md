# Kind: ComponentInput

## Status

Proposed file schema.

## Class

Cluster intent document.

## Owner

The cluster configuration owner maintains `ComponentInput` documents, usually
with review from the package owner when new inputs are introduced.

## Normal Locations

- `clusters/<cluster>/inputs/<component>.yaml`
- `inputs/<cluster>/<component>.yaml`

## Purpose

`ComponentInput` supplies cluster-specific non-secret values for the inputs
declared by a `ComponentPackage`. It keeps package defaults and cluster-specific
configuration separate.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentInput
metadata:
  name: prod-01-kubernetes
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `component` | Yes | Component receiving these values. |
| `values` | Yes | Structured non-secret values keyed by the declared input contract. |
| `profile` | No | Profile selector if one file serves multiple profiles. |
| `targetRevision` | No | Optional revision pin when values are known to be revision-specific. |

## Validation Rules

- `component` must match a selected `ComponentPackage`.
- Every top-level value should correspond to a declared package input.
- Values must not contain secrets.
- Paths, when used as values, must be repository-relative unless the input
  contract explicitly allows runtime paths.
- Unknown values should fail validation unless a package explicitly allows open
  structured input.

## Lifecycle

1. A package declares accepted inputs.
2. Cluster owners provide cluster-specific values through `ComponentInput`.
3. Hydration merges package defaults and component inputs.
4. `HydratedBundle` records the input source provenance, not secret material.

## Boundaries

- `ComponentInput` does not define package contents.
- `ComponentInput` does not select a distribution revision.
- `ComponentInput` must not carry secret values.
- `ComponentInput` does not represent runtime state.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentInput
metadata:
  name: prod-01-kubernetes
spec:
  component: kubernetes
  values:
    clusterCIDR: 10.244.0.0/16
    serviceCIDR: 10.96.0.0/12
    controlPlaneEndpoint: api.prod-01.example.com:6443
```

## Related Kinds

- `ComponentPackage` declares accepted inputs.
- `ClusterTarget` references component input files.
- `HydratedBundle` records rendered values and input provenance.
