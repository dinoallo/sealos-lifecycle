# Plan: Minimal Package-Based Kubernetes PoC

## Status

Draft

## Goal

Run a minimal proof of concept that:

1. builds package directories directly on one machine
2. renders them through the new package and BOM flow
3. installs the rendered result on that same machine
4. produces a working single-node Kubernetes cluster

The PoC is intentionally narrow. It is not a general reconcile loop and it is not a production installer.

## Key Decision

For the first PoC, **three packages are enough**:

- one `containerd` package
- one `kubernetes` package
- one `cni` package

That is only true if the following stay **outside** the package scope for now:

- base OS provisioning
- SSH orchestration and multi-node coordination
- upgrades, rollback, and drift management

In other words, the PoC target is:

- one Linux machine
- kernel modules and basic host prerequisites already available

## Recommended PoC Shape

### Cluster Topology

- single-node control plane
- same machine also acts as the worker
- no HA
- no external load balancer

This is the fastest path to prove the package model can bootstrap a real cluster.

### Package Set

Recommended package set:

- `containerd-runtime`
- `kubernetes-rootfs`
- `cilium-cni`

Recommended Cilium profile for PoC v1:

- kube-proxy-compatible mode
- no Hubble
- no cluster mesh
- no encryption
- no BGP or advanced routing features

This keeps the PoC focused on proving package-based bootstrap, not on proving the full Cilium feature set.

## Next Packaging Model After PoC

Once the three-package PoC works, the next packaging model should stay coarse-grained:

- keep `containerd-runtime` as a separate runtime package
- keep Kubernetes node bootstrap assets inside `kubernetes-rootfs`
- add a later `kubernetes-control-plane-patch` package for reusable SRE customization
- add more separate packages only for components with an independent lifecycle, such as CSI or ingress

The important design choice is to avoid splitting core Kubernetes daemons into per-binary packages too early. Most customization pressure belongs in a patch layer or hydrated inputs, not in separate packages for `kubelet`, `kube-apiserver`, or similar components.

## PoC Scope

### In Scope

- package directories authored locally on disk
- one BOM referencing the three local packages
- rendering via `sealos sync render`
- a temporary PoC installer script that consumes the rendered bundle
- validation that Kubernetes API, node readiness, and CNI come up successfully

### Out Of Scope

- OCI build and push pipeline
- multi-node join flow
- long-running reconcile loop
- generic `sync apply`
- promotion, rollback, or drift workflows
- secret management beyond local static files

## Host Assumptions

The target machine should already satisfy:

- Linux with `systemd`
- root access
- `iptables`, `mount`, `modprobe`, `sysctl`, `conntrack`, and `nsenter` available
- swap disabled
- required kernel modules available such as `overlay` and `br_netfilter`
- required sysctls allowed, especially bridge networking and IP forwarding
- BPF filesystem and cgroup mounts available for the chosen Cilium profile

If these assumptions are not true, the PoC should fail fast during preflight rather than trying to own those concerns in v1.

## Package Responsibilities

### 1. `containerd-runtime`

Class:

- `rootfs`

Responsibility:

- deliver the container runtime binaries and default host configuration
- install the `containerd` service and runtime dependencies needed by kubelet
- start and validate the runtime before Kubernetes bootstrap begins

Expected contents:

- `rootfs/`
  - `containerd`
  - `ctr`
  - `containerd-shim-runc-v2`
  - `runc`
  - systemd unit or drop-in files
- `files/etc/containerd/config.toml`
- `hooks/preflight.sh`
- `hooks/bootstrap.sh`
- `hooks/healthcheck.sh`

Expected hook behavior:

- `preflight`
  - verify required cgroup and filesystem prerequisites
  - verify no conflicting runtime service is already bound to the expected socket
- `bootstrap`
  - copy runtime payload into host locations
  - install or refresh the `containerd` unit or drop-ins
  - write default `config.toml`
  - enable and start `containerd`
- `healthcheck`
  - verify the `containerd` socket exists
  - verify the service is active

### 2. `kubernetes-rootfs`

Class:

- `rootfs`

Responsibility:

- deliver Kubernetes node binaries and baseline host files
- install kubelet service defaults
- provide kubeadm configuration defaults
- bootstrap the single-node control plane

Expected contents:

- `rootfs/`
  - `kubeadm`
  - `kubelet`
  - `kubectl`
  - systemd unit or drop-in files
- `files/etc/kubernetes/kubeadm.yaml`
- `files/etc/sysctl.d/99-kubernetes.conf`
- `hooks/preflight.sh`
- `hooks/bootstrap.sh`
- `hooks/healthcheck.sh`

Expected hook behavior:

- `preflight`
  - verify swap is off
  - verify containerd socket exists
  - verify required kernel modules and sysctls
- `bootstrap`
  - copy rootfs payload into host locations
  - install or refresh kubelet unit/drop-ins
  - apply sysctl settings
  - run `kubeadm init --config ...`
  - export admin kubeconfig
- `healthcheck`
  - verify API server is healthy
  - verify node reaches `Ready`

### 3. `cilium-cni`

Class:

- `application`

Responsibility:

- install the pod networking layer after the control plane is up

Expected contents:

- `manifests/cilium.yaml` or `manifests/`
  - namespace
  - service account
  - RBAC
  - config map
  - daemon set
  - operator deployment
- optional `files/values.yaml` if parameterization is needed
- optional `hooks/healthcheck.sh`

Expected behavior:

- install after `kubernetes-rootfs` bootstrap succeeds
- apply manifests with `kubectl apply -f`
- wait until the Cilium daemonset and operator are ready
- keep kube-proxy replacement disabled for PoC v1

## BOM Shape

The BOM should reference exactly three components:

1. `containerd`
2. `kubernetes`
3. `cilium`

Recommended ordering and dependency:

- `kubernetes` depends on `containerd`
- `cilium` depends on `kubernetes`

That lets the existing hydrate plan sort and render them in a sane order.

Example conceptual shape:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: minimal-single-node
spec:
  revision: rev-poc-001
  channel: alpha
  components:
    - name: containerd
      kind: infra
      version: v1.7.18
      artifact:
        name: containerd-runtime
        image: local/poc/containerd-runtime:v1.7.18
        digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
    - name: kubernetes
      kind: infra
      version: v1.30.3
      dependencies:
        - containerd
      artifact:
        name: kubernetes-rootfs
        image: local/poc/kubernetes-rootfs:v1.30.3
        digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
    - name: cilium
      kind: infra
      version: v1.15.0
      dependencies:
        - kubernetes
      artifact:
        name: cilium-cni
        image: local/poc/cilium-cni:v1.15.0
        digest: sha256:3333333333333333333333333333333333333333333333333333333333333333
```

For the PoC, the image and digest fields can be placeholders as long as `sealos sync render` is invoked with local `--package-source` overrides for all three components.

## Recommended Repo Layout

Use repo-local paths under `scripts/` for runnable PoC assets:

```text
scripts/poc/minimal-single-node/
  bom.yaml
  packages/
    containerd/
      package.yaml
      rootfs/
      files/
      hooks/
    kubernetes/
      package.yaml
      rootfs/
      files/
      hooks/
    cilium/
      package.yaml
      manifests/
      hooks/
  inputs/
    kubeadm.yaml
  render.sh
  install.sh
  validate.sh
```

Why this layout:

- `docs/` keeps the plan
- `scripts/poc/` keeps runnable assets together
- package directories remain local and do not require OCI build/push yet

## Execution Plan

### Phase 0: Author The Three Local Packages

Deliverables:

- `scripts/poc/minimal-single-node/packages/containerd/package.yaml`
- `scripts/poc/minimal-single-node/packages/kubernetes/package.yaml`
- `scripts/poc/minimal-single-node/packages/cilium/package.yaml`
- minimal payload files for all three packages

Success criteria:

- all three package directories load through `packageformat.LoadDir`

### Phase 1: Draft The BOM

Deliverables:

- `scripts/poc/minimal-single-node/bom.yaml`

Success criteria:

- BOM validates
- BOM resolves against local package sources

### Phase 2: Render The Desired Bundle

Use the existing local-package override path:

```bash
sealos sync render \
  --file scripts/poc/minimal-single-node/bom.yaml \
  --cluster poc-minimal \
  --package-source containerd=scripts/poc/minimal-single-node/packages/containerd \
  --package-source kubernetes=scripts/poc/minimal-single-node/packages/kubernetes \
  --package-source cilium=scripts/poc/minimal-single-node/packages/cilium
