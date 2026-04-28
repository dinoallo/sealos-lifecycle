# Plan: Minimal Package-Based Kubernetes PoC

## Status

Verified on this machine

## Goal

Prove that the new package and BOM flow in this repository can bootstrap a real
single-node Kubernetes cluster with the narrowest possible scope:

1. use three local package directories
2. render them through `sealos sync render`
3. stage real runtime and Kubernetes payloads into the packages
4. apply the rendered bundle on one Linux host
5. validate that Kubernetes API, node readiness, and Cilium all come up

This PoC is intentionally narrow. `sealos sync apply` now exists for the
prepared single-node host flow, but this is still not a generic multi-node
engine, not a reconcile loop, and not a production installer.

## What Already Exists In This Repo

The PoC is no longer just a proposal. The repo already contains the main
render/apply pieces:

- BOM, package, hydration, and applied-state types under `pkg/distribution/*`
- `sealos sync render` in `cmd/sealos/cmd/sync.go`
  - resolves BOM package artifacts from OCI image references by default
  - still supports `--package-source` as a local package-directory override for development
- `sealos sync apply` in `cmd/sealos/cmd/sync.go`
- a render path test for this PoC in `cmd/sealos/cmd/sync_test.go`
- runnable PoC assets under `scripts/poc/minimal-single-node/`
- a PoC-only installer and validator:
  - `scripts/poc/minimal-single-node/install.sh`
  - `scripts/poc/minimal-single-node/validate.sh`

The main repo and host path has now been proven on this machine. The remaining
gaps are broader follow-up work rather than blockers for the PoC itself:

- deciding which generated PoC assets should stay tracked versus generated
- making fresh-host setup more automated
- broadening `sync apply` beyond the prepared single-node host path
- extending the PoC beyond one machine and one cluster

## Current Machine Reality

This section records the current state of the machine after successful setup and
PoC execution on 2026-04-27.

| Item | Current State | Impact |
| --- | --- | --- |
| OS / arch | Linux x86_64 | Matches the amd64 PoC packages. |
| User | `root` | Satisfies the installer's root requirement. |
| `gcc` | present at `/usr/bin/gcc` | Satisfies the CGO build requirement for `sealos`. |
| `go` | `go1.23.1` on `PATH` | `make build BINS=sealos` works. |
| `sealos` | present on `PATH` | `sealos sync render` works directly. |
| `containerd`, `ctr`, `runc` | present and staged at PoC versions | Runtime payload is install-ready. |
| `kubeadm`, `kubelet`, `kubectl` | present and staged at PoC versions | Kubernetes payload is install-ready. |
| PID 1 | `systemd` | `install.sh` can manage services on this host. |
| `systemctl is-system-running` | `running` | Confirms this host is valid for the PoC install path. |
| swap | disabled and `swap.img.swap` masked | Satisfies kubeadm preflight and survives reboot. |
| cluster state | single-node cluster is up | `kubectl` reports one `Ready` control-plane node with Cilium healthy. |

Bottom line for this environment:

- compile succeeded
- render succeeded
- install succeeded
- validation succeeded

## PoC Scope

### In Scope

- BOM artifact resolution through mounted OCI package images
- local package directories loaded via `packageformat.LoadDir` as the current verified PoC override path
- one BOM with three components
- rendering via `sealos sync render`
- applying via `sealos sync apply`
- a PoC-only host installer script
- a PoC-only validation script
- single-node Kubernetes bootstrap on one Linux host

### Out Of Scope

- OCI build and push pipeline
- multi-node join flow
- generic multi-node `sync apply`
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

## Repo Assets To Use

| Path | Role |
| --- | --- |
| `scripts/poc/minimal-single-node/bom.yaml` | The PoC BOM. |
| `scripts/poc/minimal-single-node/packages/containerd/` | Local `containerd-runtime` package. |
| `scripts/poc/minimal-single-node/packages/kubernetes/` | Local `kubernetes-rootfs` package. |
| `scripts/poc/minimal-single-node/packages/cilium/` | Local `cilium-cni` package. |
| `cmd/sealos/cmd/sync_package.go` | First-class `sealos sync package build/push` CLI for OCI component package images. |
| `scripts/poc/minimal-single-node/render.sh` | Convenience wrapper around `sealos sync render`, preferring the generated OCI BOM when present. |
| `scripts/poc/minimal-single-node/publish-oci.sh` | Publishes the PoC package set to OCI and writes an OCI-backed BOM. |
| `scripts/poc/minimal-single-node/stage-assets.sh` | Replaces placeholder payloads with real binaries and manifests. |
| `scripts/poc/minimal-single-node/fetch-assets.sh` | Optional helper to download Kubernetes, containerd, runc, and Cilium assets. |
| `scripts/poc/minimal-single-node/bootstrap.sh` | End-to-end prepared-host wrapper for build, publish, render, install, and validate. |
| `scripts/poc/minimal-single-node/install.sh` | PoC-only installer for the rendered bundle. |
| `scripts/poc/minimal-single-node/validate.sh` | PoC-only cluster health validation. |

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

