# Sealos Distribution Controller 安装指南

本文说明如何安装当前 `DistributionTarget` controller manifests，并启动一个最小的
controller 驱动 reconcile loop。

## 会安装什么

[`deploy/distribution-controller/base`](../deploy/distribution-controller/base)
里的 base manifests 会安装：

- `distribution.sealos.io/v1alpha1` `DistributionTarget` 和
  `DistributionRolloutPolicy` CRD
- `sealos-system` namespace
- `sealos-distribution-controller` service account、role 和 role binding
- 一个运行 `sealos-agent --controller` 的 deployment

controller 会 watch `sealos-system` 里的 `DistributionTarget` 对象，把每个 target
映射成一次现有 agent reconcile pass，并写入 `Ready` 和 `Degraded` status
condition。target 可以通过 `spec.rolloutPolicyRef` 引用同 namespace 下的
`DistributionRolloutPolicy`；policy 更新后会重新 enqueue 引用它的 targets。

service account RBAC 只覆盖被 watch 的 API、status 更新、leader election leases 和
events。rendered bundle apply 仍然使用 `spec.kubeconfigPath` 或 deployment 默认值选择的
kubeconfig；base deployment 里默认是 `/host/etc/kubernetes/admin.conf`。

## 前置条件

- 已经有 Kubernetes 集群，并且集群可以拉取或预加载 controller 镜像。
- controller 镜像里包含 `/usr/bin/sealos-agent`。tagged release 会发布
  `ghcr.io/<owner>/sealos-agent:<tag>` 多架构镜像；本仓库也提供了一个最小镜像定义：
  [`docker/sealos-agent/Dockerfile`](../docker/sealos-agent/Dockerfile)。base deployment
  会把挂载的 host paths 放到 `PATH` 里，所以 `kubectl` 和 hook tools 可以放进派生镜像，
  也可以由 host 提供。
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

如果使用 tagged release，先用已发布的 controller 镜像渲染 release bundle：

```bash
make render-distribution-controller-bundle \
  DISTRIBUTION_CONTROLLER_IMAGE=ghcr.io/labring/sealos-agent:vNEXT
```

默认会输出到 `dist/distribution-controller/`。安装方式：

```bash
kubectl apply -f dist/distribution-controller/install.yaml
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
```

也可以手动替换 deployment 镜像：

```bash
kubectl -n sealos-system set image \
  -f deploy/distribution-controller/base/deployment.yaml \
  sealos-agent=ghcr.io/labring/sealos-agent:vNEXT \
  --local -o yaml > /tmp/sealos-distribution-controller-deployment.yaml
```

本地开发时，先构建或发布一个包含 `sealos-agent` binary 的镜像，然后在应用
manifests 前替换 deployment 里的镜像：

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

应用 CRD、RBAC、namespace 和替换后的 deployment：

```bash
kubectl apply -f deploy/distribution-controller/base/namespace.yaml
kubectl apply -f deploy/distribution-controller/base/crd.yaml
kubectl wait --for=condition=Established crd/distributiontargets.distribution.sealos.io --timeout=60s
kubectl wait --for=condition=Established crd/distributionrolloutpolicies.distribution.sealos.io --timeout=60s
kubectl apply -f deploy/distribution-controller/base/rbac.yaml
kubectl apply -f /tmp/sealos-distribution-controller-deployment.yaml
```

使用 `-f` 时请逐个应用这些文件。不要执行
`kubectl apply -f deploy/distribution-controller/base` 来应用整个目录；该目录还包含
`kustomization.yaml`，它需要通过 `kubectl apply -k` 渲染。

如果使用 Kustomize，也可以在 base 目录里更新镜像并应用渲染结果：

```bash
cd deploy/distribution-controller/base
kustomize edit set image labring/sealos-agent:dev=example.com/sealos-agent:dev
kubectl apply -k .
```

## 验证安装路径

本地非变更校验可以跑：

```bash
make verify-distribution-controller-manifests
```

如果要在真实集群里跑安装 smoke，需要显式选择 kubeconfig 并打开变更开关。这个流程会在选中的集群里
安装或升级 controller，创建临时 `DistributionTarget` 和 `DistributionRolloutPolicy`，等待 controller
把该 target reconcile 到 `Degraded=True`，然后删除临时 target 和 policy。

```bash
make verify-distribution-controller-real-cluster \
  I_UNDERSTAND_THIS_MUTATES_HOST=1 \
  DISTRIBUTION_CONTROLLER_IMAGE=ghcr.io/labring/sealos-agent:vNEXT \
  DISTRIBUTION_CONTROLLER_SMOKE_ARGS="--kubeconfig ~/.kube/config --artifact-dir /tmp/controller-smoke"
```

