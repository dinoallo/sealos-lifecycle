# Sub-Design: Local Patch Policy Source And Scope

## Status

Draft with current single-node MVP behavior

## Summary

This document defines what a `LocalPatchPolicy` is allowed to govern, where it
is allowed to come from, and how Sealos should carry its provenance after
render.

The current decision is intentionally narrow:

- `LocalPatchPolicy` governs only cluster-local override surfaces
- the policy document scope must be `clusterLocal`
- the current supported sources are only:
  - `localRepo`
  - `builtInDefault`
- package and BOM content do not define local-patch policy in the current MVP
- after render, the bundle-carried policy artifact becomes the effective policy
  source of truth for compare, validation, and `sync commit`

## Related Documents

- Local repo layout and secret handling:
  [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md)
- Local patch policy authoring and review workflow:
  [sealos-local-patch-policy-authoring-and-review.md](./sealos-local-patch-policy-authoring-and-review.md)
- Tracking and drift model:
  [sealos-materialization-tracking-and-drift-detection-model.md](./sealos-materialization-tracking-and-drift-detection-model.md)
- Ownership and reconcile model:
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- Operator action reference:
  [sealos-sync-operator-action-reference.md](./sealos-sync-operator-action-reference.md)
- Current policy schema:
  [pkg/distribution/ownership/document.go](../pkg/distribution/ownership/document.go)
- Current rendered-policy handling:
  [pkg/distribution/hydrate/policy.go](../pkg/distribution/hydrate/policy.go)
- Current plan assembly:
  [pkg/distribution/reconcile/materialize.go](../pkg/distribution/reconcile/materialize.go)

## Why This Needs A Separate Design

The package-format design already defines:

- what package content may contain
- which inputs may be bound from outside the package
- which content is `global` by default

The local-repo guide already defines:

- where cluster-local values and resources should live
- why they should not silently mutate shared package artifacts

What remained ambiguous was narrower:

- who is allowed to define the local-patch allowlist itself
- whether that allowlist is part of package/BOM baseline or cluster-local state
- how rendered bundles should prove which policy they actually used

This document closes that gap.

## Decision

### 1. Policy Scope Is Always `clusterLocal`

`LocalPatchPolicy` does not describe reusable package behavior. It describes
which local override surfaces one cluster is allowed to use.

So the policy object itself is scoped to cluster-local ownership:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: custom-local-patch-policy
spec:
  scope: clusterLocal
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
```

In the current MVP:

- `spec.scope: clusterLocal` is the only supported value
- an omitted scope is interpreted as `clusterLocal` for legacy compatibility
- any other scope is rejected

### 2. Supported Policy Sources Are Only `localRepo` And `builtInDefault`

The current bundle provenance model supports only two policy sources:

- `localRepo`
  - the cluster-local repo explicitly provided
    `policy/local-patch-policy.yaml`
- `builtInDefault`
  - Sealos rendered the built-in default policy because the local repo did not
    provide one

This is reflected in bundle metadata:

- `bundle.spec.localPatchPolicySource`
- `bundle.spec.localPatchPolicyScope`
- `bundle.spec.localPatchPolicyName`
- `bundle.spec.localPatchPolicyPath`
- `bundle.spec.localPatchPolicyDigest`

### 3. Package And BOM Content Do Not Define Local-Patch Policy

The current MVP intentionally rejects package-side or BOM-side local-patch
policy as a supported source.

The reason is architectural, not accidental:

- package/BOM content defines shared baseline
- `LocalPatchPolicy` defines which cluster-local mutations are acceptable
- letting shared baseline producers silently widen local mutation surfaces would
  blur the global/local ownership boundary

Put differently:

- package/BOM may define extension points
- but they do not currently define the cluster's local mutation policy

### 4. The Rendered Bundle Is The Effective Policy Carrier

Once render completes, the effective policy is no longer inferred from ambient
repo state. It is carried by the bundle revision itself.

That bundle-carried policy is what later consumers must use for:

- local patch validation during render
- mismatch `policyEligible` annotation during compare
- local patch overlay extraction during `sync commit`
- operator-visible policy provenance in `sync diff` and `sync status`

This keeps the rendered revision self-describing.

## Resolution Rules

The current resolution order is:

1. If `local-repo/policy/local-patch-policy.yaml` exists, use it.
2. Otherwise, use the built-in default policy document.
3. Render the chosen document into `bundle/policy/local-patch-policy.yaml`.
4. Record source, scope, name, path, and digest in `bundle.yaml`.
5. Require later policy consumers to read the rendered bundle artifact, not the
   ambient local repo.

The current MVP does not merge multiple policy layers.

There is exactly one effective policy document per rendered bundle revision.

## Legacy Compatibility

Two compatibility rules are intentionally kept:

- a legacy bundle that has no explicit local-patch policy metadata is still
  interpreted as using the built-in default policy
- a legacy policy document with no `spec.scope` is still interpreted as
  `clusterLocal`

Compatibility stops there.

If a bundle explicitly claims unsupported provenance, or if its recorded
name/scope/path/digest do not match the rendered artifact, policy consumers
should reject that bundle instead of guessing.

## Rejected Alternatives

### Package-Scoped Local Patch Policy

Rejected for now because it would let package producers define the local
mutation envelope for every consuming cluster, which is stronger than the
current ownership model allows.

### BOM-Scoped Local Patch Policy

Rejected for now because a BOM is still part of global release selection. It
can choose baseline artifacts, but it does not currently own the cluster-local
override budget.

### Layered Merge Of Package + BOM + Local Repo Policy

Rejected for now because it adds precedence complexity before the simpler
single-policy model is proven.

## Future Extension Gate

If Sealos later needs package/BOM-scoped policy, that should not be added as a
silent third source.

It should require a separate design that answers at least:

- what the new scope means
- who reviews widening of allowed local surfaces
- whether cluster-local policy may further narrow, but not widen, shared policy
- how rendered provenance distinguishes baseline-owned policy from
  cluster-local policy

Until that design exists, the current rule stays simple:

- policy scope is `clusterLocal`
- supported sources are `localRepo` and `builtInDefault`
