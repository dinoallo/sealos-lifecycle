# Execution Plan: Sealos Multi-Cluster Distribution and Config Sync

## Status

Draft

## Source

This plan is derived from `docs/sealos-multi-cluster-distribution-and-config-sync-design.md` and translates the design into repo-scoped implementation work.

## Planning Assumptions

- The design document remains the source of truth for architecture intent.
- The patch format, ownership rules, revision metadata, and promotion workflow are still decision gates.
- Early implementation should target infrastructure distribution first and defer broad tenant workload support.
- The first useful outcome is not full automation. It is a deterministic, recoverable infrastructure MVP.

## Repo Fit

The current repository is CLI-heavy and centered on one-shot lifecycle operations. The implementation should respect that structure rather than forcing the long-running agent into the existing `pkg/apply` pipeline.

### Existing Packages To Reuse

| Path | Current Role | Recommended Use In This Plan |
| --- | --- | --- |
| `cmd/sealos/cmd` | Main user CLI | Add user-facing sync commands under a new parent command. |
| `pkg/apply` | Imperative, one-shot cluster/image apply path | Reuse only narrowly scoped rendering or apply helpers if they prove generic. Do not place the agent loop here. |
| `pkg/clusterfile` | Clusterfile parsing and preprocessing | Keep as declarative input loading for existing workflows. Do not overload it as the local repo abstraction. |
| `pkg/bootstrap` | Host bootstrap context and orchestration primitives | Reuse for Day 0 or low-level infra actions when needed. |
| `pkg/runtime` | Kubernetes and k3s runtime operations | Reuse through explicit adapters when hydrated desired state requires runtime actions. |
| `pkg/client-go/kubernetes` | Kubernetes client wrappers | Reuse for live-state reads, discovery, diff, and apply support. |
| `pkg/filesystem/registry` | Registry sync integration at the filesystem layer | Reuse for artifact movement and local cache flows where applicable. |
| `pkg/sreg/registry/sync` | Registry copy and sync primitives | Reuse for OCI artifact fetch, mirror, and offline cache operations. |
| `pkg/utils/retry` | Retry utilities | Reuse in reconcile and artifact fetch loops. |
| `test/e2e` | End-to-end coverage | Add reconcile, drift, and promotion scenarios here once the MVP stabilizes. |

### New Package Boundaries

The cleanest implementation path is a new `pkg/distribution` tree for the long-running sync model:

| Proposed Path | Responsibility |
| --- | --- |
| `pkg/distribution/bom` | BOM types, validation, revision resolution, and release-channel selection. |
| `pkg/distribution/cache` | Local cache layout and cache lifecycle for pulled base artifacts. |
| `pkg/distribution/localrepo` | Cluster-local patch loading, validation, and revision tracking. |
| `pkg/distribution/hydrate` | Deterministic render of `Base + Patch` into a final desired-state bundle. |
| `pkg/distribution/ownership` | Ownership rules for global-owned versus local-owned resources. |
| `pkg/distribution/state` | Applied revision metadata, cluster state (`Clean`, `Dirty`, `Orphan`), and degraded conditions. |
| `pkg/distribution/reconcile` | Pull, hydrate, diff, apply, retry, and status update loop. |
| `pkg/distribution/promotion` | Release channels, candidate revisions, promotion policies, and upstream handoff. |
| `pkg/distribution/health` | Health proof schema, collection contracts, and promotion inputs. |

If the team wants fewer packages initially, `ownership`, `state`, and `promotion` can begin as subpackages with small public surfaces and be merged later if they do not justify separate boundaries.

### Command Layout

The conceptual design used `sealos diff`, `sealos revert`, and `sealos commit`, but the repo already exposes a top-level `diff` command through the buildah command set. To avoid collisions, the repo-specific CLI should introduce a new parent command:

```text
sealos sync diff
sealos sync revert
sealos sync commit
sealos sync status
```

Recommended command placement:

- `cmd/sealos/cmd/sync.go`
- `cmd/sealos/cmd/sync_diff.go`
- `cmd/sealos/cmd/sync_revert.go`
- `cmd/sealos/cmd/sync_commit.go`
- `cmd/sealos/cmd/sync_status.go`

For the long-running in-cluster component, the cleanest end state is a dedicated binary:

- `cmd/sealos-agent/main.go`

Do not place the agent under `cmd/sealctl`; `sealctl` is currently a helper-style binary, while the agent is a long-running control-loop process.

## Epics

### Epic 0: Close Design Gates

Goal: remove the remaining semantic ambiguity before broad coding begins.

Primary packages:

- `docs/`
- `pkg/distribution/bom`
- `pkg/distribution/ownership`
- `pkg/distribution/state`

Tasks:

- Choose one patch format for the local repo MVP.
- Define the initial BOM schema and revision identity format.
- Publish a first-pass ownership matrix for global and local resources.
- Define the persisted cluster state model, including degraded states.
- Decide whether the agent rollout target is a new binary immediately or a staged experimental command first.

