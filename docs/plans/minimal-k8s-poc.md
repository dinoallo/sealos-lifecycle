# Plan: Minimal Package-Based Kubernetes PoC

## Status

Locally validated PoC record

This document now serves as both the original PoC plan and a local validation
record. Host-specific notes describe one prepared Linux environment and should
not be read as a generic repo guarantee.

## Goal

Prove that the new package and BOM flow in this repository can bootstrap a real
single-node Kubernetes cluster with the narrowest possible scope:

1. select a published, digest-pinned release target
2. initialize the cluster-local repo for that target
3. validate and render the target with `sealos sync` commands
4. apply the rendered bundle on one Linux host
5. validate that Kubernetes API, node readiness, and Cilium all come up

This PoC is intentionally narrow. It follows the 0-to-1 guide directly instead
of hiding the flow behind repository helper scripts. Package assembly and asset
fetching belong to release build/publish automation, not the operator-facing
PoC.

## What Already Exists In This Repo

The PoC is no longer just a proposal. The repo already contains the main
render/apply pieces:

- BOM, package, hydration, and applied-state types under `pkg/distribution/*`
- `sealos sync render` in `cmd/sealos/cmd/sync.go`
  - resolves BOM package artifacts from OCI image references by default, pulling them into a digest-keyed runtime cache first
  - still supports `--package-source` as a local package-directory override for development
- `sealos sync apply` in `cmd/sealos/cmd/sync.go`
- a render path test for this PoC in `cmd/sealos/cmd/sync_test.go`
- PoC package templates and default local inputs under
  `scripts/poc/minimal-single-node/`
- direct 0-to-1 install guidance in `docs/guides/day-0-install.md`

The main repo and host path has now been proven on this machine. The remaining
gaps are broader follow-up work rather than blockers for the PoC itself:

- publishing productized release assets outside the repository checkout
- making fresh-host setup more automated without turning the PoC into a wrapper
- expanding real-host multi-node acceptance coverage for the current
  CLI-driven `sync apply` orchestration path
- extending the PoC beyond one machine and one cluster

## Current Machine Reality

This section records the current state of the machine after successful setup and
PoC execution on 2026-04-27.

| Item | Current State | Impact |
| --- | --- | --- |
| OS / arch | Linux x86_64 | Matches the amd64 PoC packages. |
| User | `root` | Satisfies the host-apply root requirement. |
| `gcc` | present at `/usr/bin/gcc` | Satisfies the CGO build requirement for `sealos`. |
| `go` | `go1.23.1` on `PATH` | `make build BINS=sealos` works. |
| `sealos` | present on `PATH` | `sealos sync render` works directly. |
| `containerd`, `ctr`, `runc` | present at PoC versions from the selected release target | Runtime payload is install-ready. |
| `kubeadm`, `kubelet`, `kubectl` | present at PoC versions from the selected release target | Kubernetes payload is install-ready. |
| PID 1 | `systemd` | `sealos sync apply` can manage services on this host. |
| `systemctl is-system-running` | `running` | Confirms this host is valid for the PoC apply path. |
| swap | disabled and `swap.img.swap` masked | Satisfies kubeadm preflight and survives reboot. |
| cluster state | single-node cluster is up | `kubectl` reports one `Ready` control-plane node with Cilium healthy. |

Bottom line for this environment:

- compile succeeded
- render succeeded
- apply succeeded
- validation succeeded

## PoC Scope

### In Scope

- BOM artifact resolution through cached OCI package images
- one BOM with three components
- rendering via `sealos sync render`
- applying via `sealos sync apply`
- validation with normal Kubernetes and `sealos sync status` commands
- single-node Kubernetes bootstrap on one Linux host

### Out Of Scope

- OCI build and push pipeline
- asset fetching and package staging on the install host
- repository helper scripts as the PoC operator interface
- multi-node PoC validation
- controller-driven multi-node rollout policy
- upgrades, rollback, or drift workflows
- long-running reconcile loop
- secret management beyond local static files

## Required PoC Shape

### Cluster Topology

- single-node control plane
- same machine also schedules workloads
- no HA
- no external load balancer

### Package Set

The first PoC should continue to use exactly these three packages:

