# Kind: LocalPatchPolicyGateApproval

## Status

Implemented file schema.

## Class

Approval evidence document.

## Owner

The approving owner writes or signs this document. Automation validates that the
approval matches the detected gate violation.

## Normal Locations

- `approvals/local-patch-policy/<approval>.yaml`
- `clusters/<cluster>/approvals/<approval>.yaml`

## Purpose

`LocalPatchPolicyGateApproval` records explicit approval for a local patch
policy gate violation, such as widening policy scope or accepting incompatible
local patches.

It is evidence that a specific risk was reviewed. It is not a blanket bypass.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: kubernetes-policy-widening-2026-06-01
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `owner` | Yes | Owner responsible for the approval. |
| `approvedBy` | Yes | Person, team, or automation identity that approved the change. |
| `changeRef` | Yes | Stable reference to the change being approved. |
| `expiresAt` | No | RFC3339 expiration time. |
| `oldPolicy` | No | Reference or digest of the previous policy. |
| `newPolicy` | Yes | Reference or digest of the new policy. |
| `approvals` | Yes | Approval entries for concrete gate violations. |

Each approval entry should include:

- violation code
- expected count
- expected impact
- reason

## Gate Codes

Known gate codes include:

- `wideningChange`
- `incompatiblePatches`
- `approvalExpiresSoon`

## Validation Rules

- The approval must match the policy references being evaluated.
- The approval must not be expired.
- The expected impact must match the detected gate impact.
- The approval must not approve more violations than declared.
- A missing approval for a required gate must fail validation.

## Lifecycle

1. Policy validation detects a gate violation.
2. The responsible owner reviews the change.
3. The owner writes a gate approval for the exact change.
4. Automation validates the approval during hydration or apply.
5. Evidence is retained for audit.

## Boundaries

- This document does not define local patch policy.
- This document does not contain patch content.
- This document does not approve unrelated future changes.
- This document must not include secret values.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: kubernetes-policy-widening-2026-06-01
spec:
  owner: kubernetes-package-owner
  approvedBy: release-team
  changeRef: git:abc123
  expiresAt: "2026-07-01T00:00:00Z"
  oldPolicy:
    path: ownership/local-patch-policy.yaml
    digest: sha256:...
  newPolicy:
    path: ownership/local-patch-policy.yaml
    digest: sha256:...
  approvals:
    - code: wideningChange
      expectedCount: 1
      expectedImpact: allow rootfs etc/kubernetes patches
      reason: production cluster requires kubeadm config override
```

## Related Kinds

- `LocalPatchPolicy` defines the policy being approved.
- `HydratedBundle` records the policy and approval provenance.
- `AppliedRevision` can surface policy-related conditions.
