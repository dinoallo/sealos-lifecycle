# Guide: Local Patch Policy Authoring And Review

## Status

Guide for the current single-node MVP

## Summary

This guide defines who authors `LocalPatchPolicy`, what kinds of changes are
considered safe or risky, and how those changes should be reviewed before they
become part of a rendered bundle.

It builds on the current source-and-scope design:

- policy scope is `clusterLocal`
- supported sources are `localRepo`, `bom`, `package`, and `builtInDefault`
- the rendered bundle carries the effective policy artifact and its provenance

This guide answers the next operational question:

- who is allowed to change that policy
- what reviewers should look for
- what minimum validation should happen before accepting a policy change

## Related Documents

- Local patch policy source and scope:
  [Local patch policy](../architecture/local-patch-policy.md)
- Local repo layout and secret handling:
  [Local repo and secret](../guides/local-repo-and-secret.md)
- Tracking and drift model:
  [Materialization and drift](../architecture/materialization-and-drift.md)
- Operator action reference:
  [Sync operator actions](../reference/sync-operator-actions.md)

## Current Authoring Boundary

In the current MVP, `LocalPatchPolicy` can be authored in these places:

- `local-repo/policy/local-patch-policy.yaml`
- a BOM-selected policy file referenced by `BOM.spec.localPatchPolicy`
- a component-package policy file referenced by
  `ComponentPackage.spec.localPatchPolicy`

That means:

- cluster operators may author or adjust cluster-local policy in the local repo
- BOM authors may select a reviewed cluster-local policy for a rendered
  revision
- package authors may ship a reviewed cluster-local policy in a package
  artifact, but only one selected package may do so unless the BOM or local repo
  chooses the effective policy
- no source may currently define a package/BOM-scoped policy

This is a direct consequence of the current design:

- package/BOM define shared baseline
- local patch policy defines cluster-local override budget

## What A Policy Change Means

Every policy change falls into one of three categories.

### 1. Narrowing Change

Examples:

- remove one `allowedPrefix`
- remove one supported kind
- add a newly forbidden path

Operational effect:

- some local patches that used to validate may now be rejected
- existing cluster-local drift may become non-committable

Review stance:

- safe from a shared-baseline perspective
- risky from an operator continuity perspective

### 2. Widening Change

Examples:

- add one new `allowedPrefix`
- add one new supported kind
- remove a previously forbidden field

Operational effect:

- Sealos now permits more local override surface than before
- more drift paths may become `policyEligible`
- more `Orphan` drift may become promotable to `localPatch`

Review stance:

- the highest-risk class in the current model
- must be justified explicitly

### 3. Refactoring Change

Examples:

- reordering rules
- adding comments
- rewriting YAML without changing effective meaning

Operational effect:

- no intended policy behavior change

Review stance:

- should be kept separate from widening/narrowing changes whenever possible

## Review Questions

Every policy review should answer these questions.

### Scope And Ownership

1. Does the change still fit `spec.scope: clusterLocal`?
2. Is the proposal widening cluster-local override surface, or only narrowing
   it?
3. Is the requested field truly cluster-local, or is it trying to smuggle a
   shared baseline decision into local policy?

### Behavioral Intent

4. What operator use case requires this path to be allowed?
5. If the same path is needed across many clusters, should this really be a
   package/BOM baseline improvement instead?
6. Would this change let operators override a field that changes workload
   identity, rollout semantics, or security posture too broadly?

### Existing Drift And Compatibility

7. Could this break existing local patches already stored under
   `local-repo/patches/**`?
8. Could it change how current `sync diff` classifies drift, for example by
   turning previously non-eligible object paths into `policyEligible` ones?
9. Does the change affect `sync commit` eligibility in ways operators would not
   expect?

## Review Checklist

Use this shorter checklist when you need a yes/no review pass rather than the
full narrative above.

