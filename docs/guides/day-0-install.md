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

In the examples below, use either `--file "$TARGET_BOM"` or
`--release-channel "$TARGET_CHANNEL"`. Do not pass both.

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
`local-repo doctor`, `validate`, and `render`:

```bash
sudo $SEALOS sync render \
  --runtime-root "$RUNTIME_ROOT" \
  --cluster "$CLUSTER" \
  --release-channel "$TARGET_CHANNEL" \
  --local-repo "$LOCAL_REPO"
```

The current resolver only reads a local `ReleaseChannel` file. It does not yet
perform registry/API-backed lookup by distribution line and channel.

## Current PoC Shortcut

For the repository's minimal single-node PoC, the convenience wrapper is:

```bash
sudo scripts/poc/minimal-single-node/bootstrap.sh \
  --cluster poc-minimal
```

That wrapper:

1. builds `sealos`
2. starts a temporary local registry
3. publishes the three PoC packages as OCI package images
4. renders from the generated OCI-backed BOM
5. runs `sealos sync apply`
6. runs the PoC validator

This shortcut is useful for repository development and validation. It is not the
intended long-term user-facing install interface.

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

For that development path, package templates must first be filled with real
assets. That is why the PoC has:

```bash
scripts/poc/minimal-single-node/stage-assets.sh \
  --kubelet-bin /usr/bin/kubelet \
  --cilium-manifest /absolute/path/to/cilium.yaml
```

This should be treated as release-builder or developer work. Ordinary installers
should consume already-published, digest-pinned packages.

## Completion Criteria

Day 0 is complete when:

- `sync apply` succeeds
- `/etc/kubernetes/admin.conf` exists
- `kubectl get nodes` shows the expected node set as `Ready`
- cluster critical workloads, including the selected CNI, are healthy
- `sealos sync status --cluster <name>` reports the expected BOM revision and
  desired state

## Current Boundaries

- The local `ReleaseChannel` resolver is file-backed only.
- The minimal PoC is single-node and prepared-host oriented.
- `stage-assets.sh` exists because this repository does not commit large
  runtime/Kubernetes/Cilium payloads as release artifacts.
- A productized install should move package assembly to release build/publish
  automation so users only select a target and run validate/render/apply.
