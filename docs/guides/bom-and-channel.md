# Guide: BOMs, Revisions, and ReleaseChannel Semantics

## Status

Design guide with implementation notes

## Summary

This guide explains how Sealos should think about:

- `ComponentPackage` revisions
- `BOM` revisions
- `distribution lines`
- `ReleaseChannel` objects
- Day 0 and Day 1 revision selection

It exists because these concepts currently appear across several design
documents, while the current PoC code still uses a simpler transition model in
which `spec.channel` lives inside the BOM itself. The repository now also
supports a narrow local-file `ReleaseChannel` path for selecting a BOM
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
- Current local `ReleaseChannel` resolver:
  [pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go)

## Why This Guide Exists

The current design needs one place that answers these questions plainly:

- What is a BOM?
- What is a BOM revision?
- What is the difference between a BOM and a distribution line?
- What should a cluster choose at Day 0?
- What is `ReleaseChannel`, and why should it exist separately from the
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
| `ReleaseChannel` | One mutable release object that says which BOM revision is currently recommended for one channel on one distribution line. | Mutable |
| `AppliedRevision` | Cluster-local state that records what the cluster last rendered or applied. | Mutable cluster state |

The most important rule is:

- packages are component building blocks
- a BOM revision is one concrete release snapshot
- a distribution line is the sequence of those snapshots over time
- a `ReleaseChannel` is the moving head that points clusters at the
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
- `spec.packages[]`

### Recommended Semantics

The cleanest reading of those fields is:

- `metadata.name`
  The BOM family or line-facing name. In practice this should normally stay
  stable across revisions on the same distribution line.
- `spec.revision`
  The immutable snapshot identifier for this exact BOM revision.
- `spec.packages[]`
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
  packages:
    - name: containerd
      category: infra
      version: v1.7.18
      artifact:
        name: containerd-runtime
        image: registry.example/platform/containerd-runtime:v1.7.18
        digest: sha256:<digest>
    - name: kubernetes
      category: infra
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
- `spec.packages` is required
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

## Package Cache, Retention, And Offline Operation

When `sync render`, `sync validate`, or the agent consumes BOM package artifacts
without a local `--package-source` override, Sealos pulls each OCI package into
the cluster runtime package cache:

```text
<runtime-root>/clusters/<cluster>/etc/distribution/package-cache/
  sha256/<digest>/
  ref/<sanitized-reference>/
```

Digest-pinned BOM references use the `sha256/<digest>/` path. This is the
preferred shape for production because the cache key is immutable and the
rendered bundle records the resolved BOM and package digests.

Use the cache commands as the operational entry point:

```bash
sealos sync package cache list --cluster my-cluster
sealos sync package cache gc --cluster my-cluster --max-age 168h --dry-run
sealos sync package cache gc --cluster my-cluster --max-age 720h --include-valid
```

`cache list` reports valid and invalid package cache entries, component
metadata, entry sizes, and cache paths. `cache gc` always allows invalid entries
to be removed when they are older than `--max-age`; valid package entries are
kept by default and are only removed when `--include-valid` is set. This default
protects the last known good package set used by rollback and re-render
workflows.

Registry outage and offline mirror rules:

- Keep BOM package references digest-pinned. Do not rely on mutable tags for
  recovery.
- Warm the package cache before planned offline windows by rendering or
  validating the target BOM while the registry is reachable.
- Keep the last successful rendered bundle and package cache until a newer
  revision has been applied and validated.
- During a registry outage, prefer rendering from the warmed cache or from
  explicit `--package-source` package directories instead of changing the BOM.
- For offline mirrors, push the same package digests to the mirror registry and
  update the BOM image host only after verifying the mirrored digest matches the
  original digest.
- Run `cache gc --dry-run` first; use `--include-valid` only after confirming
  the removed entries are no longer referenced by active BOM revisions,
  rollback targets, or derived lines.

## About `baseArtifacts`

The BOM schema also includes `spec.baseArtifacts`, but the current PoC and the
current walkthroughs are centered on `spec.packages`.

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
- move the mutable channel head into `ReleaseChannel`

## What `ReleaseChannel` Should Mean

`ReleaseChannel` is the release object that answers one question:

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
kind: ReleaseChannel
metadata:
  name: default-platform-stable
spec:
  distribution: default-platform
  channel: stable
  targetRevision: rev-007
  bomPath: bom.yaml
  bomDigest: sha256:<bom-digest>