1. `containerd-runtime`
2. `kubernetes-rootfs`
3. `cilium-cni`

That remains the right split for this repo because:

- `containerd` has its own host lifecycle boundary
- Kubernetes node bootstrap assets should move together
- CNI should remain independently swappable

Current Cilium profile in the repo:

- `kubeProxyReplacement: false`
- `operator.replicas: 1`
- `hubble.enabled: false`

## Repo Assets And Fixtures

| Path | Role |
| --- | --- |
| `docs/guides/day-0-install.md` | Scriptless 0-to-1 operator flow. |
| `scripts/poc/minimal-single-node/bom.yaml` | Development BOM fixture for render and package tests. |
| `scripts/poc/minimal-single-node/packages/containerd/` | Local `containerd-runtime` package fixture. |
| `scripts/poc/minimal-single-node/packages/kubernetes/` | Local `kubernetes-rootfs` package fixture. |
| `scripts/poc/minimal-single-node/packages/cilium/` | Local `cilium-cni` package fixture. |
| `cmd/sealos/cmd/sync_package.go` | First-class `sealos sync package build/push/pull` CLI for OCI component package images. |
| `scripts/poc/minimal-single-node/smoke.sh` | Safe package lifecycle smoke wrapper for package inspect, local repo init/doctor, source preflight, render, runtime preflight, plan, and `sourcePreflight` metadata verification. |
| `scripts/poc/minimal-single-node/fetch-assets.sh` | Release-builder helper for downloading fixture assets; not an install step. |
| `scripts/poc/minimal-single-node/stage-assets.sh` | Release-builder helper for assembling fixture package payloads; not an install step. |
| `scripts/poc/minimal-single-node/publish-oci.sh` | CI/release-builder helper for creating a local OCI-backed fixture release. |
| `scripts/poc/minimal-single-node/render.sh` | Legacy convenience wrapper around `sealos sync render`; the guide uses direct CLI commands. |
| `scripts/poc/minimal-single-node/bootstrap.sh` | Legacy end-to-end wrapper retained for compatibility gates, not the PoC operator path. |
| `scripts/poc/minimal-single-node/validate.sh` | Legacy PoC validator; the guide uses direct Kubernetes and `sync status` checks. |

## Package Responsibilities

### 1. `containerd-runtime`

Class:

- `rootfs`

Current manifest:

- rootfs payload at `rootfs/`
- config file at `files/etc/containerd/config.toml`
- hooks:
  - `preflight`
  - `bootstrap`
  - `healthcheck`

Current repo behavior:

- the manifest models `preflight` as a hook named `preflight` with
  `phase: bootstrap`
- this matches the current package API, which has no separate `preflight`
  phase

Host responsibility:

- install `containerd`, `ctr`, `containerd-shim-runc-v2`, and `runc`
- write `/etc/containerd/config.toml`
- enable and restart `containerd`

### 2. `kubernetes-rootfs`

Class:

- `rootfs`

Current manifest:

- rootfs payload at `rootfs/`
- kubeadm config at `files/etc/kubernetes/kubeadm.yaml`
- sysctl profile at `files/etc/sysctl.d/99-kubernetes.conf`
- bootstrap manifests at `manifests/bootstrap/`
- hooks:
  - `preflight`
  - `bootstrap`
  - `healthcheck`

Host responsibility:

- install `kubeadm`, `kubelet`, and `kubectl`
- write kubeadm and sysctl files
- run `kubeadm init --config /etc/kubernetes/kubeadm.yaml`
- export admin kubeconfig
- apply bootstrap manifests after API readiness

### 3. `cilium-cni`

Class:

- `application`

Current manifest:

- Cilium manifest at `manifests/cilium.yaml`
- values file at `files/values/basic.yaml`
- hook:
  - `healthcheck`

Host responsibility:

- apply the Cilium manifest after Kubernetes bootstrap
- wait for Cilium daemonset and operator rollout

## Important Repo Constraint: Release Payloads

The package directories intentionally still contain placeholder payloads for the
runtime and Kubernetes rootfs content:

- `scripts/poc/minimal-single-node/packages/containerd/rootfs/README`
- `scripts/poc/minimal-single-node/packages/kubernetes/rootfs/README`

