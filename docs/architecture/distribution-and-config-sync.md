# Design Proposal: Sealos Multi-Cluster Distribution and Config Sync

## Status

Draft

## Summary

This document defines the top-level architecture for distributing Sealos system
baselines across many clusters while preserving cluster-local adaptation.

The core idea is simple: publish immutable global baselines, keep private and
environment-specific data local to each cluster, and let an in-cluster agent
pull, hydrate, and reconcile the final desired state. Detailed reconcile
semantics and release-promotion rules are split into focused sub-designs so
this document can stay at the architecture level.

## Related Documents

- For the OCI-backed component artifact contract assumed by this design, see
  [Package format](../architecture/package-format.md).
- For reconcile behavior, ownership rules, drift states, and failure semantics,
  see
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md).
- For release channels, health proof, and promotion policy, see
  [Release and promotion](../architecture/release-and-promotion.md).
- For the object model around BOM revisions, distribution lines, and
  `ReleaseChannel`, see
  [BOM and channel](../guides/bom-and-channel.md).
- For repo-scoped epics, milestones, package boundaries, and testing order, see
  [Distribution implementation plan](../plans/distribution-implementation-plan.md).

## Working Terminology

This design uses `distribution` in two related but different senses. The
documents should distinguish them explicitly so revision tracking, ownership,
and forking behavior stay precise.

| Term | Meaning | Why It Matters |
| --- | --- | --- |
| `Component package revision` | One immutable OCI package revision referenced by digest. | This is the component-level building block. |
| `BOM revision` | One digest-pinned set of component package revisions. | This is the concrete releasable baseline snapshot. |
| `Distribution snapshot` | The full platform baseline represented by one BOM revision. | This is what a cluster actually targets at one point in time. |
| `Distribution line` | A named release lineage made of successive BOM revisions, plus the channel and promotion metadata that govern how clusters follow it. | This is what operators stay on, advance along, or fork away from. |
| `ReleaseChannel` | A mutable release object that declares which BOM revision is current for one release channel on one distribution line. | This is how clusters can follow a line indirectly rather than pinning one revision manually. |

The practical reading rule is:

- when this design says "publish a new distribution version", it usually means
  publish a new `BOM revision`
- when it says "stay on", "advance", or "fork" a distribution, it means a
  `distribution line`
- local binding does not create a new distribution; it customizes one cluster's
  rendered state from the same `distribution snapshot`

## Problem Statement

Operating Sealos across multiple clusters creates several conflicting
requirements:

- Clusters may run in private networks where inbound connectivity is restricted
  or unavailable.
- Platform teams need a consistent distribution baseline for core
  infrastructure components.
- Individual clusters still need environment-specific overrides such as
  certificates, CIDRs, registry policy, and hardware tuning.
- Operators need a safe way to debug and patch live clusters without losing the
  ability to reconcile back to a known baseline.
- Validated field fixes should be promotable into the shared distribution
  instead of remaining permanent local forks.

Without a formal ownership model and revision model, these requirements turn
into drift, unclear responsibility boundaries, accidental secret leakage, and
slow promotion of fixes back into the supported baseline.

## Goals

- Define a clear ownership boundary between global distribution content and
  cluster-local overrides.
- Support pull-based synchronization in private or disconnected environments.
- Keep sensitive data, especially Secrets, inside the cluster boundary.
- Make distribution versions deterministic through immutable artifacts and
  version-locked BOMs.
- Support both Day 0 bootstrap and Day 1 to Day N lifecycle management.
- Allow operators to turn validated live fixes into either local patches or
  upstream changes.

## Non-Goals

- A centralized control plane that requires direct push access into every
  managed cluster.
- Full replacement of all existing app-level GitOps systems.
- Allowing unrestricted local mutation of globally owned core components
  without state tracking.
- Defining every CLI, CRD, or repo package detail in this document.

## Design Principles

- Offline autonomy: clusters must continue to operate and reconcile from cached
  state when upstream systems are unavailable.
- Determinism first: promoted baselines must be digest-pinned and reproducible.
- Local privacy: cluster-local secrets and environment-specific private data
  must not leave the cluster by default.
- Explicit ownership: the system must distinguish platform-owned resources from
  cluster-owned or tenant-owned resources.
- Controlled flexibility: local forks are allowed, but they must be visible,
  reviewable, and promotable.

## Core Model: Four Configuration Quadrants

Sealos divides managed configuration by scope and ownership boundary. This
quadrant model defines what belongs to the distribution layer and what must
stay cluster-local.

| Dimension | Global (shared baseline / upstream artifacts) | Local (cluster-specific differences / private configuration) |
| --- | --- | --- |
| Infra (system distribution) | Standardized CNI, CSI, node agents, core control policies, and other platform baseline components. | Cloud provider drivers, private certificates, network CIDRs, registry mirrors, and hardware tuning. |
| User (multi-tenant workloads) | Shared workload templates, standard Deployments and Services, and policy defaults intended to be synchronized across clusters. | Cluster-local runtime state such as HPA values, tenant Secrets, local routing, and environment-specific overrides. |

