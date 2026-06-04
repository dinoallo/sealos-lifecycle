# Current Package Capability Matrix

## Status

Repo-aligned capability snapshot for the current BOM-driven MVP

## Summary

This document answers one narrow question:

- what package-related capability is already present in this repository today
- what is present but intentionally narrow
- what should not be mistaken as already implemented

The scope here is the current `sync`-based, BOM-driven MVP.

## Reading Rule

Read this matrix as an implementation snapshot, not as a long-term promise.

- `Ready now` means the repository already has a concrete code path and tests
  for the capability.
- `Ready with boundary` means the capability exists, but only inside the
  current single-node or narrow-surface model.
- `Not implemented` means users should not assume the repository already
  supports that workflow.

## Ready Now

| Capability | Current State | Main Evidence | Important Caveat |
| --- | --- | --- | --- |
| Author and validate a component package directory | Ready now | [pkg/distribution/packageformat/load.go](../../pkg/distribution/packageformat/load.go), [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go) | The contract is still marked experimental in the wider `sync` flow. |
| Inspect package metadata from a local package directory | Ready now | `sealos sync package inspect` in [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go), tests in [cmd/sealos/cmd/sync_package_test.go](../../cmd/sealos/cmd/sync_package_test.go) | This is package-directory inspection, not release selection. |
| Build one OCI image from one component package directory | Ready now | `sealos sync package build` in [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go), staging logic in [pkg/distribution/ocipackage/ocipackage.go](../../pkg/distribution/ocipackage/ocipackage.go) | Build still depends on the local image/build environment being prepared. |
| Push a built OCI component package image and capture its digest | Ready now | `sealos sync package push` in [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go) | Push is transport-level only; it does not update any BOM automatically. |
| Pull an OCI component package image into a local package directory | Ready now | `sealos sync package pull` in [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go), reusable pull/cache logic in [pkg/distribution/ocipackage/pull.go](../../pkg/distribution/ocipackage/pull.go) | Pull extracts the package filesystem and validates `package.yaml`; registry auth is still delegated to the local image/buildah environment. |
| Resolve component packages from a BOM | Ready now | [pkg/distribution/bom/resolve.go](../../pkg/distribution/bom/resolve.go), `sync render` in [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go) | Resolution is still explicit-BOM driven. |
| Resolve packages from either local directory overrides or cached OCI artifacts | Ready now | [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), [pkg/distribution/ocipackage/pull.go](../../pkg/distribution/ocipackage/pull.go), [pkg/distribution/packageformat/load.go](../../pkg/distribution/packageformat/load.go) | BOM-driven OCI references are pulled into the cluster runtime cache under a digest-derived key before render/validate reads them. |
| Render a BOM into a hydrated desired-state bundle | Ready now | [pkg/distribution/reconcile/materialize.go](../../pkg/distribution/reconcile/materialize.go), [pkg/distribution/hydrate/render.go](../../pkg/distribution/hydrate/render.go) | Render is cluster-targeted but still centered on the current single-node path. |
| Apply a rendered bundle to the resolved cluster targets | Ready now | [pkg/distribution/reconcile/apply.go](../../pkg/distribution/reconcile/apply.go) | The deployment unit is still the rendered bundle, and multi-node behavior is executor-level with optional host batching rather than controller-driven. |
| Carry package content types through deployment | Ready now | `rootfs`, `file`, `manifest`, hooks through [pkg/distribution/reconcile/apply.go](../../pkg/distribution/reconcile/apply.go) | The semantics are intentionally narrow and repo-specific for the MVP. |
| Initialize a cluster-local repo skeleton from package input contracts | Ready now | `sealos sync local-repo init` in [cmd/sealos/cmd/sync_localrepo.go](../../cmd/sealos/cmd/sync_localrepo.go), tests in [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go) | It creates templates and policy metadata only; real Secret values must still be supplied by operators. |
| Inspect a cluster-local repo before validation/render | Ready now | `sealos sync local-repo doctor` in [cmd/sealos/cmd/sync_localrepo.go](../../cmd/sealos/cmd/sync_localrepo.go), tests in [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go) | It catches unresolved init templates, stale component dirs, Secret-like permission/kind mistakes, and missing local patch policy without printing Secret payload. |
| Bind local repo `inputs/`, `resources/`, and `patches/` into render/apply | Ready now | [pkg/distribution/localrepo/localrepo.go](../../pkg/distribution/localrepo/localrepo.go), [pkg/distribution/reconcile/materialize.go](../../pkg/distribution/reconcile/materialize.go) | The local-repo model is still cluster-local and single-node scoped. |
| Validate a BOM, local package sources, local repo, and cluster topology before render/apply | Ready now | `sealos sync validate` in [cmd/sealos/cmd/sync_validate.go](../../cmd/sealos/cmd/sync_validate.go) | Validation is read-only and checks package/local-repo/topology conformance; it is not a live cluster health check. |
| Run, enforce, and record source preflight before render | Ready now | `sealos sync preflight --file ...` and the default `sealos sync render` source gate in [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), bundle metadata in [pkg/distribution/hydrate/bundle.go](../../pkg/distribution/hydrate/bundle.go), tests in [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go) | Source preflight aggregates local-repo doctor and validate, then render persists a sanitized summary into `spec.sourcePreflight`. |
| Check rendered bundle freshness and runtime readiness before apply | Ready now | `sealos sync preflight --bundle-dir ...` in [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), runtime checks in [sync_runtime_preflight.go](../../cmd/sealos/cmd/sync_runtime_preflight.go) | Rendered-bundle preflight checks topology/render-input freshness plus local host/runtime readiness such as privileges, systemd, swap, Kubernetes state, ports, known binaries, kubeconfig/client availability, and managed service state. Runtime warnings stay in structured output; blocking runtime checks gate `sync apply`. |
| Preview rendered bundle apply intent before mutating the cluster | Ready now | `sealos sync plan` in [cmd/sealos/cmd/sync_plan.go](../../cmd/sealos/cmd/sync_plan.go) | The plan is static and read-only: it resolves targets, summarizes resources, reports package/phase safety profiles, and marks generated host projections, but does not run SSH, kubectl, or dynamic apply probes. |
| Resolve a release channel through the release metadata API | Ready now | `sealos sync render --release-source --release-line --channel`, `sealos sync release-metadata serve` in [sync_release_metadata.go](../../cmd/sealos/cmd/sync_release_metadata.go), lookup client/service in [pkg/distribution/bom](../../pkg/distribution/bom), tests in [channel_test.go](../../pkg/distribution/bom/channel_test.go) and [sync_test.go](../../cmd/sealos/cmd/sync_test.go) | The service exposes digest-pinned `ReleaseChannel` and BOM documents over `GET /v1/distributions/{line}/channels/{channel}` and `GET /v1/distributions/{line}/revisions/{revision}/bom`. |
| Advance a release channel through the health-gated promotion API | Ready now | `POST /v1/distributions/{line}/channels/{channel}/promotions` in [release_metadata_service.go](../../pkg/distribution/bom/release_metadata_service.go), shared promotion policy in [channel.go](../../pkg/distribution/bom/channel.go), tests in [channel_test.go](../../pkg/distribution/bom/channel_test.go) | The service accepts a structured promotion request with `targetRevision`, approval metadata, and a passed `DistributionHealthProof`, writes proof evidence, evaluates source/target channel policy plus health evidence thresholds, and advances the local channel file. This is a local metadata service, not a multi-tenant hosted release platform. |
| Promote a local `ReleaseChannel` to a target BOM revision with policy-gated health proof | Ready now | `sealos sync promote` in [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), `DistributionHealthProof` and promotion helper in [pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go), policy rules in [pkg/distribution/promotion](../../pkg/distribution/promotion) | This updates one local channel file, validates the target BOM belongs to the same line, enforces source/target channel policy, requires a passed local `DistributionHealthProof` for beta/stable target channels, rejects missing or failed required signals and unmet `minPassedSignals`, returns `policyDecision`, and appends `spec.promotionHistory[]` with proof metadata when present. |
| Generate local promotion health proof from package acceptance evidence | Ready now | `sealos sync health-proof` in [cmd/sealos/cmd/sync_health_proof.go](../../cmd/sealos/cmd/sync_health_proof.go), `acceptance-report.yaml` from [scripts/poc/minimal-single-node/smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) | This converts a `PackageAcceptanceReport` into a `DistributionHealthProof` for the selected target BOM line/revision. The generated proof normalizes required signals with `source`, `evidenceRef`, `thresholds.requiredSignals`, `thresholds.minPassedSignals`, and `signalSummary`. Preflight-only smoke reports, target identity/digest mismatches, missing rendered desired-state/local-repo digests, missing acceptance stage contract or mutating markers, missing mutating apply, or missing clean post-apply evidence produce `spec.passed: false`. |
| Diff, status, commit, and revert against the rendered desired state | Ready now | [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), [pkg/distribution/compare](../../pkg/distribution/compare), [pkg/distribution/commit](../../pkg/distribution/commit) | This is the current `sync` operator loop, not a controller yet. |
| Run a safe end-to-end package lifecycle smoke flow | Ready now | `make verify-sync-package-smoke`, backed by [scripts/poc/minimal-single-node/smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) | The default smoke path builds the current CLI, uses temporary state, runs package inspect, local-repo init/doctor, source preflight, render, runtime preflight, plan, and sourcePreflight verification. Host mutation and OCI image build are explicit opt-ins via `SYNC_PACKAGE_SMOKE_ARGS`. |
| Run a mutating single-node apply acceptance flow | Ready now | `make verify-sync-package-apply I_UNDERSTAND_THIS_MUTATES_HOST=1`, backed by [scripts/poc/minimal-single-node/smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) | This target intentionally mutates the host. It reuses the smoke path, then runs `sync apply`, `sync status`, `sync diff`, and `validate.sh` after the rendered-bundle runtime preflight passes. Extra smoke arguments still flow through `SYNC_PACKAGE_SMOKE_ARGS`. |
| Run a mutating single-node drift/revert acceptance flow | Ready now | `make verify-sync-package-revert I_UNDERSTAND_THIS_MUTATES_HOST=1`, backed by [scripts/poc/minimal-single-node/smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) | This target first runs the apply acceptance flow, then injects a temporary Cilium ConfigMap drift, verifies `sync diff` observes it, runs object-scoped `sync revert`, and verifies the rendered desired value is restored. This is drift recovery, not uninstall, and it does not delete Secret/PVC/database data-plane resources. |
| Produce and validate a package lifecycle acceptance report | Ready now | `acceptance-report.yaml` from [scripts/poc/minimal-single-node/smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh), validated by [scripts/poc/minimal-single-node/check-report.sh](../../scripts/poc/minimal-single-node/check-report.sh) | Every smoke/apply/revert run writes a report under the workdir unless `--report-file` is provided, then validates it in `safe`, `apply`, or `revert` mode. It captures stage status, output paths, BOM/package/local-repo identity, desired-state digest, and post-apply/post-revert state without copying Secret payloads. |
| Override the cluster runtime root for `sync` workflows | Ready now | `--runtime-root` in [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go) | This is primarily for tests, smoke runs, and scripted workflows that need explicit control over where rendered state, current bundles, and Clusterfile inventory are read from. |
| Policy-driven validation for cluster-local patch surface | Ready now | [pkg/distribution/ownership/policy.go](../../pkg/distribution/ownership/policy.go), [pkg/distribution/policyreport](../../pkg/distribution/policyreport) | The supported policy scope is still deliberately narrow. |