Exit criteria:

- The implementation team can create package skeletons without inventing data model semantics.

Suggested verification:

- Review-only milestone with doc approval and package skeleton compilation.

Initial task breakdown:

- Define shared API version and kind constants under `pkg/distribution`.
- Draft the BOM schema and validation rules under `pkg/distribution/bom`.
- Define the ownership rule shape under `pkg/distribution/ownership`.
- Draft the applied-revision schema and state validation rules under `pkg/distribution/state`.
- Keep promotion policy decisions documented, but do not block schema code on those later subsystems.
- Use package-local tests to lock down validation semantics before agent work begins.

### Epic 1: Baseline Artifact And BOM Resolution

Goal: make the global baseline reproducible, addressable, and cacheable.

Primary packages:

- `pkg/distribution/bom`
- `pkg/distribution/cache`
- `pkg/sreg/registry/sync`
- `pkg/filesystem/registry`

Tasks:

- Implement BOM Go types and schema validation.
- Define how a release channel resolves to a concrete BOM revision.
- Implement OCI artifact fetch and local cache population.
- Define local cache invalidation and retention behavior.
- Add digest verification and failure surfaces for corrupted cache entries.

Dependencies:

- Epic 0 patch and revision decisions.

Exit criteria:

- A node can resolve a BOM, pull all required immutable artifacts, and rehydrate from cache during a simulated registry outage.

Suggested verification:

- Unit tests for BOM parsing and validation.
- Integration test for pull-once and reuse-from-cache behavior.

### Epic 2: Local Repo And Hydration

Goal: render a deterministic desired state inside the cluster from immutable base artifacts plus local patch content.

Primary packages:

- `pkg/distribution/localrepo`
- `pkg/distribution/hydrate`
- `pkg/distribution/ownership`

Tasks:

- Define the on-disk layout for the local repo MVP.
- Implement local patch loading and validation.
- Implement deterministic hydration from `Base + Patch`.
- Add ownership checks during render so illegal overrides are caught before apply.
- Produce a render artifact that can be hashed and diffed repeatably.

Dependencies:

- Epic 0 patch and ownership decisions.
- Epic 1 BOM and cache support.

Exit criteria:

- The same base revision and local patch revision always produce the same rendered desired state.

Suggested verification:

- Golden-file tests for hydration.
- Negative tests for invalid patch shapes and forbidden ownership overrides.

### Epic 3: Agent Runtime And Reconcile Loop

Goal: create the in-cluster control loop that converges live state toward the hydrated desired state.

Primary packages:

- `cmd/sealos-agent`
- `pkg/distribution/reconcile`
- `pkg/distribution/state`
- `pkg/client-go/kubernetes`
- `pkg/utils/retry`

Tasks:

- Create the agent binary entrypoint and process configuration.
- Implement reconcile phases: resolve, pull, hydrate, diff, apply, persist state.
- Persist the last applied revision and last known health state.
- Implement retry, backoff, and degraded-state transitions.
- Add structured status output that can be consumed by the CLI and future promotion logic.

Dependencies:

- Epic 1 and Epic 2.

Exit criteria:

- A cluster converges to the desired revision and survives temporary upstream failures without losing the last known good state.

Suggested verification:

- Unit tests for state transitions.
- Integration tests for retry and partial-failure handling.
- Single-cluster e2e smoke test for empty-to-converged bootstrap.

### Epic 4: Operator Workflows And Drift Management

Goal: make live-state debugging visible, safe, and recoverable.

Primary packages:

- `cmd/sealos/cmd`
- `pkg/distribution/reconcile`
- `pkg/distribution/state`
- `pkg/client-go/kubernetes`

Tasks:

- Add the `sealos sync` parent command.
- Implement `sealos sync diff`.
- Implement `sealos sync revert`.
- Implement `sealos sync commit` for local persistence.
- Implement `sealos sync status` for revision and drift visibility.
- Detect and surface `Clean`, `Dirty`, and `Orphan` transitions in CLI output.

Dependencies:

- Epic 2 and Epic 3.

Exit criteria:

- An operator can inspect drift, revert drift, or persist intentional local drift without mutating the base artifact.

Suggested verification:

- CLI-focused unit tests.
- Integration tests that manually perturb live state and verify resulting transitions.

### Epic 5: Promotion Flow And Release Channels

Goal: support controlled promotion from candidate revisions to shared baselines.

Primary packages:

- `pkg/distribution/promotion`
- `pkg/distribution/bom`
- `pkg/distribution/state`

Tasks:

- Define channel metadata for `Alpha`, `Beta`, and `Stable`.
- Implement candidate revision metadata and history.
- Implement the upstream handoff path for `sealos sync commit --target=global`.
- Prevent silent promotion of `Orphan` changes.
- Record enough audit metadata to reconstruct why a promotion occurred.

