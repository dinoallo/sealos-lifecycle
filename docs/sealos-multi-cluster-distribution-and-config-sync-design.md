# Design Proposal: Sealos Multi-Cluster Distribution and Config Sync

## Status

Draft

## Summary

This document proposes a Sealos architecture for distributing system baselines and synchronizing cluster configuration across many clusters, especially in private-cloud and partially disconnected environments.

The core idea is to separate immutable global baselines from cluster-local differences, then let an in-cluster agent pull, hydrate, and apply the final desired state. The design aims to preserve offline autonomy, keep secrets local, and provide a controlled path for promoting validated local fixes into the upstream distribution baseline.

## Problem Statement

Operating Sealos across multiple clusters creates several conflicting requirements:

- Clusters may run in private networks where inbound connectivity is restricted or unavailable.
- Platform teams need a consistent distribution baseline for core infrastructure components.
- Individual clusters still need environment-specific overrides such as certificates, CIDRs, and hardware tuning.
- Tenant workloads may need to be synchronized across clusters while preserving cluster-local runtime state.
- Operators need a safe way to debug and patch live clusters without losing the ability to reconcile back to a known baseline.

Without a formal ownership model, these requirements lead to configuration drift, unclear responsibility boundaries, accidental secret leakage, and slow promotion of field fixes back into the standard distribution.

## Goals

- Define a clear ownership boundary between global distribution content and cluster-local overrides.
- Support pull-based synchronization in private or disconnected environments.
- Keep sensitive data, especially Secrets, inside the cluster boundary.
- Make distribution versions deterministic through immutable artifacts and version-locked BOMs.
- Support both Day 0 bootstrap and Day 1 to Day N lifecycle management.
- Allow operators to turn validated live fixes into either local patches or upstream changes.

## Non-Goals

- A centralized control plane that requires direct push access into every managed cluster.
- Full replacement of all existing app-level GitOps systems.
- Allowing unrestricted local mutation of globally owned core components without state tracking.
- Defining every CLI or CRD detail in this document.

## Design Principles

- Offline autonomy: clusters must continue to operate and reconcile from cached state when upstream systems are unavailable.
- Determinism first: all promoted baselines should be digest-pinned and reproducible.
- Local privacy: cluster-local secrets and environment-specific private data should not leave the cluster.
- Explicit ownership: the system must clearly distinguish platform-owned resources from cluster-owned or tenant-owned resources.
- Controlled flexibility: local forks are allowed, but they must be visible and auditable.

## Core Model: Four Configuration Quadrants

Sealos divides resources into four quadrants based on control boundary and scope. This model defines what belongs to the distribution layer and what belongs to the workload layer.

| Dimension | Global (global baseline / upstream artifacts) | Local (cluster-specific differences / private configuration) |
| --- | --- | --- |
| Infra (system distribution) | Standardized CNI, CSI, node agents, core control policies, and other platform baseline components. | Cloud provider drivers, private certificates, network CIDRs, hardware tuning, and other environment adaptation details. |
| User (multi-tenant workloads) | Shared workload templates, standard Deployments and Services, and policy defaults intended to be synchronized across clusters. | Cluster-local runtime state such as HPA values, tenant Secrets, local routing, and environment-specific overrides. |

This quadrant model is the basis for ownership, promotion rules, and reconciliation behavior.

## Requirements

### Functional Requirements

- The system must distribute immutable global baselines to many clusters.
- The system must allow each cluster to apply local patches without modifying the upstream base artifact.
- The agent must render the final desired state inside the cluster.
- Operators must be able to inspect drift, revert drift, and persist intentional drift.
- The design must support promotion of validated local improvements into the upstream baseline.

### Non-Functional Requirements

- No required inbound connection from the control side into managed clusters.
- Eventual convergence when connectivity is available.
- Safe degraded behavior when upstream registry or Git endpoints are temporarily unavailable.
- Minimal blast radius for bad releases through staged promotion.
- Auditable revision history for both global baselines and local patches.

## Architecture Overview

The design uses a decentralized pull-based architecture.

| Component | Responsibility |
| --- | --- |
| `Sealos Registry` | Stores immutable OCI artifacts for global baselines and related metadata. |
| `Local Repo` | Stores cluster-local patches, environment overrides, and cluster-scoped configuration history. |
| `Sealos Agent` | Pulls inputs, resolves the target revision, hydrates manifests, computes drift, applies changes, and reports status. |
| Promotion pipeline | Evaluates canary health signals and promotes revisions across release channels. |
| Health collection path | Receives low-level health proof from edge clusters and feeds promotion decisions. |

