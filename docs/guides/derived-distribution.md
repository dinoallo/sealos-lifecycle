# Walkthrough: Forking A Cluster Into A Derived Distribution

## Status

Current design walkthrough

## Summary

This document explains how a cluster should diverge from the shared Sealos
baseline when it does not want, or cannot safely accept, a global baseline
change.

The key rule is that a cluster should not silently mutate global-owned baseline
content and remain on the same distribution line. If the divergence is expected
to last, the supported path is to create a derived distribution:

- usually by creating a new BOM revision
- sometimes by also publishing one or more forked component package revisions
- optionally by maintaining a separate release channel lineage

This walkthrough is based on the current design documents and the minimal
single-node PoC assets in this repository. It is not a description of a fully
productized one-command workflow.

## Related Documents

- Top-level architecture:
  [Distribution and config sync](../architecture/distribution-and-config-sync.md)
- Reconcile, ownership, and drift states:
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- Release channels and promotion policy:
  [Release and promotion](../architecture/release-and-promotion.md)
- BOM and `ReleaseChannel` semantics:
  [BOM and channel](../guides/bom-and-channel.md)
- Component package contract:
  [Package format](../architecture/package-format.md)
- Current PoC BOM:
  [scripts/poc/minimal-single-node/bom.yaml](../../scripts/poc/minimal-single-node/bom.yaml)

## Terminology Used Here

This walkthrough uses the top-level terminology from
[Distribution and config sync](../architecture/distribution-and-config-sync.md):

- a `BOM revision` is one concrete releasable baseline snapshot
- a `distribution snapshot` is the full platform state represented by that BOM
  revision
- a `distribution line` is the named lineage of BOM revisions that a cluster
  follows over time

In other words, this walkthrough is about forking a `distribution line`, not
about preserving an untracked live-state mutation.

## The Decision Ladder

Not every incompatibility should lead to a forked distribution. Use this ladder
in order:

1. Stay on the current baseline if no change is needed.
2. Use local binding if the difference is a legitimate per-cluster variation.
3. Pin the cluster to an older BOM revision if the new baseline is not yet
   acceptable but no independent distribution line is needed.
4. Create a derived BOM if the cluster needs a durable, tracked divergence from
   the shared baseline.
5. Publish forked package revisions only for the components that really need to
   differ.

The important design intent is that divergence must become an explicit revision
object, not an untracked live-state mutation.

## What Should Not Happen

If a cluster directly modifies global-owned baseline content and keeps following
the same upstream BOM or channel, it is no longer on a supported distribution
line. In the ownership model this is the kind of behavior that leads toward
`Orphan` state.

That is why the fork target must be:

- a new BOM revision
- and only when necessary, new component package revisions

It should not be “whatever state the cluster drifted into.”

## Three Levels Of Divergence

### 1. Local Variation

Use local binding when the difference is expected to vary by cluster:

- CIDRs
- endpoints
- mirror settings
- certificates
- MTU
- environment-specific values

This keeps the cluster on the same baseline and does not create a new
distribution line.

### 2. Shared Policy Difference

Use a derived BOM plus a shared patch or replacement package when a group of
clusters wants a different platform policy:

- different audit policy
- different admission defaults
- different Cilium policy profile
- different hardening overlays

This creates a durable branch of the shared baseline without pretending the
difference is merely local.

### 3. True Distribution Fork

Fork the distribution when the cluster, or a cohort of clusters, must reject a
global baseline change for an extended period and continue evolving on its own
track.

Typical signals:

- a component version is incompatible with local environment constraints
- the operator wants a different CNI or runtime policy as a product choice
- the cluster needs a long-lived alternative hardening profile
- the cluster must keep accepting some upstream changes while permanently
  refusing others

## What A Derived Distribution Is

In this design, a derived distribution is usually:

- a new BOM `metadata.name` and/or `spec.revision`
- a digest-pinned component set that may selectively reuse upstream component
  digests
- optionally a separate channel or release namespace

The most important property is that the derived line is explicit and
reproducible. A derived distribution is therefore best read as a derived
`distribution line` whose concrete releases are one or more derived `BOM
revisions`.

## The Normal Fork Pattern

The normal fork pattern is selective, not total:

1. Start from an existing upstream BOM revision.
2. Copy that BOM into a new derived BOM.
3. Keep most upstream component artifact digests unchanged.
4. Replace only the incompatible components with your own package revisions.
5. Publish the new BOM and point the affected cluster at it.

This is much cheaper than forking every package.

## Step-By-Step Walkthrough

### Step 1: Identify The Type Of Divergence

Ask:

- Is this just a cluster-specific value?
- Is this a shared policy difference?
- Is this an actual incompatible component or release choice?

If it is just a cluster-specific value, stop here and use local binding.

If it changes global-owned package intent, do not keep treating it as local.
Move on to a derived BOM.

### Step 2: Pick The Upstream Starting Point

Choose the exact upstream BOM revision you are forking from.

In the PoC, that starting point is:

- BOM name: `minimal-single-node`
- revision: `rev-poc-001`
- file:
  [scripts/poc/minimal-single-node/bom.yaml](../../scripts/poc/minimal-single-node/bom.yaml)

This gives you a stable baseline to compare against later.

### Step 3: Decide Whether A BOM Fork Is Enough

Often you only need a BOM fork, not a package fork.

Examples:

