# Day 0 Install Workflow

## Status

Current implementation guide with product-direction notes.

This guide describes how an operator should take Sealos Distribution from no
cluster to one installed Sealos cluster. The current repository has a verified
minimal single-node prepared-host path. Multi-node Day 0 bootstrap,
registry/API-backed release lookup, and fully productized release assets are
still being hardened.

## Mental Model

Day 0 install is not "run every package by hand". The operator selects a
release target, renders that target into a cluster-local desired-state bundle,
then applies the bundle.

The objects in that path are:

- `ComponentPackage`: one installable component package, usually published as
  an OCI image pinned by digest.
- `BOM`: one immutable release snapshot that selects exact component package
  artifacts.
- `ReleaseChannel`: a mutable release pointer that resolves a distribution
  line and channel to one BOM revision. The current implementation supports
  local `ReleaseChannel` files.
- `LocalRepo`: cluster-local inputs, resources, patches, and policy.
- rendered bundle: the desired state produced by `sealos sync render`.
- `AppliedRevision`: cluster-local state recording the rendered/applied target.

## Product Intended Flow

For a normal installer, the package payloads should already be published by a
release build. The user should consume a digest-pinned BOM or a `ReleaseChannel`
that resolves to such a BOM.

In that final shape, an operator should not run
`scripts/poc/minimal-single-node/stage-assets.sh`. That script belongs to the
PoC release-build side because it turns package templates into real package
payloads by inserting runtime, Kubernetes, and Cilium assets.

The target product flow is:

1. choose `distribution line + channel`, or choose one explicit BOM revision
2. initialize/fill the cluster-local repo if the BOM requires local inputs
3. validate source inputs
4. render the bundle
5. run apply preflight
6. apply
7. verify status and health

## Prerequisites

For the current single-node install path, the host must be a prepared Linux
host or VM:

- `systemd` is PID 1
- root access is available for `sync apply`
- swap is disabled
- kernel support and modules are available for the chosen CNI/runtime profile
- required host commands are installed, including `systemctl`, `modprobe`,
  `sysctl`, `conntrack`, `crictl`, `socat`, and `curl`
- the selected BOM or local `ReleaseChannel` points at real package payloads,
  not placeholder package templates

Build `sealos` from this repository when working from a checkout:

```bash
make build BINS=sealos
SEALOS="$(pwd)/bin/linux_amd64/sealos"
```

For a release binary, use the installed `sealos`:

```bash
SEALOS="$(command -v sealos)"
```

Use one shared runtime root for render, apply, status, and drift commands. This
avoids splitting state between a non-root user's `${HOME}/.sealos` and root's
`${HOME}/.sealos` during host bootstrap.

```bash
RUNTIME_ROOT=/var/lib/sealos/runtime
sudo install -d -m 0755 "$RUNTIME_ROOT"
```

## Choose The Target

Use an explicit BOM when the cluster should pin exactly one revision:

```bash
CLUSTER=poc-minimal
TARGET_BOM=/var/lib/sealos/distribution/releases/default-platform/rev-007/bom.yaml
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

Use a local `ReleaseChannel` file when the cluster should follow a channel:

```bash
CLUSTER=poc-minimal
TARGET_CHANNEL=/var/lib/sealos/distribution/channels/default-platform/stable.yaml
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

Use a release metadata source when the cluster should resolve a channel from
the release service:

```bash
CLUSTER=poc-minimal
RELEASE_SOURCE=https://release.sealos.example
RELEASE_LINE=default-platform
RELEASE_CHANNEL=stable
LOCAL_REPO=/var/lib/sealos/distribution/${CLUSTER}/local-repo
KUBECONFIG=/etc/kubernetes/admin.conf
BUNDLE="${RUNTIME_ROOT}/${CLUSTER}/distribution/bundles/current"
```

In the examples below, use exactly one target selector:

- `--file "$TARGET_BOM"`
- `--release-channel "$TARGET_CHANNEL"`
- `--release-source "$RELEASE_SOURCE" --release-line "$RELEASE_LINE" --channel "$RELEASE_CHANNEL"`

## Install With An Explicit BOM

Initialize the local repo skeleton:

```bash
sudo $SEALOS sync local-repo init \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --output-dir "$LOCAL_REPO"
```

Fill any generated placeholders in the local repo. These are cluster-local
inputs and resources required by the selected packages.

Inspect the local repo:

```bash
sudo $SEALOS sync local-repo doctor \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --local-repo "$LOCAL_REPO"
```

Validate the complete source side:

```bash
sudo $SEALOS sync validate \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --local-repo "$LOCAL_REPO"
```

Render the desired-state bundle:

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --file "$TARGET_BOM" \
  --local-repo "$LOCAL_REPO"
