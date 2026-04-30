# Sub-Design: Sealos Multi-Cluster Reconcile, Drift, and Ownership

## Status

Draft

## Summary

This document defines how a Sealos cluster assembles desired state from shared
baseline artifacts plus cluster-local inputs, how the agent reconciles that
state, and how ownership rules classify live changes as acceptable local
adaptation, drift, or unsupported mutation.

## Related Documents

- For the top-level architecture and scope, see
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md).
- For the OCI component package contract consumed during hydration, see
  [sealos-component-package-format-design.md](./sealos-component-package-format-design.md).
- For release channels, health proof, and promotion rules, see
  [sealos-multi-cluster-release-and-promotion-design.md](./sealos-multi-cluster-release-and-promotion-design.md).
- For repo-scoped package boundaries and implementation order, see
  [sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md](./sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md).

## Why This Sub-Design Exists

The top-level architecture says that Sealos uses immutable global baselines and
cluster-local overlays. That is not enough to implement a control loop.

The agent still needs exact answers to these questions:

- What is the source of truth for each class of resource?
- When does a live change count as normal local adaptation versus unsupported
  mutation?
- How does the agent behave when fetch, render, validation, or apply fails?
- What state should operators see after manual drift or partial convergence?

This document answers those questions without mixing them into release policy
or repo implementation sequencing.

## Scope

### In Scope

- Desired-state assembly from BOM-selected base artifacts and cluster-local
  inputs
- Ownership categories for shared versus cluster-local resources
- Day 0 bootstrap and Day 1 to Day N continuous reconcile behavior
- Drift classification and cluster state transitions
- Failure semantics for fetch, render, validation, and apply
- Security rules around local data handling during reconcile

### Out Of Scope

- OCI component package layout details
- Release channel semantics and promotion policy
- Final CLI shape, Go package layout, and implementation milestones

## Ownership Model

Ownership is the rule that determines which system is allowed to define a given
piece of desired state.

### Ownership Categories

- `Global-owned`: the desired state comes from shared package artifacts selected
  by a BOM. A cluster may consume this content, but it must not silently fork
  it.
- `Local-owned`: the desired state comes from cluster-local inputs such as
  private values, environment-specific overlays, or runtime adaptation data.
- `Promotable local fix`: an intentional local change that may later be
  upstreamed after validation. This is not a separate source of truth. It is a
  workflow that starts from either drift or a local patch and ends in a reviewed
  global baseline change.

### Package-Level Boundary

For one component package, the ownership boundary should be read in two layers:

- `global package contract`: the immutable artifact and the contract it
  declares
- `local package binding`: the cluster-specific data bound into that contract at
  hydration time

| Package-Level Element | Scope | Expected Handling |
| --- | --- | --- |
| Package identity, version, class, compatibility, dependencies | `global` | Must come from the selected artifact digest and cannot be cluster-edited silently. |
| Packaged `rootfs`, manifests, charts, files, values defaults, and hook scripts | `global` | These bytes are shared baseline content. A cluster may consume them, not fork them implicitly. |
| Declared input surfaces in `spec.inputs` | `global` | The package decides what may vary. |
| Concrete input payloads bound during hydration | `local` | Supplied by the target cluster and expected to vary by environment. |
| Local overlays or patches applied after artifact selection | `local`, but policy-checked | Only valid when they target allowed local adaptation surfaces. |
| Live runtime observations and secret material | `local` | Must remain cluster-bound. |

This leads to one important rule:

- a package is not partly local just because it has inputs
- the package remains `global`; only the values bound through declared
  extension points are `local`

For example, a `kubernetes-rootfs` package may declare `podCIDR` or registry
override inputs. That does not make the kubeadm baseline, bootstrap logic, or
hook scripts local. It only means the package exposes a controlled local binding
surface.

### Initial Ownership Matrix