- pinning the cluster to an older component digest that still exists
- choosing a different already-published package revision
- swapping one component artifact reference while keeping the others the same

If the required package revision does not exist yet, then you also need to fork
and publish that package.

### Step 4: Create The Derived BOM

Make a copy of the upstream BOM and give it a distinct identity.

Typical changes:

- change `metadata.name`
- change `spec.revision`
- in the current BOM schema, optionally change `spec.channel`
- in the target release model, publish or update a separate
  `ReleaseChannel`
- keep the dependency graph explicit
- keep unchanged components on their upstream artifact digests

For example:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: corp-minimal-single-node
  labels:
    distribution.sealos.io/profile: corp
spec:
  revision: rev-corp-001
  channel: alpha
  components:
    - name: containerd
      kind: infra
      version: v1.7.18
      artifact:
        name: containerd-runtime
        image: local/poc/containerd-runtime:v1.7.18
        digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
    - name: kubernetes
      kind: infra
      version: v1.30.3
      dependencies:
        - containerd
      artifact:
        name: kubernetes-rootfs
        image: local/poc/kubernetes-rootfs:v1.30.3
        digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
    - name: cilium
      kind: infra
      version: v1.15.0-corp.1
      dependencies:
        - kubernetes
      artifact:
        name: cilium-cni
        image: registry.example.io/corp/cilium-cni:v1.15.0-corp.1
        digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
```

This example shows the normal case:

- `containerd` and `kubernetes` still reuse upstream artifacts
- only `cilium` has been replaced
- the distribution identity now belongs to the derived line

### Step 5: Fork And Publish Only The Needed Packages

If a component must differ, build a new package revision for that component and
push it to your own registry namespace.

The repo already demonstrates the package build/push shape through:

- `sealos sync package build`
- `sealos sync package push`

If the divergence is a Cilium-specific policy or compatibility issue, you would
normally:

1. copy or derive from
   `scripts/poc/minimal-single-node/packages/cilium`
2. adjust the package payload or packaged defaults
3. build a new OCI package image
4. push it under your own registry path
5. record the new image and digest in the derived BOM

The key point is that a derived distribution should point at immutable package
revisions, not at mutable in-cluster state.

### Step 6: Point The Cluster At The Derived BOM

Once the derived BOM exists, the affected cluster should stop following the old
upstream line and start reconciling to the derived BOM instead.

Conceptually, this means the cluster now has a different target baseline
revision. It is no longer just “the same cluster with a few exceptional local
changes.”

### Step 7: Keep Future Changes Explicit

After the fork, new changes should still happen through revision objects:

- publish new package revisions if the forked components evolve
- publish new derived BOM revisions when the component set changes
- do not bypass the derived line by editing live global-owned content directly

This keeps the fork reproducible and reviewable.

## Minimal PoC Example

The current PoC BOM is:

- [scripts/poc/minimal-single-node/bom.yaml](../../scripts/poc/minimal-single-node/bom.yaml)

If one cluster cannot accept the upstream Cilium choice but everything else is
fine, the smallest supported fork is:

1. keep `containerd` unchanged
2. keep `kubernetes` unchanged
3. publish a new `cilium-cni` package revision under a different registry path
4. create a new BOM revision that points only the `cilium` component at the new
   digest

This gives you a new distribution line with the least possible divergence.

## Rebase Strategy

Forking a distribution does not mean rejecting all upstream change forever.

The healthy maintenance pattern is selective rebase:

1. review a newer upstream BOM revision
2. copy forward the component digests you still accept
3. keep your forked component digests where incompatibility remains
4. publish a new derived BOM revision

This lets the cluster absorb compatible upstream fixes without giving up its
required divergence.

## Release And Promotion Implications

Once a derived distribution exists, it should be treated as its own reviewable
release line.

That means:

- its BOM revisions should be digest-pinned
- its component replacements should be auditable
- its health evidence should be evaluated on its own terms
- it should not silently feed incompatible local changes back into upstream
  `Stable`

If the derived line later proves broadly useful, it may become:

- an upstream candidate revision
- a shared patch package
- or a new supported baseline variant

## Practical Guardrails

- Do not fork because of a value that should simply be a declared `input`.
- Do not leave a cluster on the upstream line after changing global-owned
  package intent locally.
- Do not fork every component when only one component is incompatible.
- Do not treat a drifted live cluster as the source of truth for the fork.
- Do not lose digest pinning when creating the derived BOM.

## Current Repo Limits

This walkthrough describes the design-supported workflow, but the current repo
does not yet provide a fully productized “fork this cluster into a new
distribution” command.

Today the repo gives you:

- digest-pinned BOMs
- OCI component package build and push commands
- render/apply paths that consume a BOM
- design guidance for ownership, promotion, and review

What it does not yet give you is a first-class CLI that automatically:

- clones a BOM
- rewrites only the changed artifact references
- assigns new release metadata
- persists the derived line as a managed release object

So today, the fork is a disciplined document-and-artifact workflow rather than a
single built-in command.

## Bottom Line

When a cluster truly cannot accept a global baseline change, the supported path
is to fork the distribution line, not to carry a silent live-state divergence.

In practice that means:

1. decide whether this is really more than local variation
2. copy the upstream BOM into a new derived BOM
3. reuse unchanged component digests
4. replace only the incompatible components with forked package revisions
5. point the cluster at the new BOM line
6. maintain that line explicitly through new BOM and package revisions