That is enough for render-only validation because `packageformat.LoadDir`
requires referenced files to exist, but it does not care whether they are real
binaries.

The Cilium package is different now: `scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml`
is a tracked generated manifest because the package directory is meant to remain
install-ready once assets have been refreshed.

Those in-tree package directories are not the 0-to-1 install target. They are
fixtures and development sources. The operator-facing PoC must start from a BOM
or `ReleaseChannel` whose component package artifacts already point at real,
published payloads.

This boundary matters because `sealos sync apply` rejects placeholder or
incomplete staged bundles before host mutation starts. That includes missing
runtime or Kubernetes binaries and a Cilium manifest that does not contain the
expected daemonset and deployment payloads.

Therefore, any real host run must consume already assembled release packages and
then render from that release target. Asset fetching, staging, package image
builds, and package image pushes are release-builder work.

## BOM Shape

The current BOM already matches the intended three-component dependency chain:

1. `containerd`
2. `kubernetes` depends on `containerd`
3. `cilium` depends on `kubernetes`

That ordering is consumed by `hydrate.BuildPlanFromResolved`, which topologically
sorts component dependencies before rendering.

## Render Output Shape

`sealos sync render` currently materializes the desired state bundle to:

- `<sealos-run-root>/distribution/bundles/current`

and applied state to:

- `<sealos-run-root>/distribution/applied-revision.yaml`

The bundle contains:

- `bundle.yaml`
- `components/<name>/package.yaml`
- `components/<name>/files/...`

This is not hypothetical. It is the behavior implemented by:

- `pkg/distribution/reconcile/materialize.go`
- `pkg/distribution/hydrate/render.go`

For the validated run recorded in this document:

- `<repo-root>` was `/root/sealos-lifecycle`
- `<sealos-run-root>` was `/root/.sealos/poc-minimal`

## Execution Plan

### Repository Smoke Gates

Use the standard smoke target when you want to verify repository package
fixtures without mutating the host:

```bash
make verify-sync-package-smoke
```

The default smoke path is a repository gate, not the operator-facing PoC
install flow. It builds the current `sealos` binary, uses a temporary runtime
root and local repo, inspects the three package fixture directories,
initializes and fills the cluster-local repo from package default inputs, runs
local-repo doctor, validates source inputs, runs source preflight, renders the
bundle, runs rendered-bundle runtime preflight against a temporary host root,
runs `sync plan`, and verifies the rendered bundle contains non-blocking
`spec.sourcePreflight` metadata.

Every smoke run writes an `acceptance-report.yaml` file under its workdir unless
`--report-file` is passed through `SYNC_PACKAGE_SMOKE_ARGS`. The report records
the BOM, package sources, local repo, rendered bundle, desired-state digest,
preflight states, post-apply or post-revert state when available, and the status
and output path for each stage. Secret values are not copied into the report.
The smoke script validates the report with `check-report.sh` before returning
success, using `safe`, `apply`, or `revert` mode according to the selected
mutation flags.

When a mutating apply or revert acceptance run is being used as release
evidence, convert the report into a local promotion proof with:

```bash
sealos sync health-proof \
  --file scripts/poc/minimal-single-node/bom.yaml \
  --acceptance-report "${WORKDIR}/acceptance-report.yaml" \
  --output-file proofs/minimal-single-node-health.yaml
```

The generated `DistributionHealthProof` is conservative: safe smoke reports
without mutating apply evidence, reports whose BOM file, rendered BOM
line/revision, or rendered BOM digest differ from the target BOM, reports
missing rendered desired-state/local-repo revision digests, missing expected
acceptance stages, failed stages, blocked preflight, or missing clean post-apply
state produce `spec.passed: false` and should not satisfy beta/stable promotion
policy.

Host mutation is deliberately opt-in:

```bash
make verify-sync-package-apply I_UNDERSTAND_THIS_MUTATES_HOST=1
```

The apply acceptance target requires the explicit confirmation variable because
it mutates the real host by default. It reuses the same source preflight, render,
runtime preflight, and plan path, then runs `sync apply`, `sync status`,
`sync diff`, and legacy repository validation checks against the selected
kubeconfig and host root. Extra smoke arguments can still be passed through
`SYNC_PACKAGE_SMOKE_ARGS`.

