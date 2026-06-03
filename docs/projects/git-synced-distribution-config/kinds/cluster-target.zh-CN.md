# Kind: ClusterTarget

## 状态

提议中的文件 schema。这是 cluster 配置仓库文档，不是已实现的 Kubernetes CRD。

## 类别

集群意图文档。

## 维护方

Cluster 配置 owner 维护 `ClusterTarget` 文档。

## 常见位置

- `clusters/<cluster>/target.yaml`
- `targets/<cluster>.yaml`

## 用途

`ClusterTarget` 声明一个集群应跟随哪个 distribution line、channel、profile 和 delivery mode。它属于独立的 cluster 配置仓库，使 distribution 源配置和集群特定意图可以独立演进。

`ClusterTarget` 是集群选择的 source-of-truth 输入，不是运行时 reconcile 对象。运行时 reconcile 由 `DistributionTarget` CRD 表示。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ClusterTarget
metadata:
  name: prod-01
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `distribution` | 是 | 集群选择的 distribution line。 |
| `channel` | 是 | 集群跟随的 release channel。 |
| `profile` | 否 | 集群 profile，例如 `default`、`prod` 或 `edge`。 |
| `delivery.mode` | 是 | Delivery mode，预期值为 `sourceFirstLocalBuild` 或 `nonLocalBuild`。 |
| `distributionRef.name` | 否 | 命名的 distribution 仓库引用。 |
| `distributionRef.ref` | 否 | Distribution 仓库的 Git ref、tag 或 revision。 |
| `localPatchRevision` | 否 | 选择的本地 patch revision。 |
| `inputs` | 否 | 指向 `ComponentInput` 文档的引用。 |
| `patches` | 否 | 指向 cluster-owned patch 文件的引用。 |
| `secrets` | 否 | 按 path 或 name 引用外部 secret 位置，不能内联 secret 值。 |

## Delivery Modes

`sourceFirstLocalBuild` 表示集群 workflow 可以在 artifact 缺失或明确不使用 artifact 时，从仓库 source facts 构建 package。

`nonLocalBuild` 表示集群 workflow 只消费选中 `BOM` 引用的已发布 artifact。

两种模式应共享同一套 distribution package model。Mode 改变的是执行策略，而不是 package identity 的含义。

## 校验规则

- 必须设置 `distribution`、`channel` 和 `delivery.mode`。
- `delivery.mode` 必须属于支持的模式。
- `inputs[*].path`、`patches[*].path` 和被引用文件必须是仓库相对路径。
- `secrets` 必须引用 secret 位置，不能嵌入 secret 值。
- 当存在 cluster-owned patches 时，应 pin `localPatchRevision`。

## 生命周期

1. Cluster owner 选择 release channel 和 delivery mode。
2. Cluster owner pin local patch 和 input 引用。
3. Controller 或 materializer 根据 distribution source documents 解析 target。
4. 运行时 reconciliation 通过 `DistributionTarget` 创建或更新。

## 边界

- `ClusterTarget` 不定义 package source 或 package content。
- `ClusterTarget` 不执行 rollout。
- `ClusterTarget` 不存储运行时 status。
- `ClusterTarget` 不能包含 secret material。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ClusterTarget
metadata:
  name: prod-01
spec:
  distribution: sealos
  channel: stable
  profile: prod
  delivery:
    mode: sourceFirstLocalBuild
  distributionRef:
    name: sealos-distribution
    ref: main
  localPatchRevision: prod-01-2026-06-01
  inputs:
    - component: kubernetes
      path: inputs/kubernetes.yaml
  patches:
    - path: patches/kubernetes/
  secrets:
    - path: external-secrets/prod-01.yaml
```

## 相关 Kind

- `ReleaseChannel` 解析被选择的 channel。
- `BOM` 解析被选择的 revision。
- `ComponentInput` 提供非 secret 集群值。
- `DistributionTarget` 是从该 intent 派生的运行时 CRD。
- `AppliedRevision` 记录应用结果。
