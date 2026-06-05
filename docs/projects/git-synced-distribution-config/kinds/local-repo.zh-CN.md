# Kind: LocalRepo

## 状态

已实现为 local repo 文件 schema。它不是 Kubernetes CRD。

## 类别

本地 source document。

## 维护方

Cluster owner 维护 local repo；`sealos sync local-repo init` 写入初始文档。

## 用途

`LocalRepo` 标识 render、apply、status 和 commit workflow 使用的 cluster-local
repository。它记录这份 local inputs、local resources、patches 和 policy 属于哪个
cluster、哪条 distribution line。

## 位置

```text
local-repo/repo.yaml
```

## Spec 契约

| 字段 | 说明 |
| --- | --- |
| `cluster` | 这份 local repo 属于哪个 cluster。 |
| `distributionLine` | 这份 local repo 跟随哪条 distribution line。 |
| `channel` | init 时选择的可选 release channel。 |
| `bom` | init 时使用的 BOM name。 |
| `bomRevision` | init 时使用的 BOM revision。 |

## 校验

当 `repo.yaml` 存在时，`localrepo.Load` 会校验 `apiVersion`、`kind`、
`metadata.name`、`spec.cluster` 和 `spec.distributionLine`。旧 local repo 仍可
加载；如果 metadata 文件缺失或仍是早期示例形状，`sync local-repo doctor` 会报告
warning。

## 边界

- `LocalRepo` 不替代 `BOM`。
- `LocalRepo` 不替代 `ReleaseChannel`。
- `LocalRepo` 不携带 Secret payload。
- `LocalRepo` 不是 runtime apply state。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepo
metadata:
  name: prod-01-default-platform
spec:
  cluster: prod-01
  distributionLine: default-platform
  channel: stable
  bom: default-platform
  bomRevision: rev-2026-06-01
```

## 相关 Kind

- `LocalRepoRevision` 为这份 local repo 记录 digest-backed audit snapshot。
- `HydratedBundle` 在使用 local repo 时记录 provenance。
- `AppliedRevision` 记录 render/apply 使用的 local repo revision digest。