```

That shape keeps responsibilities clean:

- the BOM defines one immutable snapshot
- the `ReleaseChannel` tells clusters which snapshot to follow for one
  rollout stage

## Current Implementation vs Target Model

| Topic | Current Repo Behavior | Target Design Direction |
| --- | --- | --- |
| How a cluster chooses a target | Explicit BOM file path, a local `ReleaseChannel` file passed with `--release-channel`, or a release metadata source plus `distribution line + channel` | Explicit BOM revision, or `distribution line + ReleaseChannel` lookup |
| Where channel metadata lives | `BOM.spec.channel`, plus local or release-source channel selection metadata in render provenance | `ReleaseChannel` object |
| What `sync render` resolves today | A BOM document passed directly, a local `ReleaseChannel` whose `spec.bomPath` points at the BOM to load, or a `ReleaseChannel` resolved from `--release-source --release-line --channel` | One resolved BOM revision after optional channel lookup |
| What applied state records | BOM name, revision, digest, `requestedTarget`, and `resolvedTarget`; rendered bundles also record BOM and `ReleaseChannel` provenance | Same contract, with release history and promotion evidence stored by the release service |

This distinction is important because the current code path in
[pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go)
validates that the channel distribution matches the target BOM
`metadata.name`, that `targetRevision` matches the BOM `spec.revision`, and
then renders that concrete BOM. Local `ReleaseChannel` files remain supported.
The release lookup path also resolves `distribution line + channel` from a
release metadata source and requires `spec.bomDigest` so the resolved BOM is
digest-pinned before render. A local release directory can be exposed through
the read-only HTTP lookup API with:

```bash
sealos sync release-metadata serve \
  --release-source /var/lib/sealos/distribution/releases \
  --listen 127.0.0.1:8080
```

That service answers `GET /v1/distributions/{line}/channels/{channel}` with a
`ReleaseChannel` document and `GET /v1/distributions/{line}/revisions/{revision}/bom`
with the selected BOM document. It also accepts a health-gated promotion request
at `POST /v1/distributions/{line}/channels/{channel}/promotions`; the request
names a `targetRevision`, supplies a passed `DistributionHealthProof`, and then
uses the same promotion policy as `sealos sync promote` before advancing the
channel file.

### Applied State Target Contract

Every newly rendered `AppliedRevision` records the operator's request and the
concrete BOM it resolved to:

```yaml
spec:
  bom:
    name: default-platform
    revision: rev-007
    channel: stable
    digest: sha256:<resolved-bom-digest>
  requestedTarget:
    kind: releaseChannelLookup
    releaseSource: https://release.sealos.example
    distributionLine: default-platform
    channel: stable
    releaseChannelPath: https://release.sealos.example/v1/distributions/default-platform/channels/stable
  resolvedTarget:
    bom:
      name: default-platform
      revision: rev-007
      channel: stable
      digest: sha256:<resolved-bom-digest>
    releaseChannel:
      distributionLine: default-platform
      channel: stable
      targetRevision: rev-007
      source: https://release.sealos.example/v1/distributions/default-platform/channels/stable
```

`requestedTarget.kind` is one of:

- `bom` for an explicit BOM file target
- `releaseChannelFile` for a local `ReleaseChannel` file
- `releaseChannelLookup` for registry/API-backed lookup by
  `distribution line + channel`

`requestedTarget` and `resolvedTarget` are written together. Older state files
without these fields remain loadable, but new render/apply paths persist both
fields, and `status.lastSuccessfulRevision` keeps the same target metadata
after a successful apply.

Successful applies also maintain a bounded newest-first
`status.successfulRevisions` history. Rollback uses the last successful
revision snapshot, not the failed desired target, so a failed upgrade can roll
back across BOM names, distribution lines, channels, and local revision
metadata as long as the retained revision bundle is still present in the
cluster runtime store.

The same policy is available locally through `sealos sync promote` and through
the release metadata service promotion endpoint. Both paths advance one
`ReleaseChannel` file to a target BOM after checking target-channel policy,
requiring health proof for beta/stable targets, and recording an approver,
reason, timestamp, proof digest, component digests, validation cohort,
candidate record, and promotion history entry.

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

Today, the current repo implements three target paths:

- choose a specific BOM file and pass it to `sealos sync render --file`
- choose a local `ReleaseChannel` file and pass it to
  `sealos sync render --release-channel`
- choose a release metadata source, distribution line, and channel and pass
  them to `sealos sync render --release-source --release-line --channel`

The local `ReleaseChannel` must name the distribution line, channel,
target revision, and `spec.bomPath` for the target BOM. The CLI resolves the
channel to that local BOM before materialization.

For HTTP(S) release sources, Sealos requests
`/v1/distributions/{line}/channels/{channel}` and expects a `ReleaseChannel`
document. The `ReleaseChannel` must include `spec.bomDigest`; lookup fails if
the fetched BOM digest does not match.

## Local Channel Promotion

For the current local-file model, promotion means updating one
`ReleaseChannel` document so that `spec.targetRevision` and `spec.bomPath`
point at a different BOM revision on the same distribution line.

Use:

```bash
sealos sync promote \
  --release-channel channels/default-platform-stable.yaml \
  --target-bom boms/default-platform/rev-008.yaml \
  --health-proof proofs/default-platform-rev-008-health.yaml \
  --reason "beta cohort passed source preflight and rollout validation" \
  --approved-by release-team