| Resource Class | Examples | Default Source Of Truth | Local Mutation Handling |
| --- | --- | --- | --- |
| Global infrastructure baseline | CNI manifests, CSI controllers, node agents, core admission policy, platform DaemonSets | BOM-selected package artifacts | Local live mutation is treated as drift and may become `Orphan` if it changes global-owned intent. |
| Local infrastructure adaptation | CIDRs, certificates, registry mirrors, cloud-specific config, hardware tuning | Local Repo | May vary per cluster and may be persisted locally. |
| Global user-facing templates | Shared workload templates, policy defaults, standard service layouts | Shared baseline artifacts or centrally curated templates | Local override is allowed only through declared extension points. |
| Local runtime state | Secrets, HPA values, local routing, cluster-specific tenant overrides | Cluster-local systems and Local Repo | Never promoted automatically and should remain cluster-bound. |

The first implementation does not need a perfect ownership matrix for every
resource type, but it does need an explicit first-pass matrix for all
infrastructure components included in the MVP.

### Package-Level Decision Rule

When deciding whether something belongs in package content or in local repo
data, use this rule order:

1. If the value should be identical across many clusters for the same BOM
   revision, make it `global`.
2. If the value is expected to differ per cluster and is known before hydrate,
   make it a declared `local` input.
3. If the change is a reusable platform opinion shared by a subset of clusters,
   model it as shared package content or as a separate patch package, not as
   ad-hoc local repo data.
4. If the value is secret or runtime-discovered, keep it `local` and outside
   shared artifacts.

Examples:

- `global`: CNI manifests, kubelet systemd units, bootstrap hook logic, default
  admission policy, healthcheck jobs
- `local`: cluster name, API endpoint, CIDRs, mirror endpoints, private certs,
  kubelet extra env, environment-specific values files
- `shared but not local`: audit policy, common API server hardening flags,
  enterprise admission defaults, reusable static-pod patches

## Desired-State Assembly

The desired state for one cluster is assembled inside that cluster.

### Inputs

- A BOM-selected set of immutable component package revisions
- Cluster-local repo content, including patch data and private values
- Agent policy and ownership rules
- Live-state discovery needed for diff and apply planning

### Assembly Rules

1. Resolve the target BOM revision or channel-selected baseline.
2. Pull or reuse the required immutable component artifacts from cache.
3. Load the cluster-local patch and input set from the local repo.
4. Hydrate `Base + Local Repo` into one deterministic desired-state bundle.
5. Validate ownership rules before any apply is attempted.
6. Persist enough revision metadata to compare future reconciles against the
   last successful apply.

Secrets and environment-specific private values remain in the local path. They
may participate in hydration, but they are not copied into shared baseline
artifacts.

Local repo data must not be treated as a blanket override layer. It is only
allowed to bind declared local inputs or modify resource surfaces that the
ownership policy has explicitly marked as local-owned.

## Reconcile Phases

The agent should follow a stable phase model so failures and metrics are easy
to reason about.

### Phase 1: Resolve Target Revision

- Determine the intended BOM revision or release-channel target.
- Verify that referenced artifacts are digest-pinned.
- Refuse ambiguous or partially resolved targets.

### Phase 2: Materialize Inputs

- Pull missing base artifacts or reuse valid cached copies.
- Load local repo content and validate that required inputs exist.
- Record fetch or load failures before moving to hydration.

### Phase 3: Hydrate Desired State

- Render immutable baseline artifacts plus local inputs into one deterministic
  bundle.
- Produce stable ordering so repeated renders of the same inputs are identical.
- Reject malformed overlays or unresolved input references.

### Phase 4: Validate Ownership

- Ensure local overlays only modify resources they are allowed to affect.
- Detect attempted mutation of global-owned intent before apply.
- Surface policy violations as ownership failures rather than generic render
  failures.

### Phase 5: Diff And Apply

- Compare hydrated desired state with live cluster state.
- Apply the delta in a dependency-aware order.
- Treat partial success as an incomplete revision, not a healthy converge.

### Phase 6: Persist State And Report Status

- Record the last fully applied revision and its input identities.
- Publish drift state, reconcile result, and degraded conditions.
- Export enough status for operator workflows and release-promotion decisions.

## Day 0 Bootstrap

Day 0 bootstrap uses the same ownership model as steady-state reconcile, but it
starts from an empty or partially prepared cluster.

1. Select the initial BOM revision.
2. Pull the required base artifacts.
3. Load cluster-local adaptation inputs.
4. Hydrate the full desired state.
5. Validate ownership constraints.
6. Apply the bootstrap bundle in dependency-aware order.
7. Persist the applied revision only after the bootstrap reaches a healthy
   stopping point.