```

Run apply preflight:

```bash
sudo $SEALOS sync preflight \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"
```

Apply the bundle:

```bash
sudo $SEALOS sync apply \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"
```

Check the installed cluster:

```bash
kubectl --kubeconfig "$KUBECONFIG" get nodes
kubectl --kubeconfig "$KUBECONFIG" get pods -A
sudo $SEALOS sync status \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER"
```

## Install With A ReleaseChannel

Use the same flow, replacing `--file "$TARGET_BOM"` with
`--release-channel "$TARGET_CHANNEL"` in `local-repo init`,
`local-repo doctor`, `validate`, and `render`. For release-service lookup,
replace the target flags with
`--release-source "$RELEASE_SOURCE" --release-line "$RELEASE_LINE" --channel "$RELEASE_CHANNEL"`:

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-channel "$TARGET_CHANNEL" \
  --local-repo "$LOCAL_REPO"
```

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"
```

The release metadata source must return a `ReleaseChannel` document for
`/v1/distributions/{line}/channels/{channel}`. The resolved channel must point
at a BOM with `spec.bomDigest`, and Sealos verifies the fetched BOM digest
before render.

## Scriptless Minimal PoC

The repository PoC should now be exercised with the same 0-to-1 operator flow
described above. The PoC install path does not call helper scripts such as
`fetch-assets.sh`, `stage-assets.sh`, `publish-oci.sh`, `render.sh`, or
`bootstrap.sh`.

Those helpers may still exist for CI fixture generation and release-build
experiments because this repository does not commit large runtime, Kubernetes,
or Cilium payloads. They are not the operator-facing PoC.

Start from a release target whose component packages are already published and
whose BOM is digest-pinned:

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

For a local filesystem release source, verify that the channel metadata exists:

```bash
test -f "${RELEASE_SOURCE}/channels/${RELEASE_LINE}/${RELEASE_CHANNEL}.yaml"
```

Initialize the cluster-local repo from that target:

```bash
sudo $SEALOS sync local-repo init \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --output-dir "$LOCAL_REPO" \
  --overwrite
```

For the current in-repo PoC defaults, fill the generated inputs from the tracked
package default files:

```bash
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

Then run the guide flow directly:

```bash
sudo $SEALOS sync local-repo doctor \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo $SEALOS sync validate \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-source "$RELEASE_SOURCE" \
  --release-line "$RELEASE_LINE" \
  --channel "$RELEASE_CHANNEL" \
  --local-repo "$LOCAL_REPO"

sudo $SEALOS sync preflight \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"

sudo $SEALOS sync apply \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --bundle-dir "$BUNDLE" \
  --kubeconfig "$KUBECONFIG"
```

Validate with normal cluster and distribution commands:

```bash
kubectl --kubeconfig "$KUBECONFIG" get nodes -o wide
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status ds/cilium --timeout=180s
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status deploy/cilium-operator --timeout=180s
kubectl --kubeconfig "$KUBECONFIG" -n kube-system rollout status deploy/coredns --timeout=180s
sudo $SEALOS sync status \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER"
```

For a multi-node PoC, use the same commands with a cluster name that already has
a Sealos `Clusterfile` and SSH inventory. `sync render`, `sync plan`,
`sync preflight`, and `sync apply` resolve `allNodes`, `firstMaster`, and
cluster-scoped targets from that cluster state; no PoC wrapper is needed.

For the safe multi-node Day 0 acceptance gate:

```bash
make verify-day0-multinode-acceptance
```

That gate renders the PoC package set against a three-node inventory, checks
`allNodes`, `firstMaster`, and `cluster` target resolution in `sync plan`, and
runs fake-remote reconcile coverage for kubeadm join config generation, remote
first-master kubeconfig fetches, and multi-node execution targeting. It also
keeps Cilium in the rendered package set so the application/CNI package remains
part of Day 0 acceptance. The GitHub workflow
`.github/workflows/day0_multi_node_acceptance.yml` runs the same safe gate
without mutating hosts.

That gate is repository validation. It is safe and does not mutate hosts.

For a non-mutating repository check of the scriptless guide path, provide an
existing release source and run:

```bash
make verify-day0-guide-render \
  DAY0_RELEASE_SOURCE=/var/lib/sealos/distribution/release-source \
  DAY0_RELEASE_LINE=minimal-single-node \
  DAY0_RELEASE_CHANNEL=alpha \
  DAY0_CLUSTER=poc-minimal-ci
```

This target runs `local-repo init`, fills the stock PoC local inputs, runs
`local-repo doctor`, `validate`, and `render`, and then checks that the bundle
and applied-revision files were written. It does not fetch assets, publish OCI
packages, or apply to a host.

## Package Set Boundary

The current Day 0 PoC release set is intentionally limited to the installable
cluster baseline:

| Package | Owner | Required Local Input | Health Check |
| --- | --- | --- | --- |
| `containerd-runtime` | node runtime platform owner | `containerd-config` | runtime service and local runtime tooling report healthy |
| `kubernetes-rootfs` | cluster platform owner | `kubeadm-cluster-config` | kube-apiserver is reachable, nodes register, and bootstrap manifests apply |
| `cilium-cni` | network platform owner | `cilium-values` | Cilium DaemonSet and operator rollouts complete |

The next package-set expansion is a product contract, not part of the current
PoC BOM yet:

- `kubernetes-control-plane-patch`: SRE-owned hardening overlays with
  policy/admission/static-pod inputs and API/static-Pod projection healthcheck
- `csi-driver-*`: storage-owned addon with backend Secret refs,
  topology/storage-class inputs, controller/node healthchecks, and data-plane
  protection notes
- `ingress-controller-*`: network/edge-owned addon with ingress class,
  exposure/TLS/load-balancer inputs, and route/webhook healthchecks
- `observability-stack`: observability-owned addon with retention, storage,
  external endpoint inputs, and collector/dashboard/alert healthchecks

Do not add these packages to Day 0 until the package directory, local repo
templates, healthcheck hook, acceptance evidence, and rollback/reset boundary
are present.

## Repeat-Run Cleanup

The scriptless PoC has a cleanup entrypoint for state that is safe to regenerate:
rendered bundle state, cluster-local repo content, temporary workdirs, and
optional remote staged bundle mirrors. The default cleanup path does not remove
Kubernetes, CRI, kubelet, containerd, `Clusterfile`, `admin.conf`, or host data:

```bash
make cleanup-day0-poc \
  DAY0_CLEANUP_ARGS="--cluster poc-minimal \
    --runtime-root /var/lib/sealos/runtime \
    --distribution-root /var/lib/sealos/distribution"
```

For multi-node reruns where `sync apply` has copied staged bundle mirrors to
remote hosts, add `--remote-staged` only when the cluster has a default-runtime
`Clusterfile` that `sealos exec -c <cluster>` can use:

```bash
make cleanup-day0-poc \
  DAY0_CLEANUP_ARGS="--cluster sealos-distribution-test --remote-staged"
```

Resetting Kubernetes/CRI state is a separate destructive operation. It is never
part of the default cleanup path. Use it only on disposable PoC hosts:

```bash
I_UNDERSTAND_THIS_MUTATES_HOST=1 make reset-day0-poc \
  DAY0_CLEANUP_ARGS="--cluster poc-minimal"
```

For scriptless installs that use `--runtime-root /var/lib/sealos/runtime`,
prefer the safe cleanup target before rerendering. Use `reset-day0-poc` only
when the target cluster also exists in the default Sealos runtime root used by
`sealos reset`, because `sealos reset` does not accept `--runtime-root`.

## Development-Only Local Package Flow

When iterating on package directories in-tree, developers may bypass published
OCI packages with `--package-source` overrides:

```bash
$SEALOS sync render \
  --cluster poc-minimal \
  --file scripts/poc/minimal-single-node/bom.yaml \
  --package-source containerd=scripts/poc/minimal-single-node/packages/containerd \
  --package-source kubernetes=scripts/poc/minimal-single-node/packages/kubernetes \
  --package-source cilium=scripts/poc/minimal-single-node/packages/cilium
```

This is a renderer development path, not the 0-to-1 PoC install path. It is
installable only when those local package directories already contain full real
payloads. Ordinary installers should consume already-published, digest-pinned
packages through a BOM or `ReleaseChannel`.

The product does not expose a package-direct install path. Commands such as
`sealos sync package pull` or `--package-source` are package authoring and
renderer development tools. They do not replace the Day 0 release contract:
operators still select a BOM or `ReleaseChannel`, render a bundle, run
preflight, and apply that bundle. Keeping install execution behind the
BOM/bundle boundary is what preserves dependency ordering, local input binding,
render provenance, drift ownership, and rollback history.

## Completion Criteria

Day 0 is complete when:

- `sync apply` succeeds
- `/etc/kubernetes/admin.conf` exists
- `kubectl get nodes` shows the expected node set as `Ready`
- cluster critical workloads, including the selected CNI, are healthy
- `sealos sync status --cluster <name>` reports the expected BOM revision and
  desired state

## Current Boundaries

- The operator-facing PoC assumes release assets already exist. Package assembly
  belongs to release build/publish automation.
- Helper scripts remain in the repository for CI fixture generation and package
  development, but they are no longer the PoC install interface.
- Multi-node Day 0 has CLI-driven render/plan/preflight/apply support; the
  default GitHub gate remains non-mutating unless a protected environment is
  used for real hosts.