Connectivity is intentionally one-way from the cluster toward upstream systems. The cluster pulls what it needs and does not require an always-open inbound management channel.

## Artifact Model

The system separates immutable baseline content from mutable local overlays.

- `Base`: immutable OCI artifact published to `Sealos Registry`. It contains the global distribution baseline.
- `Patch`: cluster-local overlay stored in `Local Repo`. It may be expressed as parameter sets, structured patches, or rendered overlays.
- `BOM`: a bill of materials that pins the exact versions and digests that define a releasable distribution.
- `Hydrated desired state`: the rendered result of `Base + Patch`, produced inside the cluster by the agent.

This model allows the same baseline to be reused across clusters while keeping private or environment-specific details local.

## Cluster State Model

The agent tracks an explicit state for each cluster:

| State | Meaning | Expected Operator Action |
| --- | --- | --- |
| `Clean` | Live state matches the desired state derived from the approved baseline and local patch. | No action required. |
| `Dirty` | Manual or external changes have caused drift from the desired state. | Revert the drift or persist it intentionally. |
| `Orphan` | A locally modified resource has crossed the ownership boundary and changed a globally owned core component. | Review ownership, reconcile manually, and decide whether the change belongs upstream. |

The state machine clarifies SRE responsibility and prevents silent divergence from the supported distribution.

## Reconciliation Workflow

### Day 0 Bootstrap

1. Select the target release channel and BOM.
2. Pull the required base artifacts from `Sealos Registry`.
3. Load the cluster-local patch set from `Local Repo`.
4. Hydrate the final desired state inside the cluster.
5. Apply the rendered resources in dependency-aware order.

### Day 1 to Day N Continuous Reconcile

1. Poll or watch for a new target revision.
2. Pull the pinned base artifact and fetch the current local patch set.
3. Render `Base + Patch` into the final desired state.
4. Compare the rendered state with the live cluster state.
5. Apply the delta and record the applied revision.
6. Publish status and health signals.

### Operator Workflow

- `sealos diff`: show drift between live state and `Base + Patch`.
- `sealos revert`: discard manual drift and return to the rendered desired state.
- `sealos commit --target=local`: persist an intentional local change into `Local Repo`.
- `sealos commit --target=global`: prepare an upstream change request so the fix can become part of the next global baseline.

This creates a bidirectional GitOps loop: desired state flows downward, while validated field fixes can flow upward.

## Packaging and Distribution Strategy

For components that contain both reusable global logic and cluster-specific differences, Sealos uses a dual-track strategy.

- Global part: package reusable logic as immutable OCI artifacts and distribute it through `Sealos Registry`.
- Local part: keep private values and environment-specific patches in local Git or equivalent local storage.
- Local hydration: combine the two inside the cluster, similar to a Kustomize-style render phase.

The critical rule is that sensitive data remains local and the upstream-distributed artifact remains immutable.

## Distribution Evolution and Promotion

Sealos treats a distribution as an interface contract that evolves over time.

- Day 0 establishes the initial base plus overlay composition.
- Day 1 to Day N uses component-based upgrades and patching to evolve the running distribution.
- A local capability that proves stable in the field can be promoted into the global baseline.

Promotion should move through staged channels:

- `Alpha`: early validation in isolated clusters.
- `Beta`: broader canary rollout across heterogeneous environments.
- `Stable`: production-ready baseline approved for general use.

Promotion decisions should be driven by both explicit approval and measured health signals from canary clusters.

## Health Proof and Automated Promotion

The design assumes a feedback loop between edge clusters and the central promotion pipeline.

- Edge agents collect low-level health indicators, potentially including kernel anomalies, network retransmissions, and repeated reconcile failures.
- Health data is reported asynchronously so clusters do not depend on continuous connectivity.
- The promotion pipeline evaluates the collected signals before advancing a component or BOM to the next release channel.

This closes the loop between release distribution and runtime evidence.

## Failure Handling

| Failure Scenario | Expected Behavior |
| --- | --- |
| Registry temporarily unavailable | Continue running the last known good revision from local cache and retry later. |
| Local patch cannot be rendered | Block apply, mark the cluster degraded, and surface the render error clearly. |
| Apply succeeds only partially | Retry idempotently and do not mark the new revision healthy until reconciliation completes. |
| Live cluster is manually modified | Mark the cluster `Dirty` and require either `revert` or `commit`. |
| Local change modifies a global-owned component | Mark the cluster `Orphan` and prevent silent promotion. |

