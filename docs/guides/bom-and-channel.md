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
which `spec.channel` lives inside the BOM itself. The repository now also
supports a narrow local-file `DistributionChannel` path for selecting a BOM
before render.

## Related Documents

- Top-level architecture:
  [Distribution and config sync](../architecture/distribution-and-config-sync.md)
- Release and promotion policy:
  [Release and promotion](../architecture/release-and-promotion.md)
- Ownership and drift:
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- Derived distribution workflow:
  [Derived distribution](../guides/derived-distribution.md)
- Current BOM schema:
  [pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go)
- Applied revision state:
  [pkg/distribution/state/types.go](../../pkg/distribution/state/types.go)
- Current materialization path:
  [pkg/distribution/reconcile/materialize.go](../../pkg/distribution/reconcile/materialize.go)
- Current local `DistributionChannel` resolver:
  [pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go)

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
[pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go).

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
  localPatchPolicy: policy/local-patch-policy.yaml
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
- `spec.localPatchPolicy` is optional; when set, it is a relative path to a
  `LocalPatchPolicy` file next to the BOM and takes precedence over package
  policy sources
- every component artifact digest is required
- component dependency names must refer to other component names in the same BOM

Those validations are enforced in
[pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go).

## Local Test Registry

For local Distribution package tests, you can run the upstream
[`distribution/distribution`](https://github.com/distribution/distribution)
registry on the same host and address it as `registry.sealos.local:5000`.
This keeps the package image references close to production OCI references
without requiring a remote registry account.

Create a host entry:

```bash
echo "127.0.0.1 registry.sealos.local" | sudo tee -a /etc/hosts
```

Start a local registry container:

```bash
docker run -d --restart=always \
  --name sealos-local-registry \
  -p 5000:5000 \
  registry:2
```

The local registry above is plain HTTP. For test-only `sealos sync package`
build and push commands, point Sealos/buildah at an insecure registry config:

```bash
cat > /tmp/sealos-local-registries.conf <<'EOF'
unqualified-search-registries = ["docker.io"]

[[registry]]
location = "registry.sealos.local:5000"
insecure = true
EOF
```

Build and push a Kubernetes rootfs component package:

```bash
sealos --registries-conf /tmp/sealos-local-registries.conf \
  sync package build \
  --package-dir scripts/poc/minimal-single-node/packages/kubernetes \
  --image registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3 \
  --platform linux/amd64

sealos --registries-conf /tmp/sealos-local-registries.conf \
  sync package push \
  --image registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3 \
  --destination registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3
```

Capture the digest printed by `sync package push`, then pin the BOM component
to the local registry image:

```yaml
artifact:
  name: kubernetes-rootfs
  image: registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3
  digest: sha256:<digest>
```

Use TLS and a real registry policy for shared or production environments. The
insecure registry config above is intended only for local development.

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
  bomPath: bom.yaml
```

That shape keeps responsibilities clean:

- the BOM defines one immutable snapshot
- the `DistributionChannel` tells clusters which snapshot to follow for one
  rollout stage

## Current Implementation vs Target Model

| Topic | Current Repo Behavior | Target Design Direction |
| --- | --- | --- |
| How a cluster chooses a target | Explicit BOM file path, or a local `DistributionChannel` file passed with `--distribution-channel` | Explicit BOM revision, or `distribution line + DistributionChannel` lookup |
| Where channel metadata lives | `BOM.spec.channel`, plus local channel selection metadata in render provenance when a `DistributionChannel` file is used | `DistributionChannel` object |
| What `sync render` resolves today | A BOM document passed directly, or a local `DistributionChannel` whose `spec.bomPath` points at the BOM to load | One resolved BOM revision after optional channel lookup |
| What applied state records | BOM name, revision, and channel; rendered bundles also record BOM and local `DistributionChannel` provenance | BOM name, revision, and the channel or explicit target that led to that revision |

This distinction is important because the current code path in
[pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go)
resolves only local channel documents. It validates that the channel `line`
matches the target BOM `metadata.name`, that `targetRevision` matches the BOM
`spec.revision`, and then renders that concrete BOM. It does not provide live
lookup for "latest stable on this distribution line" yet.

The same local-file boundary also has a small promotion primitive:
`sealos sync promote`. It advances one local `DistributionChannel` file to a
target BOM file after checking target-channel policy, requiring local health
proof for beta/stable targets, and recording an approver, reason, timestamp,
and promotion history entry. That gives
file-backed channel followers a reviewable channel advancement path without
implying registry/API-backed release lookup.

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

### Important Current Boundary

Today, the current repo implements two local document paths:

- choose a specific BOM file and pass it to `sealos sync render --file`
- choose a local `DistributionChannel` file and pass it to
  `sealos sync render --distribution-channel`

The local `DistributionChannel` must name the distribution line, channel,
target revision, and `spec.bomPath` for the target BOM. The CLI resolves the
channel to that local BOM before materialization.

It does not yet implement:

- registry/API-backed `DistributionChannel` lookup
- "follow the latest stable revision on this line" resolution

## Local Channel Promotion

For the current local-file model, promotion means updating one
`DistributionChannel` document so that `spec.targetRevision` and `spec.bomPath`
point at a different BOM revision on the same distribution line.

Use:

```bash
sealos sync promote \
  --distribution-channel channels/default-platform-stable.yaml \
  --target-bom boms/default-platform/rev-008.yaml \
  --health-proof proofs/default-platform-rev-008-health.yaml \
  --reason "beta cohort passed source preflight and rollout validation" \
  --approved-by release-team
```

The command validates that:

- the channel document is a valid `DistributionChannel`
- the target BOM is a valid BOM
- `DistributionChannel.spec.line` matches `BOM.metadata.name`
- the default promotion policy allows the target channel to advance to the
  candidate BOM's source channel
- if the target channel requires proof, `--health-proof` points to a valid
  `DistributionHealthProof` that targets the same line and BOM revision,
  reports `spec.passed: true`, includes at least one signal, and has no
  failed signals

It then writes the updated channel file and appends
`spec.promotionHistory[]` with:

- the previous revision
- the new revision
- the BOM path written into the channel
- the reason
- the approver
- the approval timestamp
- the health proof path, digest, and summary when `--health-proof` is used

The current local-file promotion policy is intentionally small and
deterministic:

| Target channel | Allowed candidate `BOM.spec.channel` | Health proof |
| --- | --- | --- |
| `alpha` | `alpha` | not required |
| `beta` | `alpha`, `beta` | required |
| `stable` | `beta`, `stable` | required |

This blocks an unvalidated `alpha` candidate from skipping directly to
`stable`, and it treats missing proof for `beta` or `stable` as a policy
failure rather than an implicit approval.

### Generate Proof From Acceptance Reports

For package lifecycle automation, `sealos sync health-proof` can turn the
`PackageAcceptanceReport` emitted by the minimal single-node smoke flow into a
promotion-ready `DistributionHealthProof`:

```bash
sealos sync health-proof \
  --file boms/default-platform/rev-008.yaml \
  --acceptance-report workdir/acceptance-report.yaml \
  --output-file proofs/default-platform-rev-008-health.yaml \
  --summary "beta cohort passed apply and drift recovery validation"
```

The generated proof targets the line and revision from the BOM passed with
`--file`. It is conservative: the proof passes only when the report passed with
exit code `0`, the report BOM file, rendered BOM line/revision, and rendered
BOM digest match the target BOM, rendered `desiredStateDigest` and
`localRepoRevision` are present and valid digests, source and runtime preflight
were non-blocking, mutating apply was exercised, post-apply state is `Clean`,
post-revert state is `Clean` when `revertCheck: true`, and the expected
smoke/apply/revert acceptance stages are present, passing, and mark mutating
steps as mutating. Safe smoke reports that do not run a mutating apply still
generate useful evidence, but they produce `spec.passed: false` and should not
satisfy beta/stable promotion policy.

A minimal health proof looks like:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionHealthProof
metadata:
  name: default-platform-rev-008-health
spec:
  line: default-platform
  targetRevision: rev-008
  passed: true
  summary: beta cohort passed rollout health checks
  collectedAt: "2026-05-20T10:30:00Z"
  signals:
    - name: reconcile
      passed: true
      message: all canary targets reconciled
    - name: node-readiness
      passed: true
```

When `sealos sync promote` accepts the proof, the promoted channel writes the
target BOM path relative to the channel file when possible. Existing render,
validate, agent, and controller paths continue to consume the same channel file
through `--distribution-channel` or `distributionChannelPath`.

`sealos sync promote` also returns a `policyDecision` object in its structured
output. The decision records the evaluated transition, target channel rule,
health-proof requirement, and any warning or violation fields from the policy
engine. Failed decisions block before the channel file is written.

## Day 1 To Day N Behavior

Once Day 0 is complete, clusters should behave differently depending on whether
they are pinned or channel-following.

### Pinned Revision

If a cluster is pinned to one BOM revision:

- it does not move when a channel advances
- it changes only when the operator explicitly selects a new BOM revision

### Channel-Following Cluster

If a cluster follows a `DistributionChannel`:

- `sealos-agent` can re-resolve a local `DistributionChannel` file on each
  process-level reconcile pass
- `sealos-agent --controller` can also re-resolve it from a watched
  `DistributionTarget` object
- it moves only when the `DistributionChannel` target revision advances
- it should still persist the exact resolved BOM revision it last applied

This keeps operational intent and concrete state separate:

- intent: "follow `default-platform/stable`"
- concrete result: "currently on `rev-007`"

### Minimal Controller Target

The current controllerized path is intentionally small. It watches
`DistributionTarget` objects and maps each object to one existing agent
reconcile pass:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionTarget
metadata:
  name: default-platform
  namespace: sealos-system
spec:
  clusterName: default
  distributionChannelPath: /var/lib/sealos/distribution/default-platform-stable.yaml
  localRepoPath: /var/lib/sealos/distribution/local-repo
  kubeconfigPath: /host/etc/kubernetes/admin.conf
  hostRoot: /host
  requeueAfter: 1m
```

Run the agent in controller mode directly with:

```bash
sealos-agent --controller --controller-namespace sealos-system
```

Or install the CRD, RBAC, and deployment manifests from
[`deploy/distribution-controller/base`](../../deploy/distribution-controller/base).
See
[`../guides/controller-install.md`](../guides/controller-install.md)
for the in-cluster installation workflow and sample targets.

This mode currently supplies the watched API, status conditions, and installable
manifests, including a durable `DistributionRolloutPolicy` object for host
batch size, first-batch canary size, optional post-canary pause, optional
per-batch health gates, and stop-or-rollback failure behavior. A paused
or rolled-back controller target waits for an explicit target or policy update
before it re-enters apply. Registry-backed channel lookup, health-gated
promotion automation, and a package-level safety model for every multi-node
workflow are still outside the implemented surface.

## Applied Revision State

The current applied-state model records:

- BOM name
- BOM revision
- BOM channel

Rendered bundles also record render provenance, including the local
`DistributionChannel` path, digest, distribution line, BOM path, and BOM digest
when a channel file is used.

See [pkg/distribution/state/types.go](../../pkg/distribution/state/types.go).

That is useful even while the channel resolver is still local-file scoped,
because the cluster still needs a durable record of what exact baseline it last
materialized and which local selection document produced it.

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
  architecture. Treat it as a useful transition field until live channel lookup
  and promotion are modeled explicitly.

## What Still Needs To Be Designed Or Implemented

- The final API-backed `DistributionChannel` schema and storage contract
- Resolution rules from `distribution line + channel` to one BOM revision
  without requiring a local `spec.bomPath`
- API-backed channel advancement history and audit storage beyond the current
  local `spec.promotionHistory[]` field
- Health-proof ingestion or collection beyond the current local
  `DistributionHealthProof` file gate for `sealos sync promote`
- Whether `BOM.spec.channel` should become optional first and then be removed
  later
- The exact Day 0 operator interface for API-backed pinned versus
  channel-based targets

This guide does not require those pieces to be fully implemented first. It only
defines how they should fit together coherently.