A stricter mutating acceptance target also validates the current revert loop:

```bash
make verify-sync-package-revert I_UNDERSTAND_THIS_MUTATES_HOST=1
```

This is not an uninstall test and it does not delete data-plane resources such
as Secrets, PVCs, or databases. It first applies and validates the bundle, then
injects a temporary, object-scoped drift into the Cilium ConfigMap, verifies
`sync diff` observes the drift, runs object-scoped `sync revert`, verifies the
rendered desired value is restored, and validates the cluster again.

OCI package image builds are also opt-in with `--build-packages`; the default
safe smoke path focuses on package parsing, source readiness, render, runtime
preflight, and plan so it can run in local developer and CI environments without
a registry or prepared host.

### Phase 0: Compile Or Obtain `sealos`

Status:

- completed on this machine

Required version:

- Go `1.23.1`, matching `go.work` and `go.mod`

Build command once Go is installed:

```bash
cd <repo-root>
make build BINS=sealos
```

Expected binary:

```text
<repo-root>/bin/linux_amd64/sealos
```

Success criteria:

- `./bin/linux_amd64/sealos version` runs

### Phase 1: Select A Release Target

Status:

- completed on this machine

The PoC install starts from release metadata that already points at real package
payloads:

```bash
CLUSTER=poc-minimal
RUNTIME_ROOT=/var/lib/sealos/runtime
RELEASE_SOURCE=/var/lib/sealos/distribution/release-source
RELEASE_LINE=minimal-single-node
RELEASE_CHANNEL=alpha
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

Success criteria:

- `${RELEASE_SOURCE}/channels/${RELEASE_LINE}/${RELEASE_CHANNEL}.yaml` exists
- the resolved channel points at a BOM with `spec.bomDigest`
- the BOM component artifacts are digest-pinned OCI package images

### Phase 2: Initialize Local Repo And Render

Status:

- completed on this machine

Initialize the local repo from the selected release target:

```bash
cd <repo-root>
sudo ./bin/linux_amd64/sealos sync local-repo init \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --output-dir "$LOCAL_REPO" \
  --overwrite
```

The current PoC packages require three local inputs. For the stock PoC values,
fill them from the tracked package defaults:

```bash
cd <repo-root>
sudo install -D -m 0644 \
  scripts/poc/minimal-single-node/packages/containerd/files/etc/containerd/config.toml \
  "${LOCAL_REPO}/inputs/containerd/containerd-config.toml"
sudo install -D -m 0644 \
  scripts/poc/minimal-single-node/packages/kubernetes/files/etc/kubernetes/kubeadm.yaml \
  "${LOCAL_REPO}/inputs/kubernetes/kubeadm-cluster-config.yaml"
sudo install -D -m 0644 \
  scripts/poc/minimal-single-node/packages/cilium/files/values/basic.yaml \
  "${LOCAL_REPO}/inputs/cilium/cilium-values.yaml"
```

Validate the source side and render directly with the CLI:

```bash
sudo ./bin/linux_amd64/sealos sync local-repo doctor \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo ./bin/linux_amd64/sealos sync validate \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo ./bin/linux_amd64/sealos sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"
```

Success criteria:

- OCI-backed render completes with no `--package-source` overrides
- `bundle.yaml` exists under
  `<sealos-run-root>/distribution/bundles/current`
- a new desired-state digest is emitted
- `<sealos-run-root>/distribution/applied-revision.yaml` is updated
- the OCI-backed render path pulls package images from a registry and still
  produces the same rendered bundle shape
- pulled package images are cached under the cluster runtime distribution store by image digest
- local repo doctor and source validation pass without blocking diagnostics

### Phase 3: Preflight And Apply On A Real Host

Status:

- completed on this machine

Minimum host prerequisites:

- Linux host or VM with `systemd` as PID 1
- root access
- swap disabled
- suitable cgroup and BPF mounts for the chosen Cilium profile
- kernel modules available:
  - `overlay`
  - `br_netfilter`
- commands available:
  - `systemctl`
  - `modprobe`
  - `sysctl`
  - `kubectl`
  - `kubeadm`
  - `kubelet`

Product CLI command used on this host:

```bash
cd <repo-root>
sudo ./bin/linux_amd64/sealos sync preflight \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"