This quadrant model is the foundation for ownership rules, reconcile behavior,
and promotion policy.

## Architecture Overview

The design uses a decentralized pull-based architecture.

| Component | Responsibility |
| --- | --- |
| `Sealos Registry` | Stores immutable OCI component artifacts and related release metadata. |
| `Local Repo` | Stores cluster-local patches, private configuration, and cluster-scoped revision history. |
| `Sealos Agent` | Pulls inputs, resolves the target revision, hydrates desired state, computes drift, applies changes, and reports status. |
| Release metadata and promotion path | Advances validated revisions across channels and records why a revision became eligible for broader rollout. |
| Health collection path | Carries low-level health proof from clusters back into the release decision process. |

Connectivity is intentionally one-way from the cluster toward upstream systems.
The cluster pulls what it needs and does not require an always-open inbound
management channel.

## Artifact And Revision Model

The system separates immutable shared content from mutable local adaptation:

- `Component package`: an immutable OCI artifact that contains one reusable
  Sealos component revision.
- `BOM revision`: a bill of materials that pins the exact package digests that
  define one releasable baseline snapshot.
- `Distribution line`: a named lineage of BOM revisions that gives operators a
  durable release identity rather than only one isolated snapshot.
- `Local Repo`: cluster-local overlays, private values, and environment
  adaptation inputs.
- `Hydrated desired state`: the rendered result of `BOM-selected base +
  cluster-local overlay`, produced inside the cluster.
- `Applied revision record`: metadata that states which baseline and local
  inputs were last applied successfully.

This model lets many clusters share the same baseline artifacts while keeping
private or environment-specific details local.

## System Flow Overview

At a high level, the system works as follows:

1. Platform teams publish immutable component packages and assemble them into a
   digest-pinned BOM.
2. A cluster selects a target baseline revision, directly or through a release
   channel.
3. The in-cluster agent pulls the required artifacts and combines them with the
   cluster's local repo content.
4. The agent hydrates the final desired state inside the cluster and
   reconciles live state toward it.
5. Drift state, revision state, and health evidence feed operator workflows and
   release-promotion decisions.

This design intentionally places hydration inside the cluster so that private
inputs do not need to be pushed upstream.

## Supporting Design Areas

This architecture depends on three detailed design areas that should not be
mixed back into the top-level document:

| Area | Why It Exists | Document |
| --- | --- | --- |
| Reconcile, ownership, and drift | Defines how the agent assembles desired state, classifies live changes, and handles failures. | [Reconcile and ownership](../architecture/reconcile-and-ownership.md) |
| Materialization tracking and drift detection | Defines how rendered files, Kubernetes objects, and generated outputs are identified and compared against live state. | [Materialization and drift](../architecture/materialization-and-drift.md) |
| Release channels, health proof, and promotion | Defines how validated changes move from local or canary use into shared baselines. | [Release and promotion](../architecture/release-and-promotion.md) |
| Repo execution plan | Defines command layout, Go package boundaries, milestones, and verification order for this repository. | [Distribution implementation plan](../plans/distribution-implementation-plan.md) |

Keeping these concerns separate makes it easier to review architecture intent
without reading implementation sequencing or policy detail at the same time.

## Architecture Boundaries

The architecture relies on several non-negotiable boundaries:

- Shared baseline artifacts are immutable and must be referenced by exact
  digests.
- Secrets remain in the cluster-local path and are not promoted automatically.
- A cluster may continue from cached state when upstream systems are
  unavailable, but it must not invent an untracked desired state.
- Ownership must be explicit enough that the system can distinguish acceptable
  local adaptation from unsupported mutation of global baseline content.

## Alternatives Considered

### Centralized Push Model

Rejected because many target environments are private or disconnected, and
because direct push control creates a larger security and operability burden.

### Single Global Git Source For All Configuration

Rejected because it mixes cluster-local secrets and environment-specific
configuration with reusable global baselines.

### Fully Local Per-Cluster Management

Rejected because it removes a shared baseline, makes promotion difficult, and
increases long-term drift.

## Open Questions

- What exact patch representation should the local repo MVP use: Kustomize
  overlays, structured patches, Helm values, or a Sealos-specific format?
- What minimum metadata must be stored in the applied revision record for audit,
  rollback reasoning, and drift analysis?
- Which infrastructure resource classes are globally owned, locally owned, or
  promotable by policy in the first release?

## Conclusion

This design gives Sealos a clear multi-cluster architecture: immutable shared
baselines, cluster-local overlays, in-cluster hydration, explicit ownership,
and a controlled path from local validation to broader distribution. The split
sub-designs keep that architecture understandable without losing the details
needed for reconcile and promotion work.
