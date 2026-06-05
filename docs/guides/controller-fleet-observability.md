# Controller Fleet Observability Runbook

## Status

Current operations contract

## Purpose

This runbook defines the current observability surface for
`sealos-agent --controller` fleets. It turns `DistributionTarget` status,
`DistributionRolloutPolicy`, Kubernetes events, controller logs, and smoke
artifacts into a consistent view for target aggregation, rollout progress,
health evidence, promotion gates, and failure archives.

The current controller does not expose a dedicated metrics endpoint for these
fields and does not persist a per-host rollout cursor. Treat the Kubernetes API
objects and the captured artifacts below as the source of truth. External
dashboards can collect the same fields by watching `DistributionTarget` and
`DistributionRolloutPolicy` objects in `sealos-system`.

## Fleet State Aggregation

Start every fleet check from the CRD printer columns:

```bash
kubectl -n sealos-system get distributiontargets
kubectl -n sealos-system get distributionrolloutpolicies
```

Use the structured status when building a dashboard row or paging summary:

```bash
kubectl -n sealos-system get distributiontarget <target> -o yaml
kubectl -n sealos-system describe distributiontarget <target>
kubectl -n sealos-system get events \
  --field-selector involvedObject.kind=DistributionTarget,involvedObject.name=<target> \
  --sort-by=.lastTimestamp
```

One target row should expose:

| Column | Source |
| --- | --- |
| Namespace | `metadata.namespace` |
| Target | `metadata.name` |
| Cluster | `spec.clusterName` or `status.lastResult.clusterName` |
| Generation | `metadata.generation` |
| Observed generation | `status.observedGeneration` |
| Phase | `status.phase` |
| Ready | `status.conditions[type=Ready]` |
| Degraded | `status.conditions[type=Degraded]` |
| Revision | `status.lastResult.revision` |
| Channel | `status.lastResult.channel` |
| Desired digest | `status.lastResult.desiredStateDigest` |
| Applied revision | `status.lastResult.appliedRevisionPath` |
| Last reconcile | `status.lastReconcileTime` |
| Retry count | `status.retryCount` |
| Next retry | `status.nextRetryTime` |
| Hold reason | `status.holdReason` |
| Last diagnostic | `status.lastDiagnostic.reason` and `message` |

Use these aggregate counters at fleet level:

| Counter | Rule |
| --- | --- |
| `targetsTotal` | count all `DistributionTarget` objects |
| `targetsReady` | `Ready=True` and `phase=Succeeded` |
| `targetsDegraded` | `Degraded=True` |
| `targetsPaused` | `phase=Paused` |
| `targetsRollbackHold` | `phase=RollbackHold` |
| `targetsRetrying` | `phase=Retrying` |
| `targetsPartiallyFailed` | `phase=PartiallyFailed` |
| `targetsStaleGeneration` | `status.observedGeneration < metadata.generation` |
| `targetsDigestMismatch` | same line/channel but different `desiredStateDigest` where uniformity is expected |

## Rollout Progress

Rollout progress is currently observed from the target phase, target events,
the referenced `DistributionRolloutPolicy`, and controller logs. The executor
uses the rendered bundle as the rollout unit; eligible host-targeted steps can
be batched, canaried, paused after canary, health-gated, and rolled back by the
policy. It does not yet persist a durable per-host cursor in the CRD.

For each target, capture the rollout policy and event stream:

```bash
kubectl -n sealos-system get distributiontarget <target> \
  -o jsonpath='{.spec.rolloutPolicyRef.name}{"\n"}'
kubectl -n sealos-system get distributionrolloutpolicy <policy> -o yaml
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent --since=1h
```

Interpret phases as:

| Phase | Meaning | Operator Action |
| --- | --- | --- |
| `Succeeded` | Latest reconcile finished and `Ready=True`. | Keep evidence for promotion gates. |
| `Retrying` | Reconcile failed and `retryBackoff` scheduled another attempt. | Inspect `lastDiagnostic`, events, and logs before the next retry. |
| `PartiallyFailed` | The agent returned a result and an error. | Archive the partial result and inspect changed state before retrying. |
| `Paused` | Rollout paused after canary and is not degraded. | Review canary health evidence, then update the target or policy to continue. |
| `RollbackHold` | Rollback to the last successful rendered revision was triggered. | Treat as a hold; archive failure evidence before selecting the next revision. |