```

Expected output:

- `~/.sealos/poc-minimal/distribution/bundles/current`
- `~/.sealos/poc-minimal/distribution/applied-revision.yaml`

Success criteria:

- render completes without buildah or registry usage
- desired-state digest is produced
- applied revision is persisted

### Phase 3: Implement A Temporary PoC Installer

Add a narrow script:

- `scripts/poc/minimal-single-node/install.sh`

This script should be intentionally specific to the PoC bundle layout, not a generic long-term installer.

Responsibilities:

1. locate the rendered bundle
2. install the containerd runtime payload onto the host
3. execute containerd preflight and bootstrap hooks
4. install the Kubernetes rootfs payload onto the host
5. execute Kubernetes preflight and bootstrap hooks
6. apply Cilium manifests
7. wait for cluster readiness

Suggested install sequence:

1. Verify root privileges.
2. Copy `components/containerd/files/rootfs/*` into `/`.
3. Copy declared runtime config files into their target host paths.
4. Run `components/containerd/files/hooks/preflight.sh`.
5. Run `components/containerd/files/hooks/bootstrap.sh`.
6. Copy `components/kubernetes/files/rootfs/*` into `/`.
7. Copy declared Kubernetes config files into their target host paths.
8. Run `components/kubernetes/files/hooks/preflight.sh`.
9. Run `components/kubernetes/files/hooks/bootstrap.sh`.
10. Export `KUBECONFIG=/etc/kubernetes/admin.conf`.
11. Apply `components/cilium/files/manifests/...`.
12. Wait for node and system pods to become ready.

Success criteria:

- `kubectl get nodes` shows one `Ready` node
- `kubectl -n kube-system get pods` shows control plane and Cilium healthy

### Phase 4: Add Validation Script

Add:

- `scripts/poc/minimal-single-node/validate.sh`

Checks:

- API server health endpoint
- node readiness
- `coredns` readiness
- Cilium daemonset readiness
- Cilium operator readiness
- ability to create a basic test pod

Minimal success command set:

```bash
kubectl get nodes
kubectl get pods -A
kubectl run smoke --image=busybox:1.36 --restart=Never --command -- sleep 30
kubectl wait --for=condition=Ready pod/smoke --timeout=120s
```

## Suggested Milestones

### Milestone 1: Render-Only PoC

Goal:

- prove the three package directories and BOM can render deterministically

Exit criteria:

- `sealos sync render` succeeds from local package sources

### Milestone 2: Single-Node Bootstrap PoC

Goal:

- bootstrap one control-plane node from the rendered bundle

Exit criteria:

- API server is running
- node is `Ready`

### Milestone 3: CNI-Complete PoC

Goal:

- add pod networking and run a test pod

Exit criteria:

- Cilium is healthy
- test pod reaches `Ready`

## Risks

### Risk: Three packages are not actually enough

This is the main architectural risk.

Mitigation:

- keep the runtime package narrowly scoped to `containerd`
- if needed, add a later patch package for runtime or host overlays rather than overloading the Kubernetes package

### Risk: Bootstrap logic becomes too installer-specific

Mitigation:

- keep `install.sh` clearly labeled as PoC-only
- avoid turning it into the final `sync apply` design

### Risk: CNI choice creates unnecessary complexity

Mitigation:

- keep Cilium in kube-proxy-compatible mode
- defer Hubble, encryption, BGP, and kube-proxy replacement

## Recommended Next Implementation Order

1. Create the `containerd-runtime` PoC package directory.
2. Create the `kubernetes-rootfs` PoC package directory.
3. Create the `cilium-cni` PoC package directory.
4. Add the PoC BOM.
5. Verify `sealos sync render` with local package sources.
6. Implement `install.sh`.
7. Implement `validate.sh`.

## Bottom Line

Yes, a minimal PoC can reasonably start with:

- one container runtime package
- one Kubernetes package
- one CNI package

But only if the PoC explicitly assumes:

- single-node target
- runtime-specific packaging remains narrowly scoped
- temporary installer script instead of a generic apply engine

That is the narrowest path that proves the new package model can bootstrap a real cluster without first solving the full reconcile/apply problem.
