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
| `thresholds.requiredSignals` | No | Signal names that must be present and pass for promotion. |
| `thresholds.minPassedSignals` | No | Minimum number of passing signals required for the proof to pass. When omitted with no thresholds, legacy proofs require all signals to pass. |
| `signalSummary` | No | Normalized evaluation counts: total, passed, failed, required, failed/missing required, and minimum passing threshold. |
| `signals` | No | Individual health signals. |

## Signal Contract

Each signal records:

- `name`
- `passed`
- `required`
- `source`
- `evidenceRef`
- `message`

Signals should be deterministic and traceable to logs, test output, or a
`PackageAcceptanceReport`. `source` names the evidence producer, and
`evidenceRef` points at the field, stage, artifact, or log reference used to
derive the normalized signal.

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- `targetRevision` must be set.
- `passed` must reflect the aggregate result of required signals and the
  minimum passing-signal threshold.
- `collectedAt` must be an RFC3339 timestamp.
- `thresholds.requiredSignals` must not contain empty or duplicate names.
- Promotion rejects missing required signals, failed required signals, or too
  few passed signals. Failed optional signals are evidence and warnings when
  thresholds still pass.
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
  thresholds:
    requiredSignals:
      - package-acceptance
      - revert-check
    minPassedSignals: 2
  signalSummary:
    totalSignals: 2
    passedSignals: 2
    failedSignals: 0
    requiredSignals: 2
    passedRequiredSignals: 2
    minPassedSignals: 2
  signals:
    - name: package-acceptance
      passed: true
      required: true
      source: PackageAcceptanceReport
      evidenceRef: spec.status
      message: acceptance report completed successfully
    - name: revert-check
      passed: true
      required: true
      source: PackageAcceptanceReport
      evidenceRef: spec.stages[name=revert-check-revert]
      message: no managed object drift after revert
```

## Related Kinds

- `PackageAcceptanceReport` can provide raw acceptance details.
- `ReleaseChannel` references health proof during promotion.
- `BOM` identifies the revision under test.
