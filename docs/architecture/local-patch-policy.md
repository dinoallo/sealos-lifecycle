# Sub-Design: Local Patch Policy Source And Scope

## Status

Implemented MVP contract

## Summary

This document defines what a `LocalPatchPolicy` is allowed to govern, where it
is allowed to come from, and how Sealos should carry its provenance after
render.

The current decision is intentionally narrow:

- `LocalPatchPolicy` governs only cluster-local override surfaces
- the policy document scope must be `clusterLocal`
- the current supported sources are:
  - `localRepo`
  - `bom`
  - `package`
  - `builtInDefault`
- package and BOM content may select a cluster-local policy artifact, but they
  still do not create a package/BOM-scoped policy surface
- after render, the bundle-carried policy artifact becomes the effective policy
  source of truth for compare, validation, and `sync commit`
- the canonical resolver is `hydrate.SelectLocalPatchPolicy`; render and
  `sync validate` use the same source-selection and precedence result

## Related Documents

- Local repo layout and secret handling:
  [Local repo and secret](../guides/local-repo-and-secret.md)
- Local patch policy authoring and review workflow:
  [Local patch policy authoring](../guides/local-patch-policy-authoring.md)
- Tracking and drift model:
  [Materialization and drift](../architecture/materialization-and-drift.md)
- Ownership and reconcile model:
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- Operator action reference:
  [Sync operator actions](../reference/sync-operator-actions.md)
- Current policy schema:
  [pkg/distribution/ownership/document.go](../../pkg/distribution/ownership/document.go)
- Current rendered-policy handling:
  [pkg/distribution/hydrate/policy.go](../../pkg/distribution/hydrate/policy.go)
- Current plan assembly:
  [pkg/distribution/reconcile/materialize.go](../../pkg/distribution/reconcile/materialize.go)
- Operator preflight output:
  [cmd/sealos/cmd/sync_validate.go](../../cmd/sealos/cmd/sync_validate.go)

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

### 2. Supported Policy Sources

The current bundle provenance model supports these policy sources:

- `localRepo`
  - the cluster-local repo explicitly provided
    `policy/local-patch-policy.yaml`
- `bom`
  - the selected BOM references a reviewed policy file through
    `spec.localPatchPolicy`
- `package`
  - exactly one BOM-selected component package references a reviewed policy
    file through `spec.localPatchPolicy`
- `builtInDefault`
  - Sealos rendered the built-in default policy because no explicit source
    provided one

This is reflected in bundle metadata:

- `bundle.spec.localPatchPolicySource`
- `bundle.spec.localPatchPolicyScope`
- `bundle.spec.localPatchPolicyName`
- `bundle.spec.localPatchPolicyPath`
- `bundle.spec.localPatchPolicyDigest`

### 3. Package And BOM Sources Still Carry `clusterLocal` Policy

Package-side and BOM-side sources select the effective cluster-local policy for
the rendered bundle. They do not introduce package/BOM-scoped policy.

- package/BOM content defines shared baseline
- `LocalPatchPolicy` defines which cluster-local mutations are acceptable
- the policy document still must use `spec.scope: clusterLocal`

Put differently:

- package/BOM may define extension points
- package/BOM may select a reviewed policy artifact
- package/BOM may not currently define a different ownership scope or merge
  policy layers

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
2. Otherwise, if the selected BOM declares `spec.localPatchPolicy`, load that
   policy file relative to the BOM file.
3. Otherwise, if exactly one selected component package declares
   `spec.localPatchPolicy`, load that policy file relative to that package
   root.
4. Otherwise, use the built-in default policy document.
5. Render the chosen document into `bundle/policy/local-patch-policy.yaml`.
6. Record source, scope, name, path, and digest in `bundle.yaml`.
7. Require later policy consumers to read the rendered bundle artifact, not the
   ambient local repo.

The canonical implementation of steps 1-4 is
`hydrate.SelectLocalPatchPolicy`. `sync render` uses that selection when
materializing the bundle, and `sync validate` exposes the same decision through
`localPolicySource`, `localPolicy`, `localPolicyName`, `localPolicyScope`, and
`localPolicyCandidates`. The candidates list is an operator-audit view of which
external policy declarations were present and which one won precedence.

The current MVP does not merge multiple policy layers. If the package source
would be the effective source and more than one package declares a policy,
render fails instead of guessing.

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

### Package-Scoped Policy Scope

Rejected for now. A package may select a `clusterLocal` policy artifact, but it
does not define a package-owned policy scope.

### BOM-Scoped Policy Scope

Rejected for now. A BOM may select a `clusterLocal` policy artifact for the
rendered revision, but it does not define a BOM-owned policy scope.

### Layered Merge Of Package + BOM + Local Repo Policy

Rejected for now because it adds precedence complexity before the simpler
single-policy model is proven.

## Future Extension Gate

If Sealos later needs package/BOM-scoped policy, that should not be added by
reusing the current source-selection fields.

It should require a separate design that answers at least:

- what the new scope means
- who reviews widening of allowed local surfaces
- whether cluster-local policy may further narrow, but not widen, shared policy
- how rendered provenance distinguishes baseline-owned policy from
  cluster-local policy

Until that design exists, the current rule stays simple:

- policy scope is `clusterLocal`
- supported sources are `localRepo`, `bom`, `package`, and `builtInDefault`
- there is exactly one rendered effective policy