```

The command validates that:

- the channel document is a valid `ReleaseChannel`
- the target BOM is a valid BOM
- `ReleaseChannel.spec.distribution` matches `BOM.metadata.name`
- the default promotion policy allows the target channel to advance to the
  candidate BOM's source channel
- if the target channel requires proof, `--health-proof` points to a valid
  `DistributionHealthProof` that targets the same line and BOM revision,
  reports `spec.passed: true`, includes health signals, and satisfies its
  required-signal and minimum-passed-signal thresholds

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

The generated proof also normalizes evidence for channel promotion. Every
blocking signal carries `required: true`, `source: PackageAcceptanceReport`,
and an `evidenceRef` that points at the acceptance report field or stage that
fed the signal. `spec.thresholds.requiredSignals` lists the signals that must
be present and pass, `spec.thresholds.minPassedSignals` records the minimum
passing-signal threshold, and `spec.signalSummary` stores the evaluated counts.
Older proof files without thresholds remain strict: all signals must pass.

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
  thresholds:
    requiredSignals:
      - reconcile
      - node-readiness
    minPassedSignals: 2
  signalSummary:
    totalSignals: 2
    passedSignals: 2
    failedSignals: 0
    requiredSignals: 2
    passedRequiredSignals: 2
    minPassedSignals: 2
  signals:
    - name: reconcile
      passed: true
      required: true
      source: PackageAcceptanceReport
      evidenceRef: spec.stages[name=reconcile]
      message: all canary targets reconciled
    - name: node-readiness
      passed: true
      required: true
      source: PackageAcceptanceReport
      evidenceRef: spec.stages[name=node-readiness]
```

When `sealos sync promote` accepts the proof, the promoted channel writes the
target BOM path relative to the channel file when possible. Existing render,
validate, agent, and controller paths continue to consume the same channel file
through `--release-channel` or `releaseChannelPath`.

The same promotion also persists release-source audit records:

- `candidates/<line>/<revision>/candidate.yaml` records the BOM digest,
  component artifact digests, replaced revision, source/target channels,
  optional validation cohort, evidence references, and timeline.
- `promotions/<line>/<channel>/<timestamp>-<revision>.yaml` records the
  approved promotion, policy decision, candidate reference, approver, evidence,
  and timeline.

`sealos sync promote` also returns a `policyDecision` object in its structured
output. The decision records the evaluated transition, target channel rule,
health-proof requirement, required/missing/failed health signals, minimum
passing-signal threshold, and any warning or violation fields from the policy
engine. Failed decisions block before the channel file is written.

## Day 1 To Day N Behavior

Once Day 0 is complete, clusters should behave differently depending on whether
they are pinned or channel-following.

### Pinned Revision

If a cluster is pinned to one BOM revision:

- it does not move when a channel advances
- it changes only when the operator explicitly selects a new BOM revision

### Channel-Following Cluster

If a cluster follows a `ReleaseChannel`:

- `sealos-agent` can re-resolve a local `ReleaseChannel` file on each
  process-level reconcile pass
- `sealos-agent --controller` can also re-resolve it from a watched
  `DistributionTarget` object
- it moves only when the `ReleaseChannel` target revision advances
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
  releaseChannelPath: /var/lib/sealos/distribution/default-platform-stable.yaml
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
before it re-enters apply. `sync plan` also reports package and phase safety
profiles for rootfs, host-file, manifest, chart, patch, values, package hook
phases, local patch approval, and generated host projections so operators can
distinguish host-wave rollout steps from cluster-wide barriers. Health-gated
promotion automation is implemented through `sync release-metadata serve` and
`sealos sync promote`; durable per-package rollout cursors are still outside
the implemented surface.

## Applied Revision State

The current applied-state model records:

- BOM name
- BOM revision
- BOM channel

Rendered bundles also record render provenance, including the local
`ReleaseChannel` path, digest, distribution line, BOM path, and BOM digest
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
- optionally create separate `ReleaseChannel` objects for that line

That is why a derived distribution is best understood as a new distribution
line, not as "whatever the cluster currently drifted into."

## Practical Rules Of Thumb

- If you want one exact reproducible baseline, point at one BOM revision.
- If you want controlled rollout, follow a `ReleaseChannel`.
- If you need long-lived divergence from the upstream baseline, fork the
  distribution line and publish new BOM revisions there.
- Do not treat `spec.channel` inside today's BOM schema as the final release
  architecture. Treat it as a useful transition field until live channel lookup
  and promotion are modeled explicitly.

## What Still Needs To Be Designed Or Implemented

- The final API-backed `ReleaseChannel` schema and storage contract
- Health-proof ingestion or collection beyond the current local
  `DistributionHealthProof` file gate for `sealos sync promote`
- Whether `BOM.spec.channel` should become optional first and then be removed
  later
- The exact Day 0 operator interface for API-backed pinned versus
  channel-based targets

This guide does not require those pieces to be fully implemented first. It only
defines how they should fit together coherently.
