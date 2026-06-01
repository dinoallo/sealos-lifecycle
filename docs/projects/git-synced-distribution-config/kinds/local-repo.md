# Kind: LocalRepo

## Status

Illustrative only. Not implemented as a schema or CRD.

## Class

Local repository model document.

## Owner

The local cluster platform owner would maintain this model if it becomes a real
schema.

## Purpose

`LocalRepo` names the local mirror or local fact repository used by source-first
local build mode. It describes where local source facts, cached artifacts, and
local patch revisions are stored.

The current implementation does not require this kind. It is documented to keep
the local mirror concept explicit and to reserve vocabulary for future schema
work.

## Possible Locations

- `local-repos/<name>.yaml`
- `clusters/<cluster>/local-repo.yaml`

## Possible Spec Contract

| Field | Description |
| --- | --- |
| `root` | Filesystem or repository root for local facts. |
| `mode` | Mirror mode, such as `sourceMirror`, `artifactMirror`, or `mixed`. |
| `distributionRef` | Distribution source repository mirrored locally. |
| `cacheRoot` | Local cache root for generated or fetched artifacts. |
| `patchRoot` | Local patch root. |
| `retention` | Retention policy for cached revisions. |

## Validation Expectations

If implemented, a `LocalRepo` should validate that:

- roots are explicit and normalized;
- no path escapes the configured repository root;
- secret material is not stored inline;
- mirror state is recorded through immutable revisions rather than mutable
  names alone.

## Boundaries

- `LocalRepo` should not replace `BOM`.
- `LocalRepo` should not replace `ReleaseChannel`.
- `LocalRepo` should not be treated as runtime apply state.
- `LocalRepo` is currently documentation vocabulary, not an API contract.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepo
metadata:
  name: prod-01-local
spec:
  root: /var/lib/sealos/distribution/local-repo
  mode: mixed
  distributionRef:
    name: sealos-distribution
    ref: main
  cacheRoot: cache/
  patchRoot: patches/
```

## Related Kinds

- `LocalRepoRevision` would identify an immutable snapshot of this local repo.
- `ClusterTarget` may select a local delivery mode.
- `HydratedBundle` records local repo provenance when used.
- `DistributionTarget` has runtime fields for local repo paths.
