# Kind: DistributionRolloutPolicy

## Status

Implemented Kubernetes CRD.

## Class

Runtime rollout policy API.

## Owner

Cluster operators or platform owners maintain rollout policies.

## Kubernetes API

- Group: `distribution.sealos.io`
- Version: `v1alpha1`
- Kind: `DistributionRolloutPolicy`
- Scope: Namespaced
- Short name: `distrollout`

## Purpose

`DistributionRolloutPolicy` defines how a `DistributionTarget` rollout should
proceed. It keeps rollout behavior separate from release selection and package
content.

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `strategy` | No | Rollout strategy used by reconciliation. |
| `strategy.batchSize` | No | Batch size for staged rollout. |
| `canary.batchSize` | No | Canary batch size. |
| `pause.afterCanary` | No | Whether to pause after canary. |
| `healthGate` | No | Health gate configuration. |
| `failureAction` | No | Failure behavior, such as `Stop` or `Rollback`. |

The Go type delegates strategy shape to the reconcile rollout strategy model.
The CRD exposes common rollout controls such as batch, canary, pause, health
gate, and failure action.

## Status Contract

Status records:

- `observedGeneration`
- `conditions`

## Validation Rules

- Rollout policy names must be stable because `DistributionTarget` references
  them by name.
- `failureAction`, when set, must be one of the supported values.
- Batch settings must be compatible with the controller implementation.
- Status is controller-owned.

## Lifecycle

1. Platform owner creates rollout policies.
2. `DistributionTarget` references a policy by name.
3. The controller reads the policy during reconciliation.
4. Status records whether the policy was accepted or blocked.

## Boundaries

- `DistributionRolloutPolicy` does not select releases.
- `DistributionRolloutPolicy` does not define package contents.
- `DistributionRolloutPolicy` does not carry cluster-specific component input.
- `DistributionRolloutPolicy` does not store acceptance evidence.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionRolloutPolicy
metadata:
  name: default-rollout
  namespace: distribution-system
spec:
  strategy:
    batchSize: 3
  canary:
    batchSize: 1
  pause:
    afterCanary: true
  healthGate:
    enabled: true
  failureAction: Stop
```

## Related Kinds

- `DistributionTarget` references the rollout policy.
- `AppliedRevision` records the applied result.
- `PackageAcceptanceReport` and `DistributionHealthProof` provide evidence
  outside the rollout policy itself.