## Health Evidence

The target status proves what the controller last selected and applied. It does
not replace detailed health proof documents.

Minimum health evidence for one target:

- target YAML after reconcile
- `status.lastResult.bomName`, `revision`, `channel`, and
  `desiredStateDigest`
- `status.lastResult.bundlePath`
- `status.lastResult.appliedRevisionPath`
- `Ready` and `Degraded` conditions
- recent `DistributionTarget` events
- controller logs for the reconcile window
- smoke or acceptance artifact directory when the reconcile came from a test

When the candidate is used for promotion, also attach the matching
`DistributionHealthProof` and the package or PoC acceptance report that produced
it. The health proof is the promotion evidence; the controller target status is
runtime evidence showing where the candidate actually reconciled.

## Promotion Gate

A controller-backed promotion gate should require all of these checks before a
release channel advances:

| Gate | Required Evidence |
| --- | --- |
| Target selection | `DistributionTarget.spec` selects the intended BOM, local channel, or release metadata line/channel. |
| Reconciled generation | `status.observedGeneration == metadata.generation`. |
| Ready state | `phase=Succeeded`, `Ready=True`, and `Degraded=False`. |
| Revision identity | `status.lastResult.revision` and `desiredStateDigest` match the candidate BOM. |
| Health proof | A passing `DistributionHealthProof` exists for the candidate revision and required channel. |
| Artifact retention | Bundle path, applied revision path, controller logs, and events are archived. |
| Hold clearance | No target in the cohort is `Paused`, `RollbackHold`, `Retrying`, or `PartiallyFailed` unless the promotion explicitly excludes it. |

The controller does not promote channels by itself. Promotion still goes
through the release metadata service or `sealos sync promote` with health proof
evidence.

## Failure Archive

For every `Degraded=True`, `Retrying`, `PartiallyFailed`, or `RollbackHold`
target, create an incident directory and save:

```bash
mkdir -p /tmp/sealos-controller-incident/<target>
kubectl -n sealos-system get distributiontarget <target> -o yaml \
  > /tmp/sealos-controller-incident/<target>/target.yaml
kubectl -n sealos-system describe distributiontarget <target> \
  > /tmp/sealos-controller-incident/<target>/target.describe.txt
kubectl -n sealos-system get events \
  --field-selector involvedObject.kind=DistributionTarget,involvedObject.name=<target> \
  --sort-by=.lastTimestamp \
  > /tmp/sealos-controller-incident/<target>/target.events.txt
kubectl -n sealos-system get pods -l app.kubernetes.io/name=sealos-distribution-controller -o yaml \
  > /tmp/sealos-controller-incident/<target>/controller-pods.yaml
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent --since=2h \
  > /tmp/sealos-controller-incident/<target>/controller.log
```

Also copy the referenced bundle directory, applied revision file, health proof,
acceptance report, and real-cluster smoke diagnostics when they are available.
Do not archive kubeconfig contents, Secret object payloads, private keys, or
token-bearing files. Paths, digests, object names, events, and normalized status
fields are the expected diagnostic surface.

## Alert Routing

Use these alert headlines:

| Condition | Headline |
| --- | --- |
| `Degraded=True` | `distribution target degraded: <namespace>/<target> <reason>` |
| `phase=Retrying` past `nextRetryTime` | `distribution target retry overdue: <namespace>/<target>` |
| `phase=PartiallyFailed` | `distribution target partially failed: <namespace>/<target>` |
| `phase=Paused` | `distribution rollout paused after canary: <namespace>/<target>` |
| `phase=RollbackHold` | `distribution rollback hold: <namespace>/<target>` |
| stale generation | `distribution target generation not observed: <namespace>/<target>` |

Route package-owned baseline failures to the release owner, local repo and
patch failures to the cluster owner, host tool preflight failures to the
lifecycle node owner, and rollback holds to both the release owner and the
cluster owner.

## Closeout

Close an incident or promotion observation only after:

1. `kubectl get distributiontarget <target> -o yaml` shows the expected phase
   and conditions.
2. The expected revision and desired digest are present in `status.lastResult`.
3. Events and logs for the reconcile window are archived.
4. The matching health proof or acceptance report is linked when the evidence
   is used for promotion.
5. A post-change `sync status` or controller target status snapshot is attached
   when the incident involved drift or rollback.