## Ready With Boundary

| Capability | Current State | Main Evidence | Boundary |
| --- | --- | --- | --- |
| Deploy package content to Kubernetes and the host from one unified flow | Ready with boundary | [pkg/distribution/reconcile/apply.go](../../pkg/distribution/reconcile/apply.go) | The deployment unit is the rendered bundle, not a standalone package install command. |
| Orchestrate a rendered bundle across multiple cluster hosts | Ready with boundary | [pkg/distribution/reconcile/topology.go](../../pkg/distribution/reconcile/topology.go), [pkg/distribution/reconcile/apply.go](../../pkg/distribution/reconcile/apply.go), [pkg/distribution/reconcile/kubeadm_bootstrap.go](../../pkg/distribution/reconcile/kubeadm_bootstrap.go) | The CLI-driven `sync apply` path resolves `allNodes`, `firstMaster`, and `cluster`, stages bundle payloads per remote host, handles kubeadm join configs, and fetches remote first-master kubeconfig for cluster-scoped steps. Package hooks/scripts still need to be multi-node-safe. |
| Resolve a local file-backed `ReleaseChannel` target | Ready with boundary | [pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go), [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), [pkg/distribution/agent/runner.go](../../pkg/distribution/agent/runner.go) | `--release-channel` loads one local channel document, validates its `distribution` and `targetRevision` against `spec.bomPath`, then renders the resolved BOM. Local files can be advanced with `sealos sync promote`; service-backed lookup is now available separately through `--release-source --release-line --channel`. |
| Run a process-level distribution reconcile agent | Ready with boundary | [cmd/sealos-agent/main.go](../../cmd/sealos-agent/main.go), [cmd/sealos-agent/cmd/root.go](../../cmd/sealos-agent/cmd/root.go), [pkg/distribution/agent/runner.go](../../pkg/distribution/agent/runner.go) | `sealos-agent` can run once or on an interval against a BOM or local `ReleaseChannel`. This remains useful for direct host execution and debugging. |
| Run a Kubernetes controller reconcile state machine | Ready with boundary | [pkg/distribution/controller](../../pkg/distribution/controller), `--controller` in [cmd/sealos-agent/cmd/root.go](../../cmd/sealos-agent/cmd/root.go), [deploy/distribution-controller/base](../../deploy/distribution-controller/base), [Controller install](../guides/controller-install.md) | `sealos-agent --controller` watches `DistributionTarget` and `DistributionRolloutPolicy` objects, delegates each target reconcile to the existing agent runner, writes explicit phase/status conditions, retry/backoff state, hold diagnostics, and Kubernetes events, supports optional leader election, and ships installable CRD/RBAC/deployment manifests. It does not drive release promotion automatically. |
| Batch host-targeted rendered-bundle rollout with canary, pause, health gate, and failure action | Ready with boundary | `--rollout-batch-size`, `--rollout-canary-size`, `--rollout-pause-after-canary`, `--rollout-health-gate`, and `--rollout-failure-action` in [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go) and [cmd/sealos-agent/cmd/root.go](../../cmd/sealos-agent/cmd/root.go), `DistributionRolloutPolicy` in [pkg/distribution/controller/types.go](../../pkg/distribution/controller/types.go), rollout handling in [pkg/distribution/reconcile/apply.go](../../pkg/distribution/reconcile/apply.go), successful revision history in [pkg/distribution/state](../../pkg/distribution/state) | These controls apply to eligible host-targeted all-node runtime-rootfs steps inside the rendered-bundle executor. `healthGate` runs component `healthcheck` hooks after each host batch, post-canary pause reports a non-degraded paused target and waits for an explicit target or policy update, and `Rollback` re-applies the last successful rendered revision retained under the cluster bundle store. The rollback snapshot includes BOM/target/local revision metadata and can cross BOM lines; durable per-package rollout cursors are still outside this surface. |
| Report package/phase rollout safety profiles | Ready with boundary | [cmd/sealos/cmd/sync_plan.go](../../cmd/sealos/cmd/sync_plan.go), `TestSyncPlanPackageSafetyModelCoversPackageClasses` in [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go) | `sync plan` now reports package and step safety profiles for rootfs, host-file, manifest, chart, patch, values, and hook phases, including approval requirements for local patch, upgrade, and remove flows plus generated host projection gates. It is still a static review model, not a durable per-package rollout cursor. |
| Commit and revert modeled drift across multi-node projections | Ready with boundary | [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), [cmd/sealos/cmd/sync_multinode.go](../../cmd/sealos/cmd/sync_multinode.go), [pkg/distribution/commit/commit.go](../../pkg/distribution/commit/commit.go), [sync-drift guide](../guides/sync-drift.md) | Kubernetes object commit/revert remains cluster-scoped. Host-path revert can target local or remote hosts, disambiguate multi-component paths with `--component`, and restore host-scoped local-input payloads. Host-path commit covers local-input regular files only, writes selected hosts back to host-scoped inputs when present, and rejects divergent selected-host commits without host-scoped provenance. Generated projections are diff/status routed, with direct repair only for `repairable=true` modeled control-plane host paths. |
| Track host files, Kubernetes objects, and modeled generated projections | Ready with boundary | [pkg/distribution/packageformat/types.go](../../pkg/distribution/packageformat/types.go), [pkg/distribution/hydrate/inventory.go](../../pkg/distribution/hydrate/inventory.go), [pkg/distribution/compare/compare.go](../../pkg/distribution/compare/compare.go) | Packages can declare generated host-path outputs under `ComponentPackage.spec.generatedOutputs.hostPaths[]`; render stores them in bundle `spec.trackedHostPaths[]` alongside known kubeadm static Pod discovery. The compare engine semantically checks generated Kubernetes-object host files, but arbitrary generated artifact types still need explicit modeling. |
| Manage drift for modeled generated host-path projections | Ready with boundary | [pkg/distribution/compare/compare.go](../../pkg/distribution/compare/compare.go), generated remediation tests in [pkg/distribution/compare/compare_test.go](../../pkg/distribution/compare/compare_test.go), CLI output tests in [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go) | `sync diff/status` routes modeled generated drift to local input, selected package/BOM baseline, or manual review, and emits `projectionClass`, `generator`, `generatedKind`, `generatedName`, and explicit `repairable` metadata. Current automatic repair remains limited to known kubeadm control-plane static Pod projections with a retained kubeadm input. |
| Support generated control-plane static Pod tracking | Ready with boundary | [pkg/distribution/hydrate/inventory.go](../../pkg/distribution/hydrate/inventory.go) | Kubeadm control-plane static Pods are discovered automatically from retained kubeadm input; packages can also declare non-kubeadm generated host-path projections explicitly. |
| Approval-governed local patch policy gate | Ready with boundary | [pkg/distribution/policyreport/gate.go](../../pkg/distribution/policyreport/gate.go), [.github/workflows/local_patch_policy_gate.yml](../../.github/workflows/local_patch_policy_gate.yml) | This governs local patch policy only, not every future ownership policy. |
| Time-based approval hygiene scanning | Ready with boundary | `sealos sync policy-approval-scan` in [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go), [.github/workflows/local_patch_policy_approval_scan.yml](../../.github/workflows/local_patch_policy_approval_scan.yml) | The scan is repo-level and approval-focused, not a general cluster health controller. |
| Package/BOM-defined local patch policy source | Ready with boundary | [pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go), [pkg/distribution/packageformat/types.go](../../pkg/distribution/packageformat/types.go), [pkg/distribution/hydrate/policy.go](../../pkg/distribution/hydrate/policy.go) | `LocalPatchPolicy` scope is still only `clusterLocal`; precedence is `localRepo > bom > package > builtInDefault`, the package source requires exactly one selected package policy unless localRepo/BOM wins, and there is no multi-layer policy merge. |

