# Kind: AppliedRevision

## 状态

已实现的运行时状态文件 schema。

## 类别

运行时状态文档。

## 维护方

Apply 或 reconciliation workflow 写入该文档。

## 常见位置

- `state/<cluster>/applied-revision.yaml`
- `out/state/<cluster>/applied-revision.yaml`

## 用途

`AppliedRevision` 记录当前在集群上观测到或最后成功应用的 distribution revision。它为 drift 和审计 workflow 提供紧凑的运行时状态文档，避免把 source documents 当作 applied state。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedRevision
metadata:
  name: prod-01
spec: {}
status: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `clusterName` | 是 | 集群名称。 |
| `bom.name` | 是 | 已应用 BOM 名称。 |
| `bom.revision` | 是 | 已应用 BOM revision。 |
| `bom.channel` | 否 | 解析 BOM 使用的 channel。 |
| `bom.digest` | 否 | 已应用 BOM digest。 |
| `localRepoRevision` | 否 | 源优先本地模式使用的 local repo revision。 |
| `localPatchRevision` | 否 | Apply 使用的 local patch revision。 |
| `desiredStateDigest` | 是 | 已应用 desired state 的 digest。 |

## Status 契约

| 字段 | 说明 |
| --- | --- |
| `state` | `Clean`、`Dirty`、`Orphan` 或 `Degraded` 之一。 |
| `lastAppliedTime` | 最后一次 apply 尝试时间。 |
| `lastSuccessfulRevision` | 最后已知成功 revision。 |
| `observedSummary` | 观测资源数量或摘要。 |
| `conditions` | Apply 和 drift state 的结构化 conditions。 |

## 校验规则

- 必须设置 `clusterName`、BOM identity 和 `desiredStateDigest`。
- `state` 必须是支持的状态之一。
- Digest 如果存在，必须使用支持的 digest 格式。
- Runtime fields 必须由 reconciliation 写入，不应由 source authors 写入。

## 生命周期

1. Apply 消费 `HydratedBundle`。
2. Reconciliation 写入 applied revision 和 desired state digest。
3. Drift detection 更新 status conditions。
4. 后续 apply 用新 revision 更新同一个 cluster state。

## 边界

- `AppliedRevision` 是 runtime state，不是 source intent。
- `AppliedRevision` 不包含完整 object inventory。
- `AppliedRevision` 不替代 health 或 acceptance evidence。
- `AppliedRevision` 不应用来选择未来 release。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedRevision
metadata:
  name: prod-01
spec:
  clusterName: prod-01
  bom:
    name: sealos-v5.0.0
    revision: v5.0.0
    channel: stable
    digest: sha256:...
  localRepoRevision: 2026-06-01T00-00-00Z
  localPatchRevision: prod-01-2026-06-01
  desiredStateDigest: sha256:...
status:
  state: Clean
  lastAppliedTime: "2026-06-01T00:00:00Z"
  lastSuccessfulRevision: v5.0.0
```

## 相关 Kind

- `HydratedBundle` 提供要 apply 的 desired state。
- `AppliedInventory` 可以提供详细 managed inventory。
- `DistributionTarget` status 可以引用 applied revision path。
- `PackageAcceptanceReport` 记录 release 前 acceptance evidence。
