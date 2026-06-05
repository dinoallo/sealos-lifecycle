# Sealos Distribution Controller Install Guide

This guide shows how to install the current `DistributionTarget` controller
manifests and start a controller-driven reconcile loop.

## What This Installs

The base manifests in
[`deploy/distribution-controller/base`](../../deploy/distribution-controller/base)
install:

- the `distribution.sealos.io/v1alpha1` `DistributionTarget` and
  `DistributionRolloutPolicy` CRDs
- a `sealos-system` namespace
- the `sealos-distribution-controller` service account, role, and role binding
- a `sealos-agent --controller` deployment

There are two install profiles:

| Profile | Path | Intended use |
| --- | --- | --- |
| `host-agent` | `deploy/distribution-controller/base` | Default development and compatibility profile. It is the current minimal privileged host-mount agent path. |
| `production-host-agent` | `deploy/distribution-controller/overlays/production-host-agent` | Production hardening profile for trusted lifecycle control-plane nodes. It keeps the same privileged host-agent execution model, but adds an explicit node label gate, host tool preflight, resource requests and limits, profile labels, and release-bundle rendering support. |

The controller watches `DistributionTarget` objects in `sealos-system`, maps
each target to one existing agent reconcile pass, and writes an explicit status
state machine: `status.phase`, `Ready` and `Degraded` conditions, retry count,
next retry time, hold reason, last diagnostic, and Kubernetes events. A target
can reference a same-namespace `DistributionRolloutPolicy` through
`spec.rolloutPolicyRef`; policy updates enqueue the referencing targets for
another reconcile pass.

The service account RBAC is scoped to the watched API, status updates, leader
election leases, and events. Rendered bundle apply still uses the kubeconfig
selected by `spec.kubeconfigPath` or the deployment default; in the base
deployment that is `/host/etc/kubernetes/admin.conf`.

## Prerequisites

- A Kubernetes cluster that can pull or preload the controller image.
- A controller image that contains `/usr/bin/sealos-agent`. Tagged releases
  publish `ghcr.io/<owner>/sealos-agent:<tag>` as a multi-arch image, and this
  repository also ships a minimal image definition at
  [`docker/sealos-agent/Dockerfile`](../../docker/sealos-agent/Dockerfile). The
  base deployment puts mounted host paths on `PATH`, so `kubectl` and hook tools
  can either be baked into a derived image or supplied by the host.
- The selected BOM or local `ReleaseChannel` file staged under
  `/var/lib/sealos/distribution/...` on the node running the controller pod.
- A cluster-local repo staged under `/var/lib/sealos/distribution/...` when the
  selected BOM expects local inputs, resources, or patches.

The base deployment mounts:

- `/` from the host at `/host`
- `/var/lib/sealos` from the host at `/var/lib/sealos`
- `/run` from the host at `/run`

Those mounts are intentional. The current agent apply path can mutate host
files and can call host tools while applying rendered bundles. The sample
deployment therefore runs privileged and points `--kubeconfig` at
`/host/etc/kubernetes/admin.conf`. It also requires scheduling on nodes labeled
`node-role.kubernetes.io/control-plane` or `node-role.kubernetes.io/master` and
tolerates the matching `NoSchedule` taints so that the host admin kubeconfig is
present.

For production installs, use the `production-host-agent` profile and label only
the trusted lifecycle node that should run the controller:

```bash
kubectl label node <control-plane-node> sealos.io/distribution-controller=true
```

The production profile runs a `host-tool-preflight` init container before the
controller starts. The preflight requires `kubectl`, `systemctl`, `tar`, `sh`,
`/host/etc/kubernetes/admin.conf`, and `/var/lib/sealos` to be available through
the image or mounted host paths. Keep those dependencies stable across upgrades
and treat changes to the host tool list as part of the controller release
checklist.

## Install The Controller

For a tagged release, render a release bundle with the published controller
image:

```bash
make render-distribution-controller-bundle \
  DISTRIBUTION_CONTROLLER_IMAGE=ghcr.io/labring/sealos-agent:vNEXT
```

The rendered bundle is written to `dist/distribution-controller/` by default.
Install it with:

```bash
kubectl apply -f dist/distribution-controller/install.yaml
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
```

For a production install, render the hardening profile explicitly:

```bash
make render-distribution-controller-bundle \
  DISTRIBUTION_CONTROLLER_IMAGE=ghcr.io/labring/sealos-agent:vNEXT \
  DISTRIBUTION_CONTROLLER_PROFILE=production-host-agent
kubectl apply -f dist/distribution-controller/install.yaml
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
```

You can also set the deployment image manually:

```bash
kubectl -n sealos-system set image \
  -f deploy/distribution-controller/base/deployment.yaml \
  sealos-agent=ghcr.io/labring/sealos-agent:vNEXT \
  --local -o yaml > /tmp/sealos-distribution-controller-deployment.yaml
```

For local development, build or publish an image that contains the
`sealos-agent` binary, then set that image in the deployment before applying the
manifests:

```bash
make build-distribution-controller-image \
  DISTRIBUTION_CONTROLLER_IMAGE=example.com/sealos-agent:dev \
  DISTRIBUTION_CONTROLLER_PUSH_IMAGE=1
```

```bash
kubectl -n sealos-system set image \
  -f deploy/distribution-controller/base/deployment.yaml \
  sealos-agent=example.com/sealos-agent:dev \
  --local -o yaml > /tmp/sealos-distribution-controller-deployment.yaml
```

Apply the CRD, RBAC, namespace, and adjusted deployment:

```bash
kubectl apply -f deploy/distribution-controller/base/namespace.yaml
kubectl apply -f deploy/distribution-controller/base/crd.yaml
kubectl wait --for=condition=Established crd/distributiontargets.distribution.sealos.io --timeout=60s
kubectl wait --for=condition=Established crd/distributionrolloutpolicies.distribution.sealos.io --timeout=60s
kubectl apply -f deploy/distribution-controller/base/rbac.yaml
kubectl apply -f /tmp/sealos-distribution-controller-deployment.yaml
```

Apply those files individually when using `-f`. Do not apply the whole
`deploy/distribution-controller/base` directory with `kubectl apply -f`; it also
contains `kustomization.yaml`, which must be rendered with `kubectl apply -k`.

If you are using Kustomize, you can also update the image from the base
directory and apply the rendered output:

```bash
cd deploy/distribution-controller/base
kustomize edit set image labring/sealos-agent:dev=example.com/sealos-agent:dev
kubectl apply -k .
```

## Verify The Install Path

Use the non-mutating manifest gate for local validation:

```bash
make verify-distribution-controller-manifests
```

To run the real-cluster install smoke, select a kubeconfig and opt in
explicitly. This installs or upgrades the controller in the selected cluster,
creates a temporary `DistributionTarget` and `DistributionRolloutPolicy`, waits
for the controller to reconcile that target into `Degraded=True`, and then
removes the temporary target and policy.

```bash
make verify-distribution-controller-real-cluster \
  I_UNDERSTAND_THIS_MUTATES_HOST=1 \
  DISTRIBUTION_CONTROLLER_IMAGE=ghcr.io/labring/sealos-agent:vNEXT \
  DISTRIBUTION_CONTROLLER_SMOKE_ARGS="--kubeconfig ~/.kube/config --artifact-dir /tmp/controller-smoke"
```

To smoke the production profile, add
`DISTRIBUTION_CONTROLLER_PROFILE=production-host-agent` after the target node
has the `sealos.io/distribution-controller=true` label and the host tool
preflight dependencies are present.

When the smoke fails after cluster access begins, the script writes diagnostics
under the requested artifact directory: controller Deployment and Pod
descriptions, recent controller logs, CRD state, smoke target/policy YAML, and
recent `sealos-system` resources/events.

The repository also includes the `E2E Distribution Controller` GitHub Actions
workflow. Pull requests and `main` pushes run the non-mutating manifest gate:
manifest contract tests, Kustomize rendering, release bundle rendering, and the
smoke script's local render path without cluster access.

The same workflow can be triggered manually for real-cluster smoke. Manual runs
expect a published controller image and a repository or
`distribution-controller-e2e` environment secret containing the target
kubeconfig. The real-cluster job is attached to the
`distribution-controller-e2e` environment so repository maintainers can protect
cluster access with GitHub environment approvals. It uploads the rendered bundle
as a short-lived artifact after each run, and uploads smoke diagnostics when the
real-cluster check fails.

## Upgrade The Controller

To upgrade an existing installation, publish the new controller image, apply the
new CRDs and RBAC, then roll the deployment to the new image. Do not delete the
CRDs during upgrade; existing `DistributionTarget` and `DistributionRolloutPolicy`
objects are kept by the API server.

```bash
kubectl apply -f deploy/distribution-controller/base/crd.yaml
kubectl wait --for=condition=Established crd/distributiontargets.distribution.sealos.io --timeout=60s
kubectl wait --for=condition=Established crd/distributionrolloutpolicies.distribution.sealos.io --timeout=60s
kubectl apply -f deploy/distribution-controller/base/rbac.yaml
kubectl -n sealos-system set image \
  -f deploy/distribution-controller/base/deployment.yaml \
  sealos-agent=example.com/sealos-agent:vNEXT \
  --local -o yaml > /tmp/sealos-distribution-controller-deployment.yaml
kubectl apply -f /tmp/sealos-distribution-controller-deployment.yaml
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
```