## Not Implemented

These are the main things users should **not** mistake as already done.

| Capability | Current State | Why It Matters |
| --- | --- | --- |
| Direct “install this package” workflow without BOM/bundle mediation | Not implemented | The current deployment path is `package -> BOM -> render -> bundle -> apply`, not package-direct install. |
| Hosted fleet release platform | Not implemented | The local release metadata service supports lookup and health-gated channel advancement, but it is not a multi-tenant hosted service with authentication, remote storage, evidence collection, or fleet policy management. |
| Fully generalized generated-output drift management | Not implemented | Modeled generated Kubernetes-object host-path drift is routed with structured metadata, but the MVP still does not cover every possible generated artifact type or provide automatic repair for arbitrary external generators. |
| Multi-layer policy merge across package, BOM, and cluster-local inputs | Not implemented | The current model intentionally rejects that complexity. |

## Practical Interpretation

The shortest accurate statement for the current repository is:

- packaging is ready
- package resolution is ready
- deployment is ready as a BOM-driven MVP
- CLI-driven multi-node bundle orchestration is ready with a narrow boundary
- local file-backed `ReleaseChannel` selection is ready with a narrow boundary
- `sealos-agent` process-level reconciliation is ready with a narrow boundary
- `sealos-agent --controller` minimal watched reconciliation and install manifests are ready with a narrow boundary
- host rollout batching and durable batch policy are ready with a narrow boundary
- health-gated host rollout and local health-gated release metadata promotion
  are implemented with narrow boundaries

