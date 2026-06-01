# Kind: PackageAcceptanceReport

## Status

Implemented generated file schema.

## Class

Acceptance evidence document.

## Owner

The package acceptance workflow writes this document.

## Normal Locations

- `reports/package-acceptance/<revision>/<cluster>.yaml`
- `evidence/package-acceptance/<revision>.yaml`

## Purpose

`PackageAcceptanceReport` records the result of package acceptance, including
preflight state, apply stages, revert checks, package mode, source mode, and
desired state digests. It gives promotion workflows concrete evidence before a
revision is moved into a release channel.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: PackageAcceptanceReport
metadata:
  name: sealos-v5.0.0-prod-01
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `clusterName` | No | Cluster or test target name. |
| `startedAt` | No | RFC3339 start time. |
| `finishedAt` | No | RFC3339 finish time. |
| `status` | Yes | Overall report status. |
| `exitCode` | No | Process exit code. |
| `mutatingApply` | No | Whether the test performed a mutating apply. |
| `revertCheck` | No | Whether revert validation ran. |
| `packageMode` | No | Package mode used by the workflow. |
| `bomFile` | No | BOM file path used by the test. |
| `bomName` | No | BOM name. |
| `bomRevision` | No | BOM revision. |
| `bomDigest` | No | BOM digest. |
| `workdir` | No | Working directory used by the workflow. |
| `runtimeRoot` | No | Runtime root. |
| `localRepo` | No | Local repo path when local mode was used. |
| `bundleDir` | No | Hydrated bundle directory. |
| `kubeconfig` | No | Kubeconfig path, not contents. |
| `hostRoot` | No | Host root used for tests. |
| `outputsFormat` | No | Output format selected by the workflow. |
| `desiredStateDigest` | No | Desired state digest under test. |
| `localRepoRevision` | No | Local repo revision under test. |
| `sourcePreflightState` | No | Source preflight result. |
| `runtimePreflightState` | No | Runtime preflight result. |
| `postApplyState` | No | State after apply. |
| `postRevertState` | No | State after revert. |
| `stages` | No | Stage-level command and result records. |
| `notes` | No | Additional non-secret notes. |

## Stage Contract

Each stage may include:

- `name`
- `status`
- `mutates`
- `startedAt`
- `finishedAt`
- `output`
- `command`
- `reason`

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- `spec.status` must be set.
- `finishedAt`, when set, must be RFC3339.
- Each stage must have a name and status.
- Secret values must not be embedded. Paths to secret-bearing files may be
  recorded only when necessary for audit.

## Lifecycle

1. Package acceptance runs against a BOM and package mode.
2. The workflow records stage results and desired state digests.
3. A health proof may summarize one or more acceptance reports.
4. Release promotion references the health proof or report evidence.

## Boundaries

- `PackageAcceptanceReport` is evidence, not release intent.
- `PackageAcceptanceReport` does not select the release channel.
- `PackageAcceptanceReport` should not contain full logs when those logs may
  include secrets.
- `PackageAcceptanceReport` should be immutable after publication.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: PackageAcceptanceReport
metadata:
  name: sealos-v5.0.0-prod-01
spec:
  clusterName: prod-01
  status: Passed
  startedAt: "2026-06-01T00:00:00Z"
  finishedAt: "2026-06-01T00:10:00Z"
  mutatingApply: true
  revertCheck: true
  packageMode: sourceFirstLocalBuild
  bomFile: boms/sealos/v5.0.0/bom.yaml
  bomName: sealos-v5.0.0
  bomRevision: v5.0.0
  bomDigest: sha256:...
  desiredStateDigest: sha256:...
  localRepoRevision: 2026-06-01T00-00-00Z
  stages:
    - name: source-preflight
      status: Passed
      mutates: false
    - name: apply
      status: Passed
      mutates: true
```

## Related Kinds

- `DistributionHealthProof` summarizes health evidence for promotion.
- `BOM` identifies the release under test.
- `HydratedBundle` provides the desired state tested by acceptance.
- `ReleaseChannel` promotion may reference the resulting evidence.
