# Kind: LocalRepoRevision

## Status

Implemented as a local repo file schema. It is not a Kubernetes CRD.

## Class

Local source evidence document.

## Owner

`sealos sync local-repo init` writes the initial `current` revision document.
Cluster owners may refresh it after local repo edits when they need an explicit
audit checkpoint.

## Purpose

`LocalRepoRevision` records the local input revision, the full local repo digest,
BOM identity, and audit metadata for a cluster-local repo snapshot. It gives
operators a durable reference for the local facts used around render/apply
without storing Secret payloads inline.

## Location

```text
local-repo/revisions/current.yaml
```

## Spec Contract

| Field | Description |
| --- | --- |
| `cluster` | Cluster name this revision belongs to. |
| `distributionLine` | Distribution line this revision follows. |
| `channel` | Optional release channel selected at init time. |
| `bom.name` | BOM name selected at init time. |
| `bom.revision` | BOM revision selected at init time. |
| `bom.digest` | Optional digest of the selected BOM file. |
| `localInputRevision` | Digest of `inputs/**` only. |
| `digest` | Digest of local repo inputs, resources, patches, and policy. |
| `audit.createdAt` | RFC3339 creation time. |
| `audit.createdBy` | Optional user or automation identity. |
| `audit.command` | Optional command that wrote the document. |

## Validation

`localrepo.Load` validates `apiVersion`, `kind`, identity fields, required
digests, and RFC3339 audit time when `revisions/current.yaml` is present. Older
local repos remain loadable; `sync local-repo doctor` reports a warning if the
revision file is missing or still uses an older illustrative shape.

## Boundaries

- `LocalRepoRevision` does not define the target release.
- `LocalRepoRevision` does not approve local policy changes.
- `LocalRepoRevision` does not carry Secret material.
- Render/apply still use the live local repo digest, so editing inputs after
  init does not require rewriting this audit object before render.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepoRevision
metadata:
  name: current
spec:
  cluster: prod-01
  distributionLine: default-platform
  channel: stable
  bom:
    name: default-platform
    revision: rev-2026-06-01
    digest: sha256:...
  localInputRevision: sha256:...
  digest: sha256:...
  audit:
    createdAt: "2026-06-03T00:00:00Z"
    command: sealos sync local-repo init
```

## Related Kinds

- `LocalRepo` names the mutable local repository.
- `HydratedBundle` records the local revision used for rendering.
- `AppliedRevision` records the local revision used for an apply.
