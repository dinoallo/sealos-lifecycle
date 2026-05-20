# Sealos Distribution Controller 安装指南

本文说明如何安装当前 `DistributionTarget` controller manifests，并启动一个最小的
controller 驱动 reconcile loop。

## 会安装什么

[`deploy/distribution-controller/base`](../deploy/distribution-controller/base)
里的 base manifests 会安装：

- `distribution.sealos.io/v1alpha1` `DistributionTarget` CRD
- `sealos-system` namespace
- `sealos-distribution-controller` service account、role 和 role binding
- 一个运行 `sealos-agent --controller` 的 deployment

controller 会 watch `sealos-system` 里的 `DistributionTarget` 对象，把每个 target
映射成一次现有 agent reconcile pass，并写入 `Ready` 和 `Degraded` status
condition。

service account RBAC 只覆盖被 watch 的 API、status 更新、leader election leases 和
events。rendered bundle apply 仍然使用 `spec.kubeconfigPath` 或 deployment 默认值选择的
kubeconfig；base deployment 里默认是 `/host/etc/kubernetes/admin.conf`。

## 前置条件

- 已经有 Kubernetes 集群，并且集群可以拉取或使用 `sealos-agent` 镜像。
- controller 镜像里包含 `sealos-agent`、`kubectl` 和 package hooks 需要的 host
  tools；或者镜像加上挂载的 host paths 后，可以从 `PATH` 找到这些工具。
- 选中的 BOM 或本地 `DistributionChannel` 文件已经放到运行 controller pod 的节点的
  `/var/lib/sealos/distribution/...` 下。
- 如果选中的 BOM 需要 local inputs、resources 或 patches，cluster-local repo 也要放到
  `/var/lib/sealos/distribution/...` 下。

base deployment 会挂载：

- host 的 `/` 到 `/host`
- host 的 `/var/lib/sealos` 到 `/var/lib/sealos`
- host 的 `/run` 到 `/run`

这些挂载是有意设计的。当前 agent apply 路径可能会修改 host 文件，也可能会在 apply
rendered bundle 时调用 host tools。因此示例 deployment 使用 privileged 模式，并把
`--kubeconfig` 指向 `/host/etc/kubernetes/admin.conf`。它还要求调度到带有
`node-role.kubernetes.io/control-plane` 或 `node-role.kubernetes.io/master` label 的节点，
并容忍对应的 `NoSchedule` taints，确保 host admin kubeconfig 存在。

## 安装 Controller

先构建或发布一个包含 `sealos-agent` binary 的镜像，然后在应用 manifests 前替换
deployment 里的镜像：

```bash
kubectl -n sealos-system set image \
  -f deploy/distribution-controller/base/deployment.yaml \
  sealos-agent=example.com/sealos-agent:dev \
  --local -o yaml > /tmp/sealos-distribution-controller-deployment.yaml
```

应用 CRD、RBAC、namespace 和替换后的 deployment：

```bash
kubectl apply -f deploy/distribution-controller/base/namespace.yaml
kubectl apply -f deploy/distribution-controller/base/crd.yaml
kubectl wait --for=condition=Established crd/distributiontargets.distribution.sealos.io --timeout=60s
kubectl apply -f deploy/distribution-controller/base/rbac.yaml
kubectl apply -f /tmp/sealos-distribution-controller-deployment.yaml
```

如果使用 Kustomize，也可以在 base 目录里更新镜像并应用渲染结果：

```bash
cd deploy/distribution-controller/base
kustomize edit set image labring/sealos-agent:dev=example.com/sealos-agent:dev
kubectl apply -k .
```

## 创建 Distribution Target

如果集群要 apply 一个明确的 BOM 文件，使用 pinned BOM target：

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-bom.yaml
```

如果集群要跟随一个本地 channel selection 文件，使用本地 `DistributionChannel` target：

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-channel.yaml
```

controller 要求 `spec.bomPath` 和 `spec.distributionChannelPath` 必须二选一，且不能同时设置。
这两个路径都必须能在 controller pod 内读取。

## 检查状态

```bash
kubectl -n sealos-system get distributiontargets
kubectl -n sealos-system describe distributiontarget default-platform
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent
```

成功后，target 会报告 `Ready=True`、`Degraded=False`、解析出来的 BOM revision、
desired state digest 和 applied revision path。

## 当前边界

这只是最小 controller 安装路径。它还没有加入 registry-backed `DistributionChannel`
lookup、promotion automation、canary pause、带 health gate 的 rollout 或自动 rollback。
controller 仍然委托给现有 BOM 驱动的 render/apply agent 路径。
