# Sub-Design: Sealos Multi-Cluster Release Channels, Health Proof, and Promotion

## Status

Draft

## Summary

This document defines how Sealos turns validated component or BOM revisions
into shared multi-cluster baselines through release channels, health evidence,
and explicit promotion rules.

## Related Documents

- For the top-level architecture and ownership boundary, see
  [Distribution and config sync](../architecture/distribution-and-config-sync.md).
- For reconcile behavior, drift states, and ownership violations, see
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md).
- For the OCI component artifact contract consumed by BOMs, see
  [Package format](../architecture/package-format.md).
- For the concrete object model around BOM revisions and `ReleaseChannel`,
  see
  [BOM and channel](../guides/bom-and-channel.md).
- For repo-scoped implementation sequencing, see
  [Distribution implementation plan](../plans/distribution-implementation-plan.md).

## Why This Sub-Design Exists

Baseline distribution and baseline promotion are related, but they are not the
same problem.

The reconcile design answers how one cluster converges to a target revision.
This document answers how a revision becomes eligible to be that target for
more clusters.

That separation matters because promotion policy involves release channels,
candidate history, approval workflow, and health evidence that should not blur
the control-loop design.

## Scope

### In Scope

- Release-channel semantics for shared baselines
- Candidate revision tracking for BOMs or component sets
- Health-proof inputs used for promotion decisions
- Explicit guardrails for moving local or canary-validated changes upstream
- Halt and rollback expectations when promotion evidence degrades

### Out Of Scope

- Low-level reconcile mechanics inside a single cluster
- OCI package layout and hydration semantics
- Final repo command layout and milestone sequencing

## Release Objects

Promotion should operate on explicit revision objects rather than on mutable
tags or ad-hoc cluster state.

| Object | Meaning |
| --- | --- |
| `Component revision` | One immutable OCI package revision referenced by digest. |
| `BOM revision` | A digest-pinned set of component revisions that defines a releasable baseline. |
| `ReleaseChannel` | A mutable release object that declares which BOM revision is current for one release channel on one distribution line. |
| `Candidate revision` | A BOM or component change under evaluation for broader rollout. |
| `Health proof` | Auditable runtime evidence collected from clusters that exercised a candidate revision. |

Promotion policy should operate primarily at the BOM level, even if some
evidence originates from individual component changes.

In the top-level terminology, one `BOM revision` is one concrete
`distribution snapshot`, while a named sequence of BOM revisions plus channel
metadata forms a `distribution line`. Promotion therefore advances a
distribution line by updating its `ReleaseChannel` objects to point at
newer BOM revisions.

## Channel Model

Sealos should use staged release channels with increasing rollout confidence.

| Channel | Intent | Expected Use |
| --- | --- | --- |
| `Alpha` | Early validation with high operator attention and limited blast radius | Isolated internal clusters, feature bring-up, and early field trials |
| `Beta` | Broader canary validation across more realistic environments | Heterogeneous test fleets and controlled customer pilots |
| `Stable` | General-use baseline approved for production rollout | Default production target for supported clusters |

Channels are not just labels. They imply different evidence thresholds and
approval expectations.

The current code boundary models these expectations in
`pkg/distribution/promotion`. The default local-file policy treats a BOM's
transitional `spec.channel` as the candidate source channel and evaluates it
against the mutable `ReleaseChannel.spec.channel` target:

| Target channel | Allowed candidate source channels | Health proof |
| --- | --- | --- |
| `Alpha` | `Alpha` | Optional |
| `Beta` | `Alpha`, `Beta` | Required |
| `Stable` | `Beta`, `Stable` | Required |

This is not a registry-backed release service. It is the first deterministic
policy layer used by `sealos sync promote` before a local channel file is
advanced.

## Promotion Flow

Promotion should follow an explicit, auditable path:

1. Create a candidate revision from a new BOM or a reviewed component update.
2. Attach enough metadata to explain what changed and why the candidate exists.
3. Roll the candidate through `Alpha` clusters first.
4. Collect health proof and operational feedback for a defined validation
   window.
5. Advance the candidate to `Beta` only if review and evidence both pass.
6. Promote to `Stable` only after broader canary validation succeeds.

This flow applies both to planned baseline releases and to validated local fixes
that are being upstreamed.

## Promotion Inputs

Promotion should consider more than human approval alone.

### Required Inputs

- Candidate revision metadata
- Ownership validation that proves the candidate is promotable
- Health proof from clusters that exercised the candidate
- Operator review and approval metadata
- Audit trail that links channel advancement to evidence

### Candidate Metadata

At minimum, a candidate should record:

- the exact component or BOM digests involved
- the source revision or channel it is replacing
- the reason for promotion
- the clusters or cohorts used for validation
- the approval decision and timestamp

## Health Proof Model

Health proof is the runtime evidence used to decide whether a candidate is safe
to advance.

### Principles

- Asynchronous: clusters must be able to report health without requiring a
  permanently connected control plane.
- Comparable: signals should be normalizable enough to compare cohorts and time
  windows.
- Auditable: promotion history should show which evidence supported a decision.
- Conservative: absence of proof should not be treated as proof of health.

### Example Signal Categories

- reconcile success or repeated reconcile failure
- node readiness and control-plane health
- workload restart loops after the candidate is applied
- network or storage health signals tied to the changed component
- upgrade-specific smoke checks defined for the candidate cohort

Health proof does not need to start as a full observability platform. The MVP
can begin with a narrow, structured contract if it is stable and auditable.

## Promotion Guardrails

Promotion must be constrained by explicit policy:

- only digest-pinned revisions are promotable
- no silent promotion is allowed from `Orphan` state
- promotion must exclude cluster-local secret material
- a candidate must not skip directly from unreviewed local change to `Stable`
- channel advancement should record who approved it and which evidence was used

These guardrails are what prevent local experimentation from becoming an
untracked global fork.

For the local-file implementation, `sealos sync promote` enforces the channel
source and proof requirements before writing `ReleaseChannel`. The command
returns the promotion policy decision in structured output so automation can
inspect the selected rule, evaluated transition, and proof requirement.

## Halt And Rollback Expectations

Promotion needs a failure model, not just a happy path.

| Scenario | Expected Behavior |
| --- | --- |
| Alpha validation exposes repeated failures | Stop advancement and keep the candidate out of broader channels. |
| Beta rollout shows environment-specific regressions | Halt promotion, preserve evidence, and require review before retrying. |
| Stable candidate degrades after promotion | Freeze further advancement and prepare a rollback or replacement candidate. |
| Health evidence is missing or inconsistent | Treat the candidate as unproven rather than healthy. |

The exact rollback mechanism can evolve later, but the policy expectation must
be explicit from the start.

## Relationship To Local Fix Promotion

A validated field fix should have a path into the shared baseline, but not by
copying cluster-local data upstream blindly.

The expected flow is:

1. detect or intentionally create a local change
2. classify whether the change is locally owned or a candidate for upstream
   promotion
3. normalize the change into a reviewable candidate revision
4. validate it through channel-based rollout
5. promote the reviewed candidate into the shared baseline if evidence supports
   it

This keeps the architecture flexible without weakening ownership boundaries.

## Open Questions

- How should health signals be normalized across heterogeneous hardware and
  network environments?
- What minimum evidence threshold should each channel require before promotion?
- Where should candidate and promotion metadata live in the first
  implementation: registry metadata, Git, BOM annotations, or a dedicated
  status store?