| Check | Accept When | Reject When |
| --- | --- | --- |
| Scope | The change still fits `spec.scope: clusterLocal`. | The proposal really wants package/BOM-scoped behavior. |
| Ownership boundary | The path is truly cluster-local. | The path is really a shared baseline decision in disguise. |
| Change type | The review explicitly says whether the change widens, narrows, or only refactors policy. | The change silently widens local override surface. |
| Operator use case | One concrete cluster-local use case is stated. | The reason is only “operators may want this someday.” |
| Safety boundary | Forbidden areas stay forbidden: image, selector, status, server-managed metadata. | The policy starts allowing identity, rollout, or control-plane fields too broadly. |
| Existing local patches | Existing `local-repo/patches/**` compatibility was checked. | The change may strand existing local patches without acknowledgement. |
| Drift semantics | Review explains whether `policyEligible`, `promoteToLocalPatch`, or `sync commit` eligibility changes. | Drift-classification side effects are ignored. |
| Rendered provenance | The rendered bundle still records matching source, scope, name, path, and digest. | Bundle-carried policy provenance is missing or inconsistent. |
| Positive validation | At least one valid patch example is checked. | No positive example is validated. |
| Negative validation | At least one invalid patch example is still rejected. | No negative example is validated. |

One practical rule follows:

- if a widening change cannot name one concrete cluster-local use case and one
  positive/negative validation pair, it is not ready to accept

## Current Hard Review Rules

In the current MVP, reviewers should reject policy changes that try to allow:

- workload container image mutation
- selector mutation
- server-managed metadata
- status updates
- any path that is really a shared baseline decision rather than a
  cluster-local override

Those constraints should remain true even if the YAML shape looks convenient.

## Required Change Description

Any non-trivial policy change should come with a short structured explanation:

1. Why this path needs to be local.
2. Whether the change widens or narrows the policy.
3. Which component or object kind is affected.
4. What example local patch is expected to validate after the change.
5. What example invalid patch should still be rejected after the change.

This explanation can live in a PR description, design note, or commit body. It
does not need a new schema field.

For repo-local review convenience, normal pull requests continue to use
[.github/PULL_REQUEST_TEMPLATE.md](../../.github/PULL_REQUEST_TEMPLATE.md), while
`LocalPatchPolicy`-focused pull requests can use the dedicated template at
[.github/PULL_REQUEST_TEMPLATE/local-patch-policy.md](../../.github/PULL_REQUEST_TEMPLATE/local-patch-policy.md).

## Minimum Validation

Before accepting a policy change, the minimum expected validation is:

1. Validate the policy file itself through existing tests or code paths.
2. Render a bundle that carries the policy and verify the rendered provenance:
   - `localPatchPolicySource`
   - `localPatchPolicyScope`
   - `localPatchPolicyName`
   - `localPatchPolicyPath`
   - `localPatchPolicyDigest`
3. Check at least one expected positive case.
4. Check at least one expected negative case.

In repo terms today, that usually means some combination of:

- the stable repo-local entrypoint `make verify-local-patch-policy`
- targeted `go test` under `pkg/distribution/ownership` when iterating on schema
  or fixture failures
- targeted `go test` under `pkg/distribution/hydrate` when provenance handling is
  the only area being debugged
- `sync diff` / `sync status` fixture verification when operator-facing output
  is affected

The same stable entrypoint is what the current lightweight CI lane runs in
[.github/workflows/local_patch_policy_gate.yml](../../.github/workflows/local_patch_policy_gate.yml).

For repo-local dry runs of that same CI-style gate, use:

- `make verify-local-patch-policy-gate OLD_POLICY=... NEW_POLICY=... LOCAL_REPO=...`

At the current repo state, that gate now covers two additional automatic
checks:

- policy impact analysis for widening vs narrowing changes in allowed and
  forbidden surface
- compatibility checking for existing `local-repo/patches/**` content against
  the proposed policy

The current workflow also writes a structured job-summary report that carries:

- the compared old/new policy identity
- whether widening and/or narrowing changes were detected
- the detailed impact diff
- the list of incompatible existing local patches, if any

It now also writes a separate approval follow-up section that makes it obvious:

- whether an approval file was provided
- whether that approval was actually consumed to pass the gate
- who owns the exception and who approved it
- when the approval expires
- whether the current follow-up is to renew or remove the approval

That report is now generated through the CLI entrypoint:

- `sealos sync policy-report --old-policy ... --new-policy ... --local-repo ...`

The helper script remains a repo-local utility, but it is no longer the primary
automation path in CI.

The gate itself now uses the stricter CLI entrypoint:

- `sealos sync policy-gate --old-policy ... --new-policy ... --local-repo ...`

The current CLI also exposes approval-expiry governance directly through:

- `--approval-expiry-warning-days`
- `--fail-when-approval-expires-soon`