## Important Repo Constraint: Placeholder Payloads

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

It is not enough for install-time execution:

- `install.sh` explicitly rejects placeholder rootfs payloads
- `install.sh` would also reject hook scripts if they were still placeholders
- `install.sh` also checks that the staged Cilium manifest contains a real
  daemonset and deployment payload

Therefore, any real host run must stage real assets first and then re-render the
bundle.

## BOM Shape

The current BOM already matches the intended three-component dependency chain:

1. `containerd`
2. `kubernetes` depends on `containerd`
3. `cilium` depends on `kubernetes`

That ordering is consumed by `hydrate.BuildPlanFromResolved`, which topologically
sorts component dependencies before rendering.

## Render Output Shape

`sealos sync render` currently materializes the desired state bundle to:

- `/root/.sealos/poc-minimal/distribution/bundles/current`

and applied state to:

- `/root/.sealos/poc-minimal/distribution/applied-revision.yaml`

The bundle contains:

- `bundle.yaml`
- `components/<name>/package.yaml`
- `components/<name>/files/...`

This is not hypothetical. It is the behavior implemented by:

- `pkg/distribution/reconcile/materialize.go`
- `pkg/distribution/hydrate/render.go`

## Execution Plan

### Phase 0: Compile Or Obtain `sealos`

Status:

- completed on this machine

Required version:

- Go `1.23.1`, matching `go.work` and `go.mod`

Build command once Go is installed:

```bash
cd /root/sealos-lifecycle
make build BINS=sealos
```

Expected binary:

```text
/root/sealos-lifecycle/bin/linux_amd64/sealos
```

Success criteria:

- `./bin/linux_amd64/sealos version` runs

### Phase 1: Stage Real PoC Assets

Status:

- completed on this machine

Fastest path on this machine:

- reuse the already installed host binaries for containerd and Kubernetes
- provide a real Cilium manifest

Command shape:

```bash
cd /root/sealos-lifecycle
scripts/poc/minimal-single-node/stage-assets.sh \
  --kubelet-bin /usr/bin/kubelet \
  --cilium-manifest /absolute/path/to/real/cilium.yaml
```

Notes:

- on this machine, `stage-assets.sh` can auto-discover:
  - `/usr/bin/containerd`
  - `/usr/bin/containerd-shim-runc-v2`
  - `/usr/bin/ctr`
  - `/usr/bin/runc`
  - `/usr/bin/kubeadm`
  - `/usr/bin/kubectl`
- `--kubelet-bin` is still required
- `--cilium-manifest` is required unless a chart directory and `helm` are
  provided

Optional helper path if you want the repo to fetch assets instead:

```bash
cd /root/sealos-lifecycle
assets_file=/tmp/poc-minimal-assets.env
scripts/poc/minimal-single-node/fetch-assets.sh > "${assets_file}"
set -a
. "${assets_file}"
set +a

scripts/poc/minimal-single-node/stage-assets.sh \
  --containerd-bin "${containerd_bin}" \
  --containerd-shim-bin "${containerd_shim_bin}" \
  --ctr-bin "${ctr_bin}" \
  --runc-bin "${runc_bin}" \
  --kubeadm-bin "${kubeadm_bin}" \
  --kubelet-bin "${kubelet_bin}" \
  --kubectl-bin "${kubectl_bin}" \
  --cilium-manifest "${cilium_manifest}"
```

Success criteria:

- real binaries exist under:
  - `packages/containerd/rootfs/usr/bin/`
  - `packages/kubernetes/rootfs/usr/bin/`
- `packages/cilium/manifests/cilium.yaml` is a real rendered install manifest

### Phase 2: Render The Bundle

Status:

- completed on this machine

Recommended command:

