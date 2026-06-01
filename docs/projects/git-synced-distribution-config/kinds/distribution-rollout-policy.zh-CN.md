# Kind: DistributionRolloutPolicy

## 状态

已实现的 Kubernetes CRD。

## 类别

运行时 rollout policy API。

## 维护方

Cluster operator 或 platform owner 维护 rollout policies。

## Kubernetes API

- Group: `distribution.sealos.io`
- Version: `v1alpha1`
- Kind: `DistributionRolloutPolicy`
- Scope: Namespaced
- Short name: `distrollout`

## 用途

`DistributionRolloutPolicy` 定义 `DistributionTarget` rollout 应如何推进。它把 rollout 行为与 release selection 和 package content 分开。

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `strategy` | 否 | Reconciliation 使用的 rollout strategy。 |
| `strategy.batchSize` | 否 | 分批 rollout 的 batch size。 |
| `canary.batchSize` | 否 | Canary batch size。 |
| `pause.afterCanary` | 否 | Canary 后是否暂停。 |
| `healthGate` | 否 | Health gate 配置。 |
| `failureAction` | 否 | Failure behavior，例如 `Stop` 或 `Rollback`。 |

Go 类型将 strategy shape 委托给 reconcile rollout strategy model。CRD 暴露 batch、canary、pause、health gate 和 failure action 等常用 rollout controls。

## Status 契约

Status 记录：

- `observedGeneration`
- `conditions`

## 校验规则

- Rollout policy name 必须稳定，因为 `DistributionTarget` 会按 name 引用它。
- `failureAction` 如果存在，必须是支持值之一。
- Batch settings 必须与 controller implementation 兼容。
- Status 由 controller 拥有。

## 生命周期

1. Platform owner 创建 rollout policies。
2. `DistributionTarget` 按名称引用 policy。
3. Controller 在 reconciliation 时读取 policy。
4. Status 记录 policy 被接受还是被阻塞。

## 边界

- `DistributionRolloutPolicy` 不选择 release。
- `DistributionRolloutPolicy` 不定义 package contents。
- `DistributionRolloutPolicy` 不携带集群特定 component input。
- `DistributionRolloutPolicy` 不存储 acceptance evidence。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionRolloutPolicy
metadata:
  name: default-rollout
  namespace: distribution-system
spec:
  strategy:
    batchSize: 3
  canary:
    batchSize: 1
  pause:
    afterCanary: true
  healthGate:
    enabled: true
  failureAction: Stop
```

## 相关 Kind

- `DistributionTarget` 引用 rollout policy。
- `AppliedRevision` 记录应用结果。
- `PackageAcceptanceReport` 和 `DistributionHealthProof` 在 rollout policy 之外提供 evidence。
