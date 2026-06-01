# Kind: LocalRepo

## 状态

仅作说明。尚未实现为 schema 或 CRD。

## 类别

本地仓库模型文档。

## 维护方

如果未来成为真实 schema，应由本地 cluster platform owner 维护该模型。

## 用途

`LocalRepo` 命名源优先本地构建模式使用的本地镜像或本地事实仓库。它描述 local source facts、cached artifacts 和 local patch revisions 的存储位置。

当前实现不需要该 kind。这里记录它，是为了让本地镜像概念显式化，并为未来 schema 工作保留术语。

## 可能位置

- `local-repos/<name>.yaml`
- `clusters/<cluster>/local-repo.yaml`

## 可能的 Spec 契约

| 字段 | 说明 |
| --- | --- |
| `root` | 本地 facts 的文件系统或仓库 root。 |
| `mode` | Mirror mode，例如 `sourceMirror`、`artifactMirror` 或 `mixed`。 |
| `distributionRef` | 被本地镜像的 distribution source repository。 |
| `cacheRoot` | 生成或拉取 artifacts 的本地 cache root。 |
| `patchRoot` | 本地 patch root。 |
| `retention` | Cached revisions 的 retention policy。 |

## 校验预期

如果实现 `LocalRepo`，应校验：

- root 必须显式且归一化；
- 路径不能逃逸配置的 repository root；
- 不能内联 secret material；
- mirror state 应通过不可变 revision 记录，而不是只靠可变名称。

## 边界

- `LocalRepo` 不应替代 `BOM`。
- `LocalRepo` 不应替代 `ReleaseChannel`。
- `LocalRepo` 不应被当作 runtime apply state。
- `LocalRepo` 当前是文档术语，不是 API contract。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepo
metadata:
  name: prod-01-local
spec:
  root: /var/lib/sealos/distribution/local-repo
  mode: mixed
  distributionRef:
    name: sealos-distribution
    ref: main
  cacheRoot: cache/
  patchRoot: patches/
```

## 相关 Kind

- `LocalRepoRevision` 可以标识该 local repo 的不可变 snapshot。
- `ClusterTarget` 可以选择 local delivery mode。
- `HydratedBundle` 在使用 local repo 时记录 provenance。
- `DistributionTarget` 有 local repo path 的运行时字段。