```bash
cd /root/sealos-lifecycle
scripts/poc/minimal-single-node/publish-oci.sh \
  --registry-prefix localhost:5065/poc-minimal > /tmp/poc-oci.env

set -a
source /tmp/poc-oci.env
set +a

scripts/poc/minimal-single-node/render.sh --cluster poc-minimal
```

Developer override path when iterating on in-tree package directories:

```bash
cd /root/sealos-lifecycle
./bin/linux_amd64/sealos sync render \
  --file scripts/poc/minimal-single-node/bom.yaml \
  --cluster poc-minimal \
  --package-source containerd=scripts/poc/minimal-single-node/packages/containerd \
  --package-source kubernetes=scripts/poc/minimal-single-node/packages/kubernetes \
  --package-source cilium=scripts/poc/minimal-single-node/packages/cilium
```

Success criteria:

- OCI-backed render completes with no `--package-source` overrides
- `bundle.yaml` exists under
  `/root/.sealos/poc-minimal/distribution/bundles/current`
- a new desired-state digest is emitted
- `/root/.sealos/poc-minimal/distribution/applied-revision.yaml` is updated
- the OCI-backed render path pulls package images from a registry and still
  produces the same rendered bundle shape
- the local override path still works for in-tree development without buildah or
  registry usage

### Phase 3: Install On A Real Host

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

Command used on this host:

```bash
cd /root/sealos-lifecycle
scripts/poc/minimal-single-node/install.sh \
  --cluster poc-minimal \
  --bundle-dir /root/.sealos/poc-minimal/distribution/bundles/current
```

Expected install order in the current script:

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
14. optional `validate.sh`

Success criteria:

- `/etc/kubernetes/admin.conf` exists
- `kubectl get nodes` shows one `Ready` node
- control plane and Cilium workloads are healthy

### Phase 4: Validate

Status:

- completed on this machine

Command:

```bash
cd /root/sealos-lifecycle
scripts/poc/minimal-single-node/validate.sh \
  --cluster poc-minimal \
  --bundle-dir /root/.sealos/poc-minimal/distribution/bundles/current \
  --kubeconfig /etc/kubernetes/admin.conf
```

Validation checks already implemented:

- API readiness via `/readyz`
- node readiness
- `coredns` rollout
- `cilium` daemonset rollout
- `cilium-operator` deployment rollout
- smoke pod scheduling and readiness

## Milestone Status

### Milestone 1: Render-Only PoC

Status:

- completed on this machine

Exit criteria:

- `sealos sync render` succeeds from local package sources

### Milestone 2: Real-Payload Bundle

Status:

- completed on this machine

Exit criteria:

- placeholder payloads are replaced with real runtime, Kubernetes, and Cilium
  assets
- bundle is re-rendered after staging

### Milestone 3: Single-Node Host Bootstrap

Status:

- completed on this machine

Exit criteria:

- install succeeds on a real systemd host or VM
- API server is healthy
- node is `Ready`

### Milestone 4: CNI-Complete PoC

Status:

- completed on this machine

Exit criteria:

- Cilium is healthy
- smoke pod reaches `Ready`

## Main Risks

### Risk: Confusing render-ready with install-ready

Mitigation:

- keep the doc explicit that placeholders are acceptable for render tests but
  not for host bootstrap
- always stage assets before the final render used for install

### Risk: Repeatability on a fresh host

Mitigation:

- keep the repo scripts executable and scriptable
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
2. Decide whether the real Cilium manifest under
   `scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml`
   should remain tracked or return to a generated-only workflow.
3. Use `scripts/poc/minimal-single-node/bootstrap.sh` as the default repeatable
   prepared-host path. It now exercises `publish -> render -> apply` through the
   OCI-backed package flow, and should be extended only if new host
   prerequisites or package inputs appear.
4. If this PoC will be reused, add a reset or cleanup script for tearing down
   the single-node cluster between runs.

## Bottom Line

This repo already contains the correct minimal shape for the package-based
Kubernetes PoC:

- one `containerd` package
- one Kubernetes package
- one Cilium package
- one BOM
- one render command
- one end-to-end bootstrap wrapper
- one PoC-only installer
- one PoC-only validator

The core PoC is no longer hypothetical in this repo or on this host:

- `sealos` built successfully
- real payloads were staged successfully
- the bundle rendered successfully
- the single-node control plane bootstrapped successfully
- Cilium became healthy
- the smoke pod reached `Ready`

The next work is hardening and repeatability, not first-time feasibility.
