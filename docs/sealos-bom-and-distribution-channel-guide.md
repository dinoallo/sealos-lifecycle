# Guide: BOMs, Revisions, and DistributionChannel Semantics

## Status

Design guide with implementation notes

## Summary

This guide explains how Sealos should think about:

- `ComponentPackage` revisions
- `BOM` revisions
- `distribution lines`
- `DistributionChannel` objects
- Day 0 and Day 1 revision selection

It exists because these concepts currently appear across several design
documents, while the current PoC code still uses a simpler transition model in
which `spec.channel` lives inside the BOM itself.

## Related Documents

- Top-level architecture:
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md)
- Release and promotion policy:
  [sealos-multi-cluster-release-and-promotion-design.md](./sealos-multi-cluster-release-and-promotion-design.md)
- Ownership and drift:
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- Derived distribution workflow:
  [sealos-derived-distribution-walkthrough.md](./sealos-derived-distribution-walkthrough.md)
- Current BOM schema:
  [pkg/distribution/bom/types.go](../pkg/distribution/bom/types.go)
- Applied revision state:
  [pkg/distribution/state/types.go](../pkg/distribution/state/types.go)
- Current materialization path:
  [pkg/distribution/reconcile/materialize.go](../pkg/distribution/reconcile/materialize.go)

## Why This Guide Exists

The current design needs one place that answers these questions plainly:

- What is a BOM?
- What is a BOM revision?
- What is the difference between a BOM and a distribution line?
- What should a cluster choose at Day 0?
- What is `DistributionChannel`, and why should it exist separately from the
  BOM?
- What does the current repo already implement, and what is still design-only?

This guide gives one consistent answer to all of them.

## Core Objects

| Object | Meaning | Mutability |
| --- | --- | --- |
| `ComponentPackage revision` | One immutable component artifact referenced by OCI digest. | Immutable |
| `BOM component entry` | One component selection inside a BOM, including version, artifact reference, and dependency names. | Immutable as part of one BOM revision |
| `BOM revision` | One digest-pinned set of component selections that defines one releasable baseline snapshot. | Immutable |
| `Distribution line` | One named lineage of BOM revisions that operators treat as one release family over time. | Evolves by publishing new BOM revisions |
| `DistributionChannel` | One mutable release object that says which BOM revision is currently recommended for one channel on one distribution line. | Mutable |
| `AppliedRevision` | Cluster-local state that records what the cluster last rendered or applied. | Mutable cluster state |

The most important rule is:

- packages are component building blocks
- a BOM revision is one concrete release snapshot
- a distribution line is the sequence of those snapshots over time
- a `DistributionChannel` is the moving head that points clusters at the
  current snapshot for one rollout stage

## What A BOM Is

A BOM is the release object that says:

- which components are part of this baseline
- which exact package images and digests each component uses
- which component depends on which other components
- which revision identifier names this exact baseline snapshot

In the current schema, that object is defined in
[pkg/distribution/bom/types.go](../pkg/distribution/bom/types.go).

The key fields today are:

- `metadata.name`
- `spec.revision`
- `spec.channel`
- `spec.components[]`

### Recommended Semantics

The cleanest reading of those fields is:

- `metadata.name`
  The BOM family or line-facing name. In practice this should normally stay
  stable across revisions on the same distribution line.
- `spec.revision`
  The immutable snapshot identifier for this exact BOM revision.
- `spec.components[]`
  The actual component graph and digest-pinned artifact set.
- `spec.channel`
  A transitional field in the current implementation, useful as metadata today,
  but not the ideal long-term release-head model.

This means a preferred naming pattern is:

- `metadata.name: default-platform`
- `spec.revision: rev-007`

and then later:

- `metadata.name: default-platform`
- `spec.revision: rev-008`

If you fork into a derived distribution line, you would usually change the BOM
family name as well:

- `metadata.name: corp-platform`
- `spec.revision: rev-corp-001`

## What A BOM Is Not

A BOM is not:

- a local repo
- a secret store
- a runtime state snapshot
- a drift record
- a release channel head

Those boundaries matter because a BOM must stay reviewable and reproducible.
Secret bytes, local overlays, and generated runtime objects do not belong in it.

## Current BOM Schema Shape

The current PoC-style BOM shape looks like this:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: minimal-single-node
spec:
  revision: rev-poc-001
  channel: alpha
  components:
    - name: containerd
      kind: infra
      version: v1.7.18
      artifact:
        name: containerd-runtime
        image: registry.example/platform/containerd-runtime:v1.7.18
        digest: sha256:<digest>
    - name: kubernetes
      kind: infra
      version: v1.30.3
      dependencies:
        - containerd
      artifact:
        name: kubernetes-rootfs
        image: registry.example/platform/kubernetes-rootfs:v1.30.3
        digest: sha256:<digest>
