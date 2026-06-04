# Kind: LocalRepo

## Status

Implemented as a local repo file schema. It is not a Kubernetes CRD.

## Class

Local source document.

## Owner

The cluster owner maintains the local repo; `sealos sync local-repo init` writes
the initial document.

## Purpose

`LocalRepo` identifies the cluster-local repository used during render, apply,
status, and commit workflows. It records the cluster and distribution line that
the local inputs, local resources, patches, and policy belong to.

## Location

```text
local-repo/repo.yaml
```

## Spec Contract

| Field | Description |
| --- | --- |
| `cluster` | Cluster name this local repo belongs to. |
| `distributionLine` | Distribution line the local repo follows. |
| `channel` | Optional release channel selected at init time. |
| `bom` | BOM name used to initialize the local repo. |
| `bomRevision` | BOM revision used to initialize the local repo. |

## Validation

`localrepo.Load` validates `apiVersion`, `kind`, `metadata.name`, `spec.cluster`,
and `spec.distributionLine` when `repo.yaml` is present. Older local repos remain
loadable; `sync local-repo doctor` reports a warning if the metadata file is
missing or still uses an older illustrative shape.

## Boundaries

- `LocalRepo` does not replace `BOM`.
- `LocalRepo` does not replace `ReleaseChannel`.
- `LocalRepo` does not carry Secret payloads.
- `LocalRepo` is not runtime apply state.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepo
metadata:
  name: prod-01-default-platform
spec:
  cluster: prod-01
  distributionLine: default-platform
  channel: stable
  bom: default-platform
  bomRevision: rev-2026-06-01
```

## Related Kinds

- `LocalRepoRevision` records a digest-backed audit snapshot for this local repo.
- `HydratedBundle` records local repo provenance when used.
- `AppliedRevision` records the local repo revision digest used by render/apply.
