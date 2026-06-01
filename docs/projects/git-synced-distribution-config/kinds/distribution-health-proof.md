# Kind: DistributionHealthProof

## Status

Implemented file schema.

## Class

Evidence document.

## Owner

The validation system, release automation, or release owner writes health proof
documents. Human reviewers may approve promotions based on them.

## Normal Locations

- `proofs/<distribution>/<revision>/<channel>.yaml`
- `evidence/health/<distribution>/<revision>.yaml`

## Purpose

`DistributionHealthProof` records whether a target revision passed the health
signals required for promotion. It is attached to `ReleaseChannel` promotion
history and should be treated as evidence, not source intent.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionHealthProof
metadata:
  name: sealos-v5.0.0-stable
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `line` | Yes | Distribution line. New writers may also mirror this as `distribution` when the schema is extended. |
| `targetRevision` | Yes | Revision being evaluated. |
| `passed` | Yes | Overall pass or fail result. |
| `summary` | No | Human-readable health summary. |
| `collectedAt` | Yes | RFC3339 timestamp for evidence collection. |
| `signals` | No | Individual health signals. |

## Signal Contract

Each signal records:

- `name`
- `passed`
- `message`

Signals should be deterministic and traceable to logs, test output, or a
`PackageAcceptanceReport`.

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- `targetRevision` must be set.
- `passed` must reflect the aggregate result of required signals.
- `collectedAt` must be an RFC3339 timestamp.
- Health proof documents must not contain secret values.

## Lifecycle

1. Test or acceptance workflows evaluate a candidate revision.
2. The workflow writes a health proof document.
3. Release promotion references the proof in `ReleaseChannel.promotionHistory`.
4. Reviewers and automation use the proof to audit why a revision moved.

## Boundaries

- `DistributionHealthProof` does not select the promoted revision by itself.
- `DistributionHealthProof` does not replace package acceptance reports.
- `DistributionHealthProof` does not carry kubeconfig, tokens, or secret data.
- `DistributionHealthProof` should not be edited after publication.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionHealthProof
metadata:
  name: sealos-v5.0.0-stable
spec:
  line: sealos
  targetRevision: v5.0.0
  passed: true
  summary: all required package acceptance checks passed
  collectedAt: "2026-06-01T00:00:00Z"
  signals:
    - name: package-acceptance
      passed: true
      message: acceptance report completed successfully
    - name: revert-check
      passed: true
      message: no managed object drift after revert
```

## Related Kinds

- `PackageAcceptanceReport` can provide raw acceptance details.
- `ReleaseChannel` references health proof during promotion.
- `BOM` identifies the revision under test.