```

Important current rules:

- `spec.revision` is required
- `spec.channel` is required today
- `spec.components` is required
- every component artifact digest is required
- component dependency names must refer to other component names in the same BOM

Those validations are enforced in
[pkg/distribution/bom/types.go](../pkg/distribution/bom/types.go).

## About `baseArtifacts`

The BOM schema also includes `spec.baseArtifacts`, but the current PoC and the
current walkthroughs are centered on `spec.components`.

For now, the practical reading is:

- `components` are the main first-class release graph
- `baseArtifacts` is available in the schema for future shared artifact needs,
  but it is not the central story in the current repo docs

That is why most examples and design discussion focus on `components`.

## Why `spec.channel` In BOM Is Only Transitional

The current code still keeps `spec.channel` inside the BOM itself. That is
simple for the MVP, but it is not the clean long-term shape.

The reason is straightforward:

- one BOM revision may be validated first in `alpha`
- then later promoted to `beta`
- then later promoted to `stable`

If `channel` is an immutable property inside the BOM, the system gets pushed
toward one of two awkward outcomes:

- mutate a BOM that should be immutable
- clone the same BOM content several times just to change `channel`

Neither is a good release model.

So the design direction is:

- keep BOM revisions immutable
- move the mutable channel head into `DistributionChannel`

## What `DistributionChannel` Should Mean

`DistributionChannel` is the release object that answers one question:

For this distribution line and this channel, which BOM revision is current?

That is a line-level decision, not a package-level decision.

For example:

- `default-platform / stable` -> `rev-007`
- `default-platform / beta` -> `rev-009`
- `default-platform / alpha` -> `rev-012`

Then each of those BOM revisions still contains the full component graph and
package digests.

### Suggested Shape

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionChannel
metadata:
  name: default-platform-stable
spec:
  line: default-platform
  channel: stable
  targetRevision: rev-007
```

That shape keeps responsibilities clean:

- the BOM defines one immutable snapshot
- the `DistributionChannel` tells clusters which snapshot to follow for one
  rollout stage

## Current Implementation vs Target Model

| Topic | Current Repo Behavior | Target Design Direction |
| --- | --- | --- |
| How a cluster chooses a target | Explicit BOM file path | Explicit BOM revision, or `distribution line + DistributionChannel` |
| Where channel metadata lives | `BOM.spec.channel` | `DistributionChannel` object |
| What `sync render` resolves today | One BOM document passed in directly | One resolved BOM revision after optional channel lookup |
| What applied state records | BOM name, revision, and channel | BOM name, revision, and the channel or explicit target that led to that revision |

This distinction is important because the current code path in
[pkg/distribution/reconcile/materialize.go](../pkg/distribution/reconcile/materialize.go)
does not resolve a channel head yet. It loads one explicit BOM document and
materializes that.

## Day 0 Selection

At Day 0, a cluster should not infer its release target from package content or
live state. It should be assigned one of these two target shapes:

1. an explicit BOM revision
2. a `distribution line + channel` pair

### Preferred Decision Order

1. Choose the distribution line.
2. Decide whether the cluster is pinned to one explicit revision or follows a
   channel.
3. If it follows a channel, resolve that channel to one concrete BOM revision.
4. Render and apply that resolved BOM revision.
5. Persist the chosen revision in the cluster's applied state.

### Practical Cohort Guidance

| Cluster Type | Usual Day 0 Choice |
| --- | --- |
| Internal bring-up or aggressive field trial | `alpha` |
| Canary or pilot cluster | `beta` |
| General production cluster | `stable` |
| Regulated or tightly controlled rollout | Explicit BOM revision pin |

### Important Current Limitation

Today, the current repo only implements the explicit BOM document path.

That means the current Day 0 workflow is effectively:

- choose a specific BOM file
- pass it to `sealos sync render`

It does not yet implement:

- `DistributionChannel` lookup
- "follow the latest stable revision on this line" resolution

## Day 1 To Day N Behavior

Once Day 0 is complete, clusters should behave differently depending on whether
they are pinned or channel-following.

### Pinned Revision

If a cluster is pinned to one BOM revision:

- it does not move when a channel advances
- it changes only when the operator explicitly selects a new BOM revision

### Channel-Following Cluster

If a cluster follows a `DistributionChannel`:

- it re-resolves that channel over time
- it moves only when the `DistributionChannel` target revision advances
- it should still persist the exact resolved BOM revision it last applied

This keeps operational intent and concrete state separate:

- intent: "follow `default-platform/stable`"
- concrete result: "currently on `rev-007`"

## Applied Revision State

The current applied-state model already records:

- BOM name
- BOM revision
- BOM channel

See [pkg/distribution/state/types.go](../pkg/distribution/state/types.go).

That is useful even before `DistributionChannel` is fully implemented, because
the cluster still needs a durable record of what exact baseline it last
materialized.

Longer term, the most useful state shape would capture both:

- the requested target form
  - explicit revision pin, or
  - `distribution line + channel`
- the resolved BOM revision actually rendered and applied

## How Derived Distributions Fit

Derived distributions do not fork live drift. They fork release lineage.

In BOM terms, that usually means:

- publish a new BOM family name or release namespace
- publish one or more new BOM revisions under that line
- optionally create separate `DistributionChannel` objects for that line

That is why a derived distribution is best understood as a new distribution
line, not as "whatever the cluster currently drifted into."

## Practical Rules Of Thumb

- If you want one exact reproducible baseline, point at one BOM revision.
- If you want controlled rollout, follow a `DistributionChannel`.
- If you need long-lived divergence from the upstream baseline, fork the
  distribution line and publish new BOM revisions there.
- Do not treat `spec.channel` inside today's BOM schema as the final release
  architecture. Treat it as a useful transition field until channel resolution
  is modeled explicitly.

## What Still Needs To Be Designed Or Implemented

- The final `DistributionChannel` schema
- Resolution rules from `distribution line + channel` to one BOM revision
- How channel advancement history is stored and audited
- Whether `BOM.spec.channel` should become optional first and then be removed
  later
- The exact Day 0 operator interface for choosing pinned versus channel-based
  targets

This guide does not require those pieces to be fully implemented first. It only
defines how they should fit together coherently.