## Day 1 To Day N Continuous Reconcile

Continuous reconcile repeats the same control loop with revision awareness.

1. Detect a new target baseline revision or local repo revision.
2. Materialize any missing inputs from cache or upstream.
3. Rehydrate the desired state.
4. Compare the desired state with live state and the last applied revision.
5. Apply the delta.
6. Update revision status, drift state, and health reporting.

The control loop must be idempotent. Re-running reconcile against unchanged
inputs should converge to no-op behavior.

## Cluster State Model

The cluster state model must separate ownership/drift classification from
temporary execution health.

### Primary Drift States

| State | Meaning | Expected Operator Interpretation |
| --- | --- | --- |
| `Clean` | Live state matches the desired state derived from the approved baseline and local inputs. | No action required. |
| `Dirty` | Live state differs from the desired state, but the change has not crossed a forbidden ownership boundary. | Review, revert, or persist the local change intentionally. |
| `Orphan` | A live or local change has modified global-owned intent without going through the approved promotion path. | Treat as unsupported divergence until reviewed. |

### Degraded Condition

`Degraded` should be modeled as a reconcile condition rather than a replacement
for `Clean`, `Dirty`, or `Orphan`.

Examples:

- a cluster may be `Clean` but `Degraded` because the registry is unavailable
  for future updates while the last good revision is still running
- a cluster may be `Dirty` and `Degraded` because a render error blocks
  reconciliation
- a cluster may be `Orphan` without being otherwise unhealthy if the unsupported
  mutation is not currently breaking workloads

This separation avoids overloading one state field with both ownership and
health semantics.

## Drift Handling

The agent should classify changes by both source and ownership impact:

- If live state changes but still falls within local-owned policy, mark the
  cluster `Dirty`.
- If a local patch is intentionally accepted and the desired state is updated,
  the cluster should return to `Clean` after successful reconcile.
- If a local or live change alters global-owned intent directly, mark the
  cluster `Orphan`.
- If a reconcile cannot complete because inputs are invalid or apply only
  partially succeeds, keep the previous applied revision and add `Degraded`
  status.

This model gives operators a controlled path from observation to correction
without normalizing unsupported divergence.

## Failure Semantics

| Failure Scenario | Expected Behavior |
| --- | --- |
| Registry temporarily unavailable | Continue from the last known good cached base artifact set, retry later, and mark reconcile as degraded if a newer target cannot be fetched. |
| Local repo cannot be loaded | Block reconcile for the new target, keep the last good applied revision, and surface a local-input error. |
| Hydration fails | Do not attempt apply; keep the previous applied revision and mark reconcile degraded. |
| Ownership validation fails | Do not apply the candidate desired state; classify the attempted change as an ownership violation and require review. |
| Apply succeeds only partially | Retry idempotently and do not mark the new revision healthy until convergence completes. |
| Live cluster is manually modified | Mark the cluster `Dirty` and require revert or intentional local persistence. |
| Local change modifies global-owned intent | Mark the cluster `Orphan` and block silent promotion. |

## Operator Capabilities

The system should expose these capabilities, whether through CLI, CRD, or other
operator-facing tooling:

- inspect the current target revision, last applied revision, and drift state
- compare live state with the hydrated desired state
- discard unapproved drift and return to the last supported desired state
- persist intentional local changes when they are within local ownership
- prepare a reviewed promotion path when a validated local fix should become
  part of the shared baseline

This document describes the behavior that those capabilities must support, not
the final command syntax.

## Security And Data Boundary

- Clusters pull from upstream systems; they do not require inbound management
  access.
- Secret-bearing values remain in the local repo or another cluster-local
  secret store.
- Hydration may consume private values, but promotion payloads must not publish
  those values back into shared baseline artifacts.
- Applied revision records should contain references and hashes, not raw secret
  material.

## Open Questions

- What exact patch representation should the local repo MVP use?
- How should ownership policy be expressed: by package class, resource kind,
  namespace, path, or an explicit rule DSL?
- What is the minimum applied-revision schema needed to support drift analysis,
  retry safety, and future rollback reasoning?