These failure cases should be made explicit in implementation and surfaced in operator tooling.

## Security and Trust Boundaries

- Clusters pull from upstream systems; they do not require inbound control-plane access.
- Secrets should remain in the cluster-local patch path and should not be uploaded to global storage.
- BOMs should pin exact versions and digests to prevent unintended upgrades.
- Promotion into a global baseline should require an auditable review path.

## Alternatives Considered

### Centralized Push Model

Rejected because many target environments are private or disconnected, and because direct push control creates a larger security and operability burden.

### Single Global Git Source for All Configuration

Rejected because it mixes cluster-local secrets and environment-specific configuration with reusable global baselines.

### Fully Local Per-Cluster Management

Rejected because it removes a shared baseline, makes promotion difficult, and increases long-term drift.

## Implementation Plan

Yes. This design is sufficient to draft an implementation plan, but the plan should explicitly separate decision-gating work from feature delivery. The current open questions do not block planning, but they do block parts of implementation.

For a repo-aligned execution breakdown with epics, milestones, and suggested package boundaries, see `docs/sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md`.

### Decision Gates

The following decisions should be finalized before broad implementation begins:

- Patch format: choose whether `Local Repo` uses Kustomize-style overlays, structured patches, Helm values, or a Sealos-specific format.
- Ownership rules: define which resources are global-owned, local-owned, and promotable.
- Revision model: define the exact metadata stored for BOMs, applied revisions, rollbacks, and audit trails.
- Promotion path: define the approval process for `commit --target=global`.

If these decisions remain open, implementation should be limited to a narrow infrastructure MVP.

### Workstream 0: Architecture Closure

Goal: reduce ambiguity before coding the main control loop.

Deliverables:

- A written decision record for patch format and hydration pipeline.
- A first-pass ownership matrix for core infra resources.
- A revision metadata schema for BOM, applied revision, and cluster status.
- Error-state definitions for `Clean`, `Dirty`, `Orphan`, and degraded reconcile states.

Verification:

- The design doc is updated so a contributor can implement the first agent workflow without inventing missing semantics.

### Workstream 1: Baseline Artifact and BOM Support

Goal: make the global baseline addressable, immutable, and reproducible.

Deliverables:

- OCI artifact layout for baseline packages.
- BOM schema with digest-pinned component versions.
- Registry pull logic for resolving a target release channel to a concrete BOM.
- Local cache behavior for offline or partially disconnected operation.

Verification:

- A cluster can resolve a BOM, download the required artifacts once, and reuse cached artifacts during a simulated registry outage.

### Workstream 2: Local Repo and Hydration Engine

Goal: render `Base + Patch` into a deterministic desired state inside the cluster.

Deliverables:

- Local repo layout for cluster-specific patches and secrets references.
- Hydration pipeline that combines the baseline artifact with the local patch set.
- Validation step that rejects malformed patches before apply.
- Stable manifest ordering so repeated renders are reproducible.

Verification:

- Identical input revisions produce identical rendered output.
- Secret-bearing local values remain cluster-local and are not published back to global storage.

### Workstream 3: Agent Reconcile Loop

Goal: implement the in-cluster control loop for pull, render, diff, apply, and status reporting.

Deliverables:

- Reconcile loop that fetches the target revision, hydrates manifests, diffs live state, and applies changes.
- Local state store for the last applied revision and health status.
- Retry and backoff behavior for transient registry, Git, or apply failures.
- Status reporting for revision, state, and reconcile errors.

Verification:

- A cluster converges from an empty or outdated state to the desired revision.
- Transient upstream failures do not destroy the last known good state.
- Partial apply failures are retried idempotently.

### Workstream 4: Drift Management and Operator CLI

Goal: make live-state debugging explicit and recoverable.

Deliverables:

- `sealos diff` to compare live state with the rendered desired state.
- `sealos revert` to discard drift and return to baseline.
- `sealos commit --target=local` to persist intentional local changes.
- State transitions for `Clean`, `Dirty`, and `Orphan`.

Verification:

- Manual modification of a managed resource is detected reliably.
- Revert returns the cluster to the last rendered desired state.
- A locally committed patch changes future renders without mutating the base artifact.

### Workstream 5: Promotion and Release Channels

