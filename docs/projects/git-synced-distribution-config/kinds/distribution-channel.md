# Kind: DistributionChannel

## Status

Implemented compatibility alias. New documents should use `ReleaseChannel`.

## Class

Legacy release source document.

## Owner

The release owner maintains legacy documents only when required for
compatibility.

## Normal Locations

- Existing legacy channel files.

## Purpose

`DistributionChannel` is the older name for the channel pointer now represented
by `ReleaseChannel`. It exists so older repositories can still be loaded while
the document model converges on clearer names.

## Compatibility Contract

The loader accepts both:

```yaml
kind: DistributionChannel
```

and:

```yaml
kind: ReleaseChannel
```

It also accepts legacy `spec.line` and normalizes it to `spec.distribution` when
possible.

## Required Fields

The effective required fields are the same as `ReleaseChannel`:

- `distribution` or legacy `line`
- `channel`
- `targetRevision`
- `bomPath`

## Migration Guidance

For new or edited documents:

1. Change `kind` from `DistributionChannel` to `ReleaseChannel`.
2. Change `spec.line` to `spec.distribution` if present.
3. Keep `channel`, `targetRevision`, `bomPath`, and `promotionHistory`.
4. Re-run schema validation.

## Boundaries

- Do not add new semantics to `DistributionChannel`.
- Do not create new documents with this kind unless compatibility requires it.
- Treat `ReleaseChannel` as the canonical kind in proposals and examples.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionChannel
metadata:
  name: sealos-stable
spec:
  line: sealos
  channel: stable
  targetRevision: v5.0.0
  bomPath: boms/sealos/v5.0.0/bom.yaml
```

## Related Kinds

- `ReleaseChannel` is the canonical replacement.
- `BOM` is the release package set referenced by the channel.
