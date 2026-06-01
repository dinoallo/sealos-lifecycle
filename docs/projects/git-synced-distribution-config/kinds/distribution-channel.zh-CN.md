# Kind: DistributionChannel

## 状态

已实现的兼容别名。新文档应使用 `ReleaseChannel`。

## 类别

旧版发布源文档。

## 维护方

Release owner 仅在兼容需要时维护旧文档。

## 常见位置

- 既有 legacy channel 文件。

## 用途

`DistributionChannel` 是 channel pointer 的旧名称，现在由 `ReleaseChannel` 表示。它存在的目的，是让旧仓库仍能被加载，同时文档模型逐步收敛到更清晰的命名。

## 兼容契约

Loader 同时接受：

```yaml
kind: DistributionChannel
```

以及：

```yaml
kind: ReleaseChannel
```

它也接受旧字段 `spec.line`，并在可能时归一化为 `spec.distribution`。

## 必需字段

有效必需字段与 `ReleaseChannel` 相同：

- `distribution` 或旧字段 `line`
- `channel`
- `targetRevision`
- `bomPath`

## 迁移建议

对于新建或编辑的文档：

1. 将 `kind` 从 `DistributionChannel` 改为 `ReleaseChannel`。
2. 如果存在 `spec.line`，改为 `spec.distribution`。
3. 保留 `channel`、`targetRevision`、`bomPath` 和 `promotionHistory`。
4. 重新运行 schema validation。

## 边界

- 不应向 `DistributionChannel` 增加新语义。
- 除非兼容需要，不应再创建该 kind 的新文档。
- 提案和示例中应把 `ReleaseChannel` 作为 canonical kind。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionChannel
metadata:
  name: sealos-stable
spec:
  line: sealos
  channel: stable
  targetRevision: v5.0.0
  bomPath: boms/sealos/v5.0.0/bom.yaml
```

## 相关 Kind

- `ReleaseChannel` 是 canonical replacement。
- `BOM` 是 channel 引用的 release package set。