Goal: turn validated local or canary changes into controlled upstream distribution updates.

Deliverables:

- Release channel model for `Alpha`, `Beta`, and `Stable`.
- Promotion metadata that links a candidate revision to health evidence.
- Workflow for `sealos commit --target=global` to generate an upstream review artifact or PR payload.
- Policy checks that prevent silent promotion from `Orphan` state.

Verification:

- A candidate revision can move from `Alpha` to `Beta` to `Stable` with audit history.
- Global promotion requires explicit review and cannot bypass ownership rules.

### Workstream 6: Health Proof and Automated Promotion

Goal: use runtime evidence to automate confidence building for releases.

Deliverables:

- Health signal collection contract for edge agents.
- Aggregation pipeline for canary-cluster health reports.
- Promotion rules based on stability windows and failure thresholds.
- Safeguards that halt or roll back promotion when health degrades.

Verification:

- Simulated unhealthy canary signals prevent promotion.
- Healthy canary signals over a defined window allow promotion to the next channel.

### Suggested Delivery Milestones

#### Milestone 1: Infrastructure MVP

Scope:

- BOM resolution
- Registry pull and cache
- Local repo loading
- Hydration engine
- Agent reconcile loop for infra components only

Exit criteria:

- One cluster can bootstrap from `Base + Patch`.
- The cluster can recover the desired state after restart or temporary registry unavailability.

#### Milestone 2: Manageable Drift

Scope:

- `diff`
- `revert`
- `commit --target=local`
- `Clean` / `Dirty` / `Orphan` state transitions

Exit criteria:

- Operators can debug a live cluster without losing the ability to reconcile back to a supported state.

#### Milestone 3: Controlled Promotion

Scope:

- Release channels
- Promotion metadata
- `commit --target=global`
- Ownership enforcement

Exit criteria:

- A field fix can move from local validation into an upstream-reviewed baseline.

#### Milestone 4: Automated Confidence Loop

Scope:

- Health proof ingestion
- Canary evaluation
- Policy-driven promotion gates

Exit criteria:

- Promotion can be blocked or advanced based on measured health signals instead of manual judgment alone.

### Team and Ownership Split

The work naturally separates into parallel streams:

- Artifact and BOM team: baseline packaging, registry contracts, revision metadata.
- Agent team: reconcile loop, state handling, retries, status reporting.
- CLI and operator workflow team: `diff`, `revert`, and `commit`.
- Release engineering team: channels, promotion logic, review integration.
- Observability team: health proof schema, signal ingestion, promotion metrics.

This split allows implementation to proceed in parallel after the decision gates are closed.

### Recommended First Slice

The best first slice is a narrow infrastructure-only MVP:

1. Pick one patch format.
2. Define the BOM schema.
3. Implement local hydration for infra components only.
4. Build the agent reconcile loop with cached pull behavior.
5. Add `diff` before adding promotion or automated health logic.

This slice validates the hardest architectural assumptions early without committing to the full multi-tenant or promotion system.

### Risks to Track During Implementation

- Choosing a patch format that is hard to validate or hard to promote upstream.
- Blurring ownership boundaries and making `Orphan` detection unreliable.
- Letting apply semantics depend on unstable render ordering.
- Overloading the first milestone with tenant workload features too early.
- Designing health signals before the basic revision and reconcile model is stable.

## Success Metrics

The implementation should eventually define measurable targets for:

- Reconcile latency after a new baseline is published.
- Rate of clusters converging to the intended revision.
- Number of incidents caused by secret leakage outside the cluster boundary.
- Time required to promote a validated local fix into the global baseline.
- Rate of clusters entering `Dirty` or `Orphan` state after upgrades.

## Open Questions

- What exact patch representation should `Local Repo` use: Kustomize overlays, structured patches, Helm values, or a Sealos-specific format?
- Which core components are strictly global-owned and cannot be locally overridden without entering `Orphan` state?
- What review and authorization workflow is required for `commit --target=global`?
- How should health signals be normalized across heterogeneous hardware and network environments?
- What minimal metadata must be stored with each applied revision for audit and rollback?

## Conclusion

This design gives Sealos a clear model for multi-cluster distribution and configuration synchronization: immutable global baselines, cluster-local overlays, in-cluster hydration, explicit drift states, and promotion driven by field validation. It is intentionally biased toward deterministic operations, private-cloud compatibility, and a controlled path from local experimentation to standardized distribution.
