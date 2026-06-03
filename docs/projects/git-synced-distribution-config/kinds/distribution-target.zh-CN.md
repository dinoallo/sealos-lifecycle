# Kind: DistributionTarget

## 状态

已实现的 Kubernetes CRD。

## 类别

运行时 reconciliation API。

## 维护方

Cluster operator 创建或更新该 CRD。Distribution controller 负责 reconcile。

## Kubernetes API

- Group: `distribution.sealos.io`
- Version: `v1alpha1`
- Kind: `DistributionTarget`
- Scope: Namespaced
- Short name: `disttarget`

## 用途

`DistributionTarget` 告诉 distribution controller 应为运行时目标 reconcile 哪个 BOM 或 release channel path。它是源侧 target intent 在 Kubernetes API 中的对应物。

它不同于 `ClusterTarget`。`ClusterTarget` 是提议中的 Git 文档，位于 cluster 配置仓库。`DistributionTarget` 是 controller 消费的 live CRD。

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `clusterName` | 否 | 逻辑集群名称。 |
| `bomPath` | `bomPath` 或 `releaseChannelPath` 二选一 | 直接指向 BOM 的路径。 |
| `releaseChannelPath` | `bomPath` 或 `releaseChannelPath` 二选一 | 指向 release channel 文档的路径。 |
| `localRepoPath` | 否 | 本地 source 或 artifact 使用的 local repository path。 |
| `localPatchRevision` | 否 | 要应用的 local patch revision。 |
| `packageSources` | 否 | 按 component 显式声明的 package source paths 和 digests。 |
| `cacheRoot` | 否 | Runtime cache root。 |
| `kubeconfigPath` | 否 | Kubeconfig 路径，不是内容。 |
| `hostRoot` | 否 | Reconciliation 使用的 host root。 |
| `rolloutPolicyRef` | 否 | 指向 `DistributionRolloutPolicy` 的引用。 |
| `rolloutBatchSize` | 否 | Inline batch size override。 |
| `requeueAfter` | 否 | Controller requeue interval。 |

## Status 契约

Status 记录：

- `observedGeneration`
- `lastReconcileTime`
- `lastResult.clusterName`
- `lastResult.bomName`
- `lastResult.revision`
- `lastResult.channel`
- `lastResult.bundlePath`
- `lastResult.desiredStateDigest`
- `lastResult.appliedRevisionPath`
- `conditions`

## 校验规则

- 必须且只能设置 `bomPath` 或 `releaseChannelPath` 其中之一。
- Paths 必须对 controller runtime 有意义。
- Spec 中不能嵌入 secret 值。
- Status 由 controller 拥有。

## 生命周期

1. Cluster operator 创建或更新 `DistributionTarget`。
2. Controller 解析 BOM 或 release channel。
3. Controller hydrate 并 apply desired state。
4. Status 记录最新 reconcile result 和 applied revision path。

## 边界

- `DistributionTarget` 不定义 package contents。
- `DistributionTarget` 不替代 Git source documents。
- `DistributionTarget` 不存储长篇 acceptance evidence。
- `DistributionTarget` 只能通过 path 引用 secret-bearing files。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionTarget
metadata:
  name: prod-01
  namespace: distribution-system
spec:
  clusterName: prod-01
  releaseChannelPath: channels/sealos/stable.yaml
  localRepoPath: /var/lib/sealos/distribution/local-repo
  localPatchRevision: prod-01-2026-06-01
  cacheRoot: /var/cache/sealos/distribution
  kubeconfigPath: /etc/sealos/kubeconfig
  hostRoot: /
  rolloutPolicyRef:
    name: default-rollout
  requeueAfter: 5m
```

## 相关 Kind

- `DistributionRolloutPolicy` 控制 rollout 行为。
- `ReleaseChannel` 和 `BOM` 提供 source selection。
- `HydratedBundle` 在 reconciliation 中生成。
- `AppliedRevision` 记录已应用运行时状态。
