# Sealos Distribution Controller Install Guide

This guide shows how to install the current `DistributionTarget` controller
manifests and start a minimal controller-driven reconcile loop.

## What This Installs

The base manifests in
[`deploy/distribution-controller/base`](../deploy/distribution-controller/base)
install:

- the `distribution.sealos.io/v1alpha1` `DistributionTarget` CRD
- a `sealos-system` namespace
- the `sealos-distribution-controller` service account, role, and role binding
- a `sealos-agent --controller` deployment

The controller watches `DistributionTarget` objects in `sealos-system`, maps
each target to one existing agent reconcile pass, and writes `Ready` and
`Degraded` status conditions.

The service account RBAC is scoped to the watched API, status updates, leader
election leases, and events. Rendered bundle apply still uses the kubeconfig
selected by `spec.kubeconfigPath` or the deployment default; in the base
deployment that is `/host/etc/kubernetes/admin.conf`.

## Prerequisites

- A Kubernetes cluster that already has the `sealos-agent` image available.
- A controller image that contains `/usr/bin/sealos-agent`, `kubectl`, and the
  host tools needed by package hooks, or an image plus mounted host paths that
  put those tools on `PATH`.
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
controller pod.

## Check Status

```bash
kubectl -n sealos-system get distributiontargets
kubectl -n sealos-system describe distributiontarget default-platform
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent
```

On success, the target reports `Ready=True`, `Degraded=False`, the resolved BOM
revision, the desired state digest, and the applied revision path.

## Current Boundaries

This is a minimal controller install path. It does not add registry-backed
`DistributionChannel` lookup, promotion automation, canary pauses, health-gated
rollouts, or automatic rollback. The controller still delegates to the existing
BOM-driven render/apply agent path.