For production profile upgrades, render the new release bundle with the same
profile and apply the rendered files in the same order. Verify that the
`sealos.io/distribution-controller=true` node label still points at the intended
control-plane node before rolling the Deployment:

```bash
make render-distribution-controller-bundle \
  DISTRIBUTION_CONTROLLER_IMAGE=ghcr.io/labring/sealos-agent:vNEXT \
  DISTRIBUTION_CONTROLLER_PROFILE=production-host-agent
kubectl apply -f dist/distribution-controller/crd.yaml
kubectl wait --for=condition=Established crd/distributiontargets.distribution.sealos.io --timeout=60s
kubectl wait --for=condition=Established crd/distributionrolloutpolicies.distribution.sealos.io --timeout=60s
kubectl apply -f dist/distribution-controller/rbac.yaml
kubectl apply -f dist/distribution-controller/deployment.template.yaml
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
```

If you reuse the same mutable image tag, restart the deployment after applying
the manifest so the pod pulls according to its `imagePullPolicy` and node cache
state:

```bash
kubectl -n sealos-system rollout restart deploy/sealos-distribution-controller
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
```

## Create A Distribution Target

Create the rollout policy referenced by the examples:

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-rollout-policy.yaml
```

Use a pinned BOM target when the cluster should apply one explicit BOM file:

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-bom.yaml
```

Use a local `ReleaseChannel` target when the cluster should follow a local
channel selection file:

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-channel.yaml
```

Use a release metadata source target when the cluster should follow a channel
resolved by distribution line and channel:

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-lookup.yaml
```

The controller requires exactly one target selector: `spec.bomPath`,
`spec.releaseChannelPath`, or the triple `spec.releaseSource`,
`spec.releaseLine`, and `spec.channel`. Local paths must be readable inside the
controller pod. HTTP(S) release sources are resolved through
`/v1/distributions/{line}/channels/{channel}` and must return a
digest-pinned `ReleaseChannel` target. The sample `DistributionRolloutPolicy` sets
`spec.strategy.batchSize: 1`, `spec.strategy.canary.batchSize: 1`,
`spec.strategy.pause.afterCanary: true`, `spec.strategy.healthGate: true`, and
`spec.strategy.failureAction: Rollback`. That rolls eligible host-targeted
steps one host at a time, treats the first host batch as the canary, pauses
before later batches, runs component `healthcheck` hooks after each batch, and
re-applies the last successful rendered revision if apply fails. Paused and
rolled-back controller targets do not use periodic requeue; update the target
or its referenced rollout policy, for example by clearing `pause.afterCanary`
or selecting a new desired revision, to continue. If a target does not set
`spec.rolloutPolicyRef`, it can still use the older inline
`spec.rolloutBatchSize` fallback.

## Check Status

```bash
kubectl -n sealos-system get distributionrolloutpolicies
kubectl -n sealos-system get distributiontargets
kubectl -n sealos-system describe distributiontarget default-platform
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent
```

On success, the target reports `phase=Succeeded`, `Ready=True`,
`Degraded=False`, the resolved BOM revision, the desired state digest, and the
applied revision path. Failed targets report `phase=Retrying` when
`spec.retryBackoff` schedules another reconcile, `phase=PartiallyFailed` when
the agent returned a result plus an error, `phase=Paused` for post-canary
operator holds, or `phase=RollbackHold` after rollback to the last successful
revision. `kubectl describe distributiontarget ...` also shows the emitted
events for the latest transition.

## Current Boundaries

The controller has a durable reconcile state machine for each target, but the
rollout execution unit is still the rendered bundle. `DistributionRolloutPolicy`
currently persists host rollout batch size, a first-batch canary size, an
optional post-canary pause, an optional per-batch health gate, and a
stop-or-rollback failure action used by the rendered-bundle executor. These
settings only apply to eligible host batches, while `sync plan` reports package
and phase safety profiles for rootfs, host-file, manifest, chart, patch,
values, package hook phases, local patch approval, and generated host
projections. The pause gate and rollback result are operator action holds, not
per-host rollout cursors; continuing re-enters the eligible apply path with an
updated target or policy. It does not add controller-driven promotion
automation or durable per-package rollout cursors.
Local channel files can be advanced separately with `sealos sync promote`; the
controller still delegates to the existing BOM-driven render/apply agent path.

The controller RBAC intentionally stays namespace-scoped to `sealos-system`: it
reads `DistributionTarget` and `DistributionRolloutPolicy`, updates
`DistributionTarget/status`, writes leader-election leases, and emits events.
Kubernetes object apply privileges come from the kubeconfig selected by the
target or deployment default, not from the controller service account RBAC.
