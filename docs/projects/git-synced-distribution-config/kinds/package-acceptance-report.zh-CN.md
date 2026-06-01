# Kind: PackageAcceptanceReport

## 状态

已实现的生成文件 schema。

## 类别

Acceptance evidence 文档。

## 维护方

Package acceptance workflow 写入该文档。

## 常见位置

- `reports/package-acceptance/<revision>/<cluster>.yaml`
- `evidence/package-acceptance/<revision>.yaml`

## 用途

`PackageAcceptanceReport` 记录 package acceptance 结果，包括 preflight state、apply stages、revert checks、package mode、source mode 和 desired state digests。它为 promotion workflow 在 revision 进入 release channel 前提供具体证据。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: PackageAcceptanceReport
metadata:
  name: sealos-v5.0.0-prod-01
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `clusterName` | 否 | Cluster 或 test target 名称。 |
| `startedAt` | 否 | RFC3339 开始时间。 |
| `finishedAt` | 否 | RFC3339 结束时间。 |
| `status` | 是 | 整体 report status。 |
| `exitCode` | 否 | Process exit code。 |
| `mutatingApply` | 否 | 测试是否执行 mutating apply。 |
| `revertCheck` | 否 | 是否执行 revert validation。 |
| `packageMode` | 否 | Workflow 使用的 package mode。 |
| `bomFile` | 否 | 测试使用的 BOM 文件路径。 |
| `bomName` | 否 | BOM 名称。 |
| `bomRevision` | 否 | BOM revision。 |
| `bomDigest` | 否 | BOM digest。 |
| `workdir` | 否 | Workflow 使用的工作目录。 |
| `runtimeRoot` | 否 | Runtime root。 |
| `localRepo` | 否 | 使用 local mode 时的 local repo path。 |
| `bundleDir` | 否 | Hydrated bundle 目录。 |
| `kubeconfig` | 否 | Kubeconfig 路径，不是内容。 |
| `hostRoot` | 否 | 测试使用的 host root。 |
| `outputsFormat` | 否 | Workflow 选择的 output format。 |
| `desiredStateDigest` | 否 | 被测试的 desired state digest。 |
| `localRepoRevision` | 否 | 被测试的 local repo revision。 |
| `sourcePreflightState` | 否 | Source preflight 结果。 |
| `runtimePreflightState` | 否 | Runtime preflight 结果。 |
| `postApplyState` | 否 | Apply 后状态。 |
| `postRevertState` | 否 | Revert 后状态。 |
| `stages` | 否 | Stage-level command 和 result records。 |
| `notes` | 否 | 附加的非 secret notes。 |

## Stage 契约

每个 stage 可包含：

- `name`
- `status`
- `mutates`
- `startedAt`
- `finishedAt`
- `output`
- `command`
- `reason`

## 校验规则

- 必须设置 `apiVersion`、`kind` 和 `metadata.name`。
- 必须设置 `spec.status`。
- `finishedAt` 如果存在，必须是 RFC3339。
- 每个 stage 必须有 name 和 status。
- 不能嵌入 secret 值。只有审计确实需要时，才可以记录 secret-bearing file 的路径。

## 生命周期

1. Package acceptance 针对 BOM 和 package mode 运行。
2. Workflow 记录 stage results 和 desired state digests。
3. Health proof 可以汇总一个或多个 acceptance reports。
4. Release promotion 引用 health proof 或 report evidence。

## 边界

- `PackageAcceptanceReport` 是 evidence，不是 release intent。
- `PackageAcceptanceReport` 不选择 release channel。
- 当完整日志可能包含 secrets 时，不应把完整日志放进 `PackageAcceptanceReport`。
- `PackageAcceptanceReport` 发布后应保持不可变。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: PackageAcceptanceReport
metadata:
  name: sealos-v5.0.0-prod-01
spec:
  clusterName: prod-01
  status: Passed
  startedAt: "2026-06-01T00:00:00Z"
  finishedAt: "2026-06-01T00:10:00Z"
  mutatingApply: true
  revertCheck: true
  packageMode: sourceFirstLocalBuild
  bomFile: boms/sealos/v5.0.0/bom.yaml
  bomName: sealos-v5.0.0
  bomRevision: v5.0.0
  bomDigest: sha256:...
  desiredStateDigest: sha256:...
  localRepoRevision: 2026-06-01T00-00-00Z
  stages:
    - name: source-preflight
      status: Passed
      mutates: false
    - name: apply
      status: Passed
      mutates: true
```

## 相关 Kind

- `DistributionHealthProof` 汇总 promotion 使用的 health evidence。
- `BOM` 标识被测试的 release。
- `HydratedBundle` 提供 acceptance 测试的 desired state。
- `ReleaseChannel` promotion 可以引用生成的 evidence。