如果 smoke 在开始访问集群后失败，脚本会把诊断信息写到指定 artifact 目录：controller
Deployment 和 Pod 的 describe、最近 controller logs、CRD 状态、smoke target/policy YAML，
以及最近的 `sealos-system` resources/events。

仓库还包含 `E2E Distribution Controller` GitHub Actions workflow。PR 和 `main`
push 会自动跑非变更 manifest gate：manifest contract tests、Kustomize render、release
bundle render，以及 smoke script 的本地 render 路径，不访问集群。

同一个 workflow 也可以手动触发真实集群 smoke。手动运行时需要传入已发布的 controller
镜像，并配置一个保存目标 kubeconfig 的 repository secret 或
`distribution-controller-e2e` environment secret。真实集群 job 挂在
`distribution-controller-e2e` environment 上，维护者可以通过 GitHub environment
approval 保护集群访问。每次运行后会上传渲染出来的 bundle 作为短期 artifact；真实集群检查失败时也会上传
smoke diagnostics。

## 升级 Controller

升级已有安装时，先发布新的 controller 镜像，再应用新的 CRD 和 RBAC，最后把 deployment
滚动到新镜像。升级时不要删除 CRD；已有 `DistributionTarget` 和
`DistributionRolloutPolicy` 对象会由 API server 保留。

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

如果继续复用同一个可变镜像 tag，应用 manifest 后重启 deployment，让 pod 按
`imagePullPolicy` 和节点镜像缓存状态重新拉取：

```bash
kubectl -n sealos-system rollout restart deploy/sealos-distribution-controller
kubectl -n sealos-system rollout status deploy/sealos-distribution-controller --timeout=120s
```

## 创建 Distribution Target

先创建示例 targets 引用的 rollout policy：

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-rollout-policy.yaml
```

如果集群要 apply 一个明确的 BOM 文件，使用 pinned BOM target：

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-bom.yaml
```

如果集群要跟随一个本地 channel selection 文件，使用本地 `DistributionChannel` target：

```bash
kubectl apply -f deploy/distribution-controller/examples/distribution-target-channel.yaml
```

controller 要求 `spec.bomPath` 和 `spec.distributionChannelPath` 必须二选一，且不能同时设置。
这两个路径都必须能在 controller pod 内读取。示例 `DistributionRolloutPolicy` 设置了
`spec.strategy.batchSize: 1`、`spec.strategy.canary.batchSize: 1`、
`spec.strategy.pause.afterCanary: true`、`spec.strategy.healthGate: true` 和
`spec.strategy.failureAction: Rollback`。这会让符合条件的 host-targeted steps 一次滚动一个
host，把第一批当作 canary，在进入后续批次前暂停，每批完成后运行该 component 的
`healthcheck` hooks，并在 apply 失败时重新 apply 上一次成功的 rendered revision。
controller target 进入暂停或回滚完成状态后不会通过周期 requeue 继续重试；需要更新 target
或它引用的 rollout policy，例如清掉 `pause.afterCanary` 或选择新的 desired revision，才能继续。
如果 target 没有设置 `spec.rolloutPolicyRef`，仍可使用旧的 inline `spec.rolloutBatchSize`
fallback。

## 检查状态

```bash
kubectl -n sealos-system get distributionrolloutpolicies
kubectl -n sealos-system get distributiontargets
kubectl -n sealos-system describe distributiontarget default-platform
kubectl -n sealos-system logs deploy/sealos-distribution-controller -c sealos-agent
```

成功后，target 会报告 `Ready=True`、`Degraded=False`、解析出来的 BOM revision、
desired state digest 和 applied revision path。

## 当前边界

这只是最小 controller 安装路径。`DistributionRolloutPolicy` 当前持久化的是
rendered-bundle executor 使用的 host rollout batch size、第一批 canary size、可选的
post-canary pause、可选的逐批 health gate，以及 stop-or-rollback failure action。这些设置只作用于符合条件的
all-node runtime-rootfs host batches。pause gate 和 rollback result 都是 operator action hold，
不是按 host 保存的 rollout cursor；继续时会按更新后的 target 或 policy 重新进入符合条件的 apply path。
它还没有加入 registry-backed `DistributionChannel` lookup、controller 驱动的 promotion
automation，也不是覆盖所有 multi-node workflow 的 package 级安全模型。本地 channel 文件可以另外通过
`sealos sync promote` 推进；controller 仍然委托给现有 BOM 驱动的 render/apply agent 路径。