Dependencies:

- Epic 0 revision and promotion decisions.
- Epic 3 and Epic 4.

Exit criteria:

- A validated change can move through release channels with explicit audit history and ownership enforcement.

Suggested verification:

- Unit tests for channel and policy transitions.
- Integration tests for local-to-global promotion handoff.

### Epic 6: Health Proof And Automated Promotion

Goal: turn runtime evidence into promotion confidence.

Primary packages:

- `pkg/distribution/health`
- `pkg/distribution/promotion`
- `pkg/distribution/state`

Tasks:

- Define the edge-agent health report contract.
- Implement health report ingestion or collection adapters.
- Define promotion policies based on health windows and failure thresholds.
- Implement promotion blockers for unhealthy canary signals.
- Define the minimum rollback or halt behavior on post-promotion degradation.

Dependencies:

- Epic 5.

Exit criteria:

- Promotion can be automatically blocked or advanced based on collected health evidence.

Suggested verification:

- Policy tests with synthetic healthy and unhealthy signals.
- Multi-cluster canary simulation in e2e once the earlier epics stabilize.

## Milestones

### Milestone 0: Design Locked

Scope:

- Epic 0 only

Deliverable:

- Approved data contracts and package skeletons.

### Milestone 1: Infrastructure MVP

Scope:

- Epic 1
- Epic 2
- Epic 3, limited to infra baseline reconcile

Deliverable:

- A single cluster can pull a pinned baseline, hydrate local patch data, and converge from empty or stale state.

### Milestone 2: Safe Operator Control

Scope:

- Epic 4

Deliverable:

- Operators can inspect, revert, and persist local drift through `sealos sync`.

### Milestone 3: Controlled Promotion

Scope:

- Epic 5

Deliverable:

- Validated local changes can move into a shared baseline through explicit review and release channels.

### Milestone 4: Automated Confidence Loop

Scope:

- Epic 6

Deliverable:

- Promotion decisions can incorporate health evidence rather than relying only on manual review.

## Suggested Package Tree

```text
cmd/
  sealos/
    cmd/
      sync.go
      sync_diff.go
      sync_revert.go
      sync_commit.go
      sync_status.go
  sealos-agent/
    main.go

pkg/
  distribution/
    bom/
    cache/
    localrepo/
    hydrate/
    ownership/
    state/
    reconcile/
    promotion/
    health/
```

This tree keeps the new control-loop model isolated from the existing imperative apply path while still allowing selective reuse of existing helpers.

## Testing Strategy

### Unit Tests

- `pkg/distribution/bom`: schema validation, channel resolution, digest pinning.
- `pkg/distribution/localrepo`: patch loading, validation, error cases.
- `pkg/distribution/hydrate`: deterministic rendering and ownership checks.
- `pkg/distribution/state`: state transitions and revision bookkeeping.
- `pkg/distribution/promotion`: policy and channel transitions.

### Integration Tests

- Cache behavior during registry outage.
- Reconcile idempotency after partial apply failure.
- Drift detection after manual resource changes.
- Local commit persistence and re-render behavior.

### End-To-End Tests

- Single-cluster infrastructure bootstrap from `Base + Patch`.
- Cluster drift, revert, and local commit flow.
- Canary promotion flow after release channels exist.

## Recommended Build Order

1. Lock the data model and CLI names.
2. Implement BOM resolution and local cache.
3. Implement local repo loading and hydration.
4. Implement the agent reconcile loop.
5. Add `sealos sync diff` and `sealos sync status`.
6. Add `sealos sync revert` and local commit support.
7. Add release-channel promotion.
8. Add health-based automation.

This order proves the hardest technical assumptions first and postpones automation until the core state model is stable.

## Risks And Mitigations

| Risk | Why It Matters | Mitigation |
| --- | --- | --- |
| Patch format is too flexible | Hard to validate, diff, and promote | Start with the narrowest format that supports infrastructure MVP needs. |
| Agent logic leaks into `pkg/apply` | Blurs one-shot install flow with long-running reconcile flow | Keep new sync logic under `pkg/distribution` and reuse only explicit helpers. |
| CLI naming collides with existing commands | Breaks root command behavior and user expectations | Use `sealos sync ...` rather than new top-level `diff`. |
| Ownership rules are underspecified | `Orphan` detection becomes unreliable | Ship an initial ownership matrix before reconcile code is merged. |
| Health automation arrives too early | Adds policy complexity before the baseline state model is proven | Gate Epic 6 behind stable Milestone 3 behavior. |

## Immediate Next Actions

- Approve the repo-level command shape: `sealos sync ...` plus `cmd/sealos-agent`.
- Choose the patch format for the MVP.
- Define the initial BOM and applied-revision schemas.
- Create package skeletons under `pkg/distribution`.
- Start Epic 1 and Epic 2 in parallel once the schemas are locked.
