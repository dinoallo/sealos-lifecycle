# Kind: LocalRepoRevision

## 状态

仅作说明。尚未实现为 schema 或 CRD。

## 类别

本地仓库 evidence 文档。

## 维护方

如果该 kind 被实现，应由本地 build 或 mirror workflow 写入该文档。

## 用途

`LocalRepoRevision` 可以标识 `LocalRepo` 的不可变 snapshot。它让源优先本地构建模式能够记录 hydration 或 apply 使用了哪些 local source facts、patches 和 cached artifacts。

## 可能位置

- `local-repos/<name>/revisions/<revision>.yaml`
- `clusters/<cluster>/local-repo-revisions/<revision>.yaml`

## 可能的 Spec 契约

| 字段 | 说明 |
| --- | --- |
| `localRepo` | Local repository 名称。 |
| `revision` | 不可变 local repo revision 标识。 |
| `distributionRef` | Source distribution repository 和 ref。 |
| `sourceDigest` | Mirrored source facts 的 digest。 |
| `patchDigest` | Local patch facts 的 digest。 |
| `artifactIndexDigest` | Local artifact index 的 digest。 |
| `createdAt` | RFC3339 创建时间。 |

## 校验预期

如果实现 `LocalRepoRevision`，应校验：

- revision identifier 必须不可变；
- 所有 referenced digests 必须使用支持的 digest 格式；
- source 和 patch digests 必须基于归一化 file trees 计算；
- generated artifacts 必须能追溯到 source 和 build class provenance。

## 生命周期

1. 本地 mirror 或 build workflow snapshot local repo。
2. Workflow 写入带 digests 的 revision document。
3. Hydration 记录选中的 local repo revision。
4. `AppliedRevision` 记录哪个 local repo revision 参与了 applied state。

## 边界

- `LocalRepoRevision` 不定义目标 release。
- `LocalRepoRevision` 不批准 local policy changes。
- `LocalRepoRevision` 不携带 secret material。
- `LocalRepoRevision` 当前是文档术语，不是 API contract。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepoRevision
metadata:
  name: prod-01-local-2026-06-01
spec:
  localRepo: prod-01-local
  revision: 2026-06-01T00-00-00Z
  distributionRef:
    name: sealos-distribution
    ref: abc123
  sourceDigest: sha256:...
  patchDigest: sha256:...
  artifactIndexDigest: sha256:...
  createdAt: "2026-06-01T00:00:00Z"
```

## 相关 Kind

- `LocalRepo` 命名 mutable local repository。
- `HydratedBundle` 记录 render 使用的 local revision。
- `AppliedRevision` 记录 apply 使用的 local revision。