sudo ./bin/linux_amd64/sealos sync apply \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"
```

Expected apply order in the current `sync apply` executor:

1. containerd preflight
2. containerd rootfs and config copy
3. containerd bootstrap
4. containerd healthcheck
5. Kubernetes rootfs and config copy
6. Kubernetes preflight
7. sysctl apply
8. Kubernetes bootstrap
9. wait for API and remove control-plane taints
10. apply Kubernetes bootstrap manifests
11. Kubernetes healthcheck
12. apply Cilium manifests
13. Cilium healthcheck
14. health checks with Kubernetes rollout and `sync status` commands

Success criteria:

- `/etc/kubernetes/admin.conf` exists
- `kubectl get nodes` shows one `Ready` node
- control plane and Cilium workloads are healthy

### Phase 4: Validate

Status:

- completed on this machine

Command:

```bash
cd <repo-root>
kubectl --kubeconfig "$KUBECONFIG" get nodes -o wide
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status ds/cilium --timeout=180s
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status deploy/cilium-operator --timeout=180s
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status deploy/coredns --timeout=180s
sudo ./bin/linux_amd64/sealos sync status \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER"
```

Validation checks:

- node readiness
- `coredns` rollout
- `cilium` daemonset rollout
- `cilium-operator` deployment rollout
- rendered/applied distribution state is visible through `sync status`

## Milestone Status

### Milestone 1: Render-Only PoC

Status:

- completed on this machine

Exit criteria:

- `sealos sync render` succeeds from a selected BOM or `ReleaseChannel`
- no PoC wrapper is required for target selection or render

### Milestone 2: Real-Payload Release Target

Status:

- completed on this machine

Exit criteria:

- the selected BOM points at digest-pinned package artifacts
- rendered bundles contain real runtime, Kubernetes, and Cilium payloads

### Milestone 3: Single-Node Host Bootstrap

Status:

- completed on this machine

Exit criteria:

- apply succeeds on a real systemd host or VM
- API server is healthy
- node is `Ready`

### Milestone 4: CNI-Complete PoC

Status:

- completed on this machine

Exit criteria:

- Cilium is healthy
- distribution status is available after apply

## Main Risks

### Risk: Confusing render-ready with install-ready

Mitigation:

- keep the doc explicit that in-tree placeholder package directories are
  development fixtures, not install targets
- always render host installs from a published release target

### Risk: Repeatability on a fresh host

Mitigation:

- keep the install path as explicit `sealos sync` commands
- keep generated artifacts out of git by default
- preserve the exact package versions and host prerequisites in this doc

### Risk: Missing build prerequisite clarity

Mitigation:

- require Go `1.23.1`
- keep `make build BINS=sealos` as the single supported compile path in the doc

### Risk: Hidden host preflight failures

Mitigation:

- keep swap disabled
- fail fast on `systemctl`, `modprobe`, containerd socket, and kubeadm
  prerequisites

## Recommended Next Actions

1. Reboot this host once and rerun a short health check to confirm swap stays
   disabled and the cluster survives restart as expected.
2. Publish the minimal PoC release source as a stable test fixture outside the
   repository checkout.
3. Keep CI helper scripts scoped to fixture generation and repository gates.
4. Add a protected real-host multi-node validation path that runs the same
   scriptless `sync render -> preflight -> apply -> status` sequence.

## Bottom Line

This repo already contains the correct minimal shape for the package-based
Kubernetes PoC:

- one `containerd` package
- one Kubernetes package
- one Cilium package
- one BOM
- one release target selection
- one local repo initialization path
- one `sync render -> preflight -> apply -> status` command sequence

The core PoC is no longer hypothetical in this repo or on this host:

- `sealos` built successfully
- a digest-pinned release target rendered successfully
- preflight and apply succeeded
- the single-node control plane bootstrapped successfully
- Cilium became healthy
- distribution status was available after apply

The next work is hardening release publication and repeatability, not first-time
feasibility.
