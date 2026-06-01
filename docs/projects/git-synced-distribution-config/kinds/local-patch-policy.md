# Kind: LocalPatchPolicy

## Status

Implemented file schema.

## Class

Ownership and policy document.

## Owner

The package owner and local cluster owner jointly maintain the policy. The
policy should make ownership boundaries reviewable before patches are applied.

## Normal Locations

- `ownership/local-patch-policy.yaml`
- `packages/<category>/<name>/ownership/local-patch-policy.yaml`
- `clusters/<cluster>/ownership/local-patch-policy.yaml`

## Purpose

`LocalPatchPolicy` declares what local patches are allowed to change, who owns
those changes, and which changes require explicit approval. It lets a cluster
carry local differences without losing control of upstream package ownership.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: kubernetes-local-patches
spec: {}
```

## Spec Shape

The policy contains a scope and policy rules. Exact rule fields may evolve, but
the document must answer these questions:

- Which package, component, or cluster scope does this policy cover?
- Which files, Kubernetes objects, or host paths may be patched locally?
- Which changes are always allowed?
- Which changes require a gate approval?
- Which changes are forbidden?
- Who owns review for each policy area?

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- Scope must be explicit.
- Paths and selectors must be deterministic.
- A policy must not grant ownership over undeclared package files.
- Widening a policy should trigger a gate approval.
- Incompatible local patches should trigger a gate violation.

## Lifecycle

1. Package or cluster owners define allowed local patch boundaries.
2. Local patches are checked against the policy before hydration or apply.
3. Gate violations require `LocalPatchPolicyGateApproval`.
4. Hydration records the policy source and digest in `HydratedBundle`.
5. Runtime drift reports distinguish expected local changes from unmanaged drift.

## Boundaries

- `LocalPatchPolicy` does not carry the patch content itself.
- `LocalPatchPolicy` does not approve policy widening by itself.
- `LocalPatchPolicy` does not contain secrets.
- `LocalPatchPolicy` does not replace runtime drift evidence.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: kubernetes-local-patches
spec:
  scope:
    component: kubernetes
    package: core/kubernetes
  policy:
    allow:
      - path: patches/kubernetes/manifests/**
        owner: cluster-platform
    requireApproval:
      - path: rootfs/etc/kubernetes/**
        owner: kubernetes-package-owner
    deny:
      - path: rootfs/usr/bin/kubelet
        reason: binary replacement must come from source build
```

## Related Kinds

- `ComponentPackage` may reference a local patch policy.
- `BOM` may reference the release-level local patch policy.
- `LocalPatchPolicyGateApproval` approves gate violations.
- `HydratedBundle` records the policy used during rendering.
