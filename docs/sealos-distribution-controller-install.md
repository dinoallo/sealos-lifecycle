# Sealos Distribution Controller Install Guide

This guide shows how to install the current `DistributionTarget` controller
manifests and start a minimal controller-driven reconcile loop.

## What This Installs

The base manifests in
[`deploy/distribution-controller/base`](../deploy/distribution-controller/base)
install:

- the `distribution.sealos.io/v1alpha1` `DistributionTarget` and
  `DistributionRolloutPolicy` CRDs
- a `sealos-system` namespace
- the `sealos-distribution-controller` service account, role, and role binding
- a `sealos-agent --controller` deployment

The controller watches `DistributionTarget` objects in `sealos-system`, maps
each target to one existing agent reconcile pass, and writes `Ready` and
`Degraded` status conditions. A target can reference a same-namespace
`DistributionRolloutPolicy` through `spec.rolloutPolicyRef`; policy updates
enqueue the referencing targets for another reconcile pass.

The service account RBAC is scoped to the watched API, status updates, leader
election leases, and events. Rendered bundle apply still uses the kubeconfig
selected by `spec.kubeconfigPath` or the deployment default; in the base
deployment that is `/host/etc/kubernetes/admin.conf`.

## Prerequisites

- A Kubernetes cluster that can pull or preload the controller image.
- A controller image that contains `/usr/bin/sealos-agent`. This repository
  ships a minimal image definition at
  [`docker/sealos-agent/Dockerfile`](../docker/sealos-agent/Dockerfile). The
  base deployment also puts mounted host paths on `PATH`, so `kubectl` and hook
  tools can either be baked into a derived image or supplied by the host.
- The selected BOM or local `DistributionChannel` file staged under
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

## Install The Controller

Build or publish an image that contains the `sealos-agent` binary, then set that
image in the deployment before applying the manifests:

```bash
PLATFORM=linux_$(go env GOARCH)
make build BINS=sealos-agent PLATFORM="${PLATFORM}"
cp "bin/${PLATFORM}/sealos-agent" docker/sealos-agent/sealos-agent
docker build -t example.com/sealos-agent:dev docker/sealos-agent
docker push example.com/sealos-agent:dev
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

## Create A Distribution Target

Create the rollout policy referenced by the examples:

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-rollout-policy.yaml
```

Use a pinned BOM target when the cluster should apply one explicit BOM file:

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-bom.yaml
```

Use a local `DistributionChannel` target when the cluster should follow a local
channel selection file:

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-channel.yaml
```

The controller requires exactly one of `spec.bomPath` or
`spec.distributionChannelPath`. Both paths must be readable inside the
controller pod. The sample `DistributionRolloutPolicy` sets
`spec.strategy.batchSize: 1`, `spec.strategy.canary.batchSize: 1`,
`spec.strategy.pause.afterCanary: true`, `spec.strategy.healthGate: true`, and
`spec.strategy.failureAction: Rollback`. That rolls eligible host-targeted
steps one host at a time, treats the first host batch as the canary, pauses
before later batches, runs component `healthcheck` hooks after each batch, and
re-applies the last successful rendered revision if apply fails. If a target
does not set `spec.rolloutPolicyRef`, it can still use the older inline
`spec.rolloutBatchSize` fallback.

## Check Status

```bash
kubectl -n sealos-system get distributionrolloutpolicies
kubectl -n sealos-system get distributiontargets
kubectl -n sealos-system describe distributiontarget default-platform
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent
```

On success, the target reports `Ready=True`, `Degraded=False`, the resolved BOM
revision, the desired state digest, and the applied revision path.

## Current Boundaries

This is a minimal controller install path. `DistributionRolloutPolicy` currently
persists host rollout batch size, a first-batch canary size, an optional
post-canary pause, an optional per-batch health gate, and a stop-or-rollback
failure action used by the rendered-bundle executor. These settings only apply
to eligible all-node runtime-rootfs host batches. It does not add
registry-backed `DistributionChannel` lookup, health-gated channel promotion, or
a package-level safety model for every multi-node workflow. The controller still
delegates to the existing BOM-driven render/apply agent path.
