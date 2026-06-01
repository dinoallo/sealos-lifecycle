# Kind: LocalRepoRevision

## Status

Illustrative only. Not implemented as a schema or CRD.

## Class

Local repository evidence document.

## Owner

The local build or mirror workflow would write this document if the kind becomes
implemented.

## Purpose

`LocalRepoRevision` would identify an immutable snapshot of a `LocalRepo`. It
allows source-first local build mode to record exactly which local source facts,
patches, and cached artifacts were used for hydration or apply.

## Possible Locations

- `local-repos/<name>/revisions/<revision>.yaml`
- `clusters/<cluster>/local-repo-revisions/<revision>.yaml`

## Possible Spec Contract

| Field | Description |
| --- | --- |
| `localRepo` | Name of the local repository. |
| `revision` | Immutable local repo revision identifier. |
| `distributionRef` | Source distribution repository and ref. |
| `sourceDigest` | Digest of mirrored source facts. |
| `patchDigest` | Digest of local patch facts. |
| `artifactIndexDigest` | Digest of local artifact index. |
| `createdAt` | RFC3339 creation time. |

## Validation Expectations

If implemented, a `LocalRepoRevision` should validate that:

- revision identifiers are immutable;
- all referenced digests are supported digest formats;
- source and patch digests are computed from normalized file trees;
- generated artifacts can be traced back to source and build class provenance.

## Lifecycle

1. A local mirror or build workflow snapshots the local repo.
2. The workflow writes a revision document with digests.
3. Hydration records the selected local repo revision.
4. `AppliedRevision` records which local repo revision contributed to the
   applied state.

## Boundaries

- `LocalRepoRevision` does not define the target release.
- `LocalRepoRevision` does not approve local policy changes.
- `LocalRepoRevision` does not carry secret material.
- `LocalRepoRevision` is currently documentation vocabulary, not an API
  contract.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepoRevision
metadata:
  name: prod-01-local-2026-06-01
spec:
  localRepo: prod-01-local
  revision: 2026-06-01T00-00-00Z
  distributionRef:
    name: sealos-distribution
    ref: abc123
  sourceDigest: sha256:...
  patchDigest: sha256:...
  artifactIndexDigest: sha256:...
  createdAt: "2026-06-01T00:00:00Z"
```

## Related Kinds

- `LocalRepo` names the mutable local repository.
- `HydratedBundle` records the local revision used for rendering.
- `AppliedRevision` records the local revision used for an apply.
