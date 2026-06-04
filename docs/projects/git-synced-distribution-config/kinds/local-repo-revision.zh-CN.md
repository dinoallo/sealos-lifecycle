# Kind: LocalRepoRevision

## 状态

已实现为 local repo 文件 schema。它不是 Kubernetes CRD。

## 类别

本地 source evidence document。

## 维护方

`sealos sync local-repo init` 写入初始 `current` revision document。Cluster owner
在需要显式 audit checkpoint 时，可以在 local repo 变更后刷新它。

## 用途

`LocalRepoRevision` 记录 cluster-local repo snapshot 的本地 input revision、完整
local repo digest、BOM identity 和审计元数据。它给 render/apply 周边使用的本地
facts 一个 durable reference，但不内联 Secret payload。

## 位置

```text
local-repo/revisions/current.yaml
```

## Spec 契约

| 字段 | 说明 |
| --- | --- |
| `cluster` | 这个 revision 属于哪个 cluster。 |
| `distributionLine` | 这个 revision 跟随哪条 distribution line。 |
| `channel` | init 时选择的可选 release channel。 |
| `bom.name` | init 时选择的 BOM name。 |
| `bom.revision` | init 时选择的 BOM revision。 |
| `bom.digest` | 可选的 selected BOM file digest。 |
| `localInputRevision` | 只覆盖 `inputs/**` 的 digest。 |
| `digest` | 覆盖 local repo inputs、resources、patches 和 policy 的 digest。 |
| `audit.createdAt` | RFC3339 创建时间。 |
| `audit.createdBy` | 可选用户或自动化身份。 |
| `audit.command` | 可选写入命令。 |

## 校验

当 `revisions/current.yaml` 存在时，`localrepo.Load` 会校验 `apiVersion`、`kind`、
身份字段、必填 digest 和 RFC3339 audit time。旧 local repo 仍可加载；如果 revision
文件缺失或仍是早期示例形状，`sync local-repo doctor` 会报告 warning。

## 边界

- `LocalRepoRevision` 不定义目标 release。
- `LocalRepoRevision` 不批准 local policy changes。
- `LocalRepoRevision` 不携带 Secret material。
- render/apply 仍使用当前 local repo 的实时 digest，所以 init 后编辑 inputs 不要求
  在 render 前重写这个 audit object。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepoRevision
metadata:
  name: current
spec:
  cluster: prod-01
  distributionLine: default-platform
  channel: stable
  bom:
    name: default-platform
    revision: rev-2026-06-01
    digest: sha256:...
  localInputRevision: sha256:...
  digest: sha256:...
  audit:
    createdAt: "2026-06-03T00:00:00Z"
    command: sealos sync local-repo init
```

## 相关 Kind

- `LocalRepo` 命名 mutable local repository。
- `HydratedBundle` 记录 render 使用的 local revision。
- `AppliedRevision` 记录 apply 使用的 local revision。