The workflow now resolves the base policy artifact through the repo-local helper
`go run ./scripts/local-patch-policy-base ...`, instead of repeating that git
lookup logic inline in shell.

If one of those blocking conditions is intentionally accepted, the current
auditable exception path is an approval file placed alongside the cluster-local
policy source, typically:

- `local-repo/policy/local-patch-policy-approval.yaml`

That approval file is now bound to both the compared old policy and the
candidate new policy through:

- `name`
- `scope`
- `digest`

It must also carry lifecycle metadata that keeps the exception auditable and
time-bounded:

- `owner`
- `approvedBy`
- `changeRef`
- `expiresAt`

Each approved violation must also carry:

- `code`
- `expectedCount`
- `expectedImpact`
- `reason`

When that approval file is actually consumed by `sealos sync policy-gate`, the
current CLI and CI output now surface that fact directly through:

- `gate.approvalSummary.approvalProvided`
- `gate.approvalSummary.approvalApplied`
- `gate.approvalSummary.owner`
- `gate.approvalSummary.approvedBy`
- `gate.approvalSummary.changeRef`
- `gate.approvalSummary.expiresAt`
- `gate.approvalSummary.expiresSoon`
- `gate.approvalSummary.daysUntilExpiry`
- `gate.approvalSummary.followUpAction`
- `gate.approvalSummary.approvedViolationCodes`
- `gate.approvedViolations[].impact`

That file may explicitly approve:

- `wideningChange`
- `incompatiblePatches`

The current gate also enforces approval lifecycle directly:

- an approval file without `owner`, `approvedBy`, or `changeRef` is invalid
- an approval file without `expiresAt` is invalid
- an approval file whose `expiresAt` is already in the past is rejected even
  if the impact itself would otherwise match

When the approval is still valid but getting close to expiry, the current gate
also adds a warning and recommends a follow-up action through
`gate.approvalSummary.followUpAction`, for example renewing or removing the
approval before it expires.

In the current repo automation, the lightweight CI lane enables the stricter
mode for this check, so near-expiry approvals are treated as blocking until
they are renewed or removed.

For a concrete example of the current output shape, see:

- [examples/sync-drift-minimal/policy-gate-approved.example.yaml](../examples/sync-drift-minimal/policy-gate-approved.example.yaml)

Approval hygiene is now also checked outside policy-change PRs. The current
repo exposes a time-based scanner through:

- `sealos sync policy-approval-scan --root ...`
- `make verify-local-patch-policy-approvals APPROVAL_SCAN_ROOT=...`

Its current semantics are:

- invalid approval files are blocking
- expired approval files are blocking
- near-expiry approval files are warnings by default
- near-expiry approval files become blocking when
  `--fail-when-approval-expires-soon` is set

The repo also runs that scanner on a schedule through:

- [.github/workflows/local_patch_policy_approval_scan.yml](../../.github/workflows/local_patch_policy_approval_scan.yml)

That scheduled lane currently enables strict near-expiry behavior, so approvals
that are close to expiry are surfaced even when no policy change is being
proposed.

In the current MVP, the default gate semantics are:

- fail if the candidate policy widens cluster-local override surface
- fail if the candidate policy would reject existing `local-repo/patches/**`
- keep narrowing changes as warnings that still require review, but do not fail
  by themselves

## Recommended Authoring Loop

The smallest safe loop in the current MVP is:

1. Edit `local-repo/policy/local-patch-policy.yaml`.
2. Render a bundle.
3. Inspect the rendered `bundle/policy/local-patch-policy.yaml`.
4. Confirm the bundle provenance fields match the rendered artifact.
5. Verify one valid patch still passes.
6. Verify one invalid patch still fails.
7. Re-check whether the change should really stay local, or should instead move
   into shared baseline design.

## When Not To Change Policy

Do not change `LocalPatchPolicy` when the real problem is:

- a missing package extension point
- a shared baseline default that should be improved centrally
- a package/BOM release decision
- a generated projection that should be fixed by changing input or baseline,
  not by widening local patch surface

In those cases, changing policy would only hide the real boundary problem.

## Current Practical Rule

Treat `LocalPatchPolicy` as a budget for cluster-local override, not as an
escape hatch for package or BOM ownership.

If a proposed path feels like "operators might need this someday", that is not
enough. In the current MVP, widening local policy should be tied to a specific
cluster-local use case and a concrete validation example.