That means the repository is already strong enough for:

- package authoring
- OCI package build, push, and pull
- BOM-driven render/apply workflows
- local `ReleaseChannel` target selection
- local `ReleaseChannel` advancement with explicit promotion policy and history
- process-level agent reconciliation
- minimal `DistributionTarget` controller reconciliation with installable manifests and RBAC
- batched host apply waves for rendered bundles
- durable batch-size rollout policy for controller targets
- drift and ownership experiments on the current CLI-driven path

But it is not yet the final shape of:

- multi-cluster release management
- Kubernetes controller-driven multi-node topology-aware deployment
- a hosted multi-tenant release platform with fleet evidence collection and
  policy management

## Related Documents

- Packaging contract:
  [Package format](../architecture/package-format.md)
- OCI packaging milestone:
  [OCI packaging milestone](../plans/oci-packaging-milestone.md)
- Minimal prepared-host PoC:
  [Minimal Kubernetes PoC](../plans/minimal-k8s-poc.md)
- Local repo and deployment loop:
  [Local repo and secret](../guides/local-repo-and-secret.md),
  [Sync drift](../guides/sync-drift.md)
- BOM and `ReleaseChannel` model, including the current local-file boundary:
  [BOM and channel](../guides/bom-and-channel.md)
- Controller install guide:
  [Controller install](../guides/controller-install.md)
