# Kind: ReleaseChannel

## 状态

已实现的文件 schema。`ReleaseChannel` 是 release 指针文档。

## 类别

发布源文档。

## 维护方

Release owner 或 promotion owner 维护 release channel 文档。

## 常见位置

- `channels/<distribution>/<channel>.yaml`
- `releases/<distribution>/channels/<channel>.yaml`

## 用途

`ReleaseChannel` 将一个命名 channel，例如 `stable`、`beta` 或 `alpha`，指向目标 distribution revision 和 BOM path。它是下游集群选择跟随的 promotion 指针。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: sealos-stable
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `distribution` | 是 | Distribution line，例如 `sealos`。 |
| `channel` | 是 | Channel 名称，例如 `stable`、`beta` 或 `alpha`。 |
| `targetRevision` | 是 | 目标 BOM revision。 |
| `bomPath` | 是 | 指向目标 BOM 的仓库相对路径。 |
| `promotionHistory` | 否 | 该 channel 的历史 promotion 记录。 |

## Promotion History

每条 promotion history 记录：

- 来源 revision
- 目标 revision
- BOM path
- promotion 原因
- approver
- approval time
- 可选的 health proof path、digest 和 summary

Promotion history 是 append-only evidence。当前 channel 状态始终以 `targetRevision` 和 `bomPath` 为准。

## 校验规则

- 必须设置 `apiVersion`、`kind` 和 `metadata.name`。
- 必须设置 `spec.distribution`。
- 必须设置 `spec.channel`、`spec.targetRevision` 和 `spec.bomPath`。
- `bomPath` 必须是仓库相对路径。
- Promotion evidence 应引用一个 `DistributionHealthProof`。

## 生命周期

1. 生成 release candidate `BOM`。
2. Package acceptance 和健康检查生成 evidence。
3. Promotion owner 更新 `ReleaseChannel.targetRevision` 和 `bomPath`。
4. 跟随该 channel 的 cluster targets reconcile 到新 revision。

## 边界

- `ReleaseChannel` 不定义 package content。
- `ReleaseChannel` 不指向具体集群。
- `ReleaseChannel` 不绕过本地 patch gate。
- `ReleaseChannel` 不是运行时状态。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: sealos-stable
spec:
  distribution: sealos
  channel: stable
  targetRevision: v5.0.0
  bomPath: boms/sealos/v5.0.0/bom.yaml
  promotionHistory:
    - fromRevision: v5.0.0-rc.1
      toRevision: v5.0.0
      bomPath: boms/sealos/v5.0.0/bom.yaml
      reason: package acceptance passed
      approvedBy: release-team
      approvedAt: "2026-06-01T00:00:00Z"
      healthProofPath: proofs/sealos/v5.0.0/stable.yaml
      healthProofDigest: sha256:...
      healthProofSummary: all required checks passed
```

## 相关 Kind

- `BOM` 提供目标 package set。
- `DistributionHealthProof` 提供 promotion evidence。
- `ClusterTarget` 可以按 channel 选择 release。
- `DistributionTarget` 可以从 channel path reconcile。
