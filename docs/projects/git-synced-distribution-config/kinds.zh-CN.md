# Distribution Document Kind 规范

## 状态

草案

## 摘要

本文定义 package-based distribution 模型中 `distribution.sealos.io/v1alpha1`
document kinds 的目录和基本规范。

不是本文里的每个 kind 都是 Kubernetes CRD。大多数 kind 是 Git 或本地文件系统中的文档。
只有需要通过 Kubernetes API 被 controller reconcile 的 kind 才应该安装成 CRD。

## Kind 分类

| 分类 | 含义 |
| --- | --- |
| Kubernetes CRD | 安装到 Kubernetes API server，并由 controller reconcile。 |
| Repository source document | 存在 `distribution-config` 或 `cluster-config` 中的可审查事实源。 |
| Local source document | 存在 local repo 或 cluster workspace 附近的 cluster-local 事实源。 |
| Built-in contract | 由 Sealos 实现并被 source document 引用的版本化行为；仓库可携带可选 descriptor。 |
| Generated document | render、apply、smoke 或 validation workflow 产生的确定性输出。 |
| Evidence document | promotion 或 policy gate 使用的可审查证明。 |
| Proposed document | 规划中的 schema，还没有一等 loader 实现。 |
| Illustrative document | 文档中有用的模型，但还不是 schema 承诺。 |

## 通用 Envelope

除非具体 kind 文档另有说明，所有 distribution document kind 都应使用同一个顶层形态：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: <Kind>
metadata:
  name: <name>
spec: {}
status: {}
```

通用规则：

- `apiVersion` 必须是 `distribution.sealos.io/v1alpha1`。
- `kind` 必须是本文列出的 kind 之一。
- 具名文档必须有 `metadata.name`。
- `metadata.labels` 可用于 ownership、release 和 automation hints。
- `spec` 保存 desired state、source facts、policy 或 evidence inputs。
- `status` 只用于 Kubernetes CRD 或生成的 runtime state document。
- source document 不能包含明文 secret 值。
- 仓库相对路径不能是绝对路径，也不能用 `..` 逃逸所属 repository root。
- generated document 必须携带足够 provenance，用来识别产生它的 source revision、
  BOM revision、local repo revision 和 digest inputs。

## Catalog

每个 kind 的详细契约位于 [`kinds/`](./kinds/) 目录。下面的目录表会链接到对应详情页。

| Kind | 分类 | Owner | 常见位置 | 状态 |
| --- | --- | --- | --- | --- |
| [`ComponentPackage`](./kinds/component-package.zh-CN.md) | Repository source document | Package owner | `packages/<category>/<name>/<version>/package.yaml`、materialized package root | 已实现文件 schema |
| [`BuildClass`](./kinds/build-class.zh-CN.md) | Built-in contract | Platform team | Sealos built-in class registry；可选 `classes/<name>/<version>.yaml` | Proposed |
| [`BOM`](./kinds/bom.zh-CN.md) | Repository source document | Platform release owner | `releases/<distribution>/<revision>/bom.yaml` | 已实现文件 schema |
| [`ReleaseChannel`](./kinds/release-channel.zh-CN.md) | Repository source document | Release manager | `channels/<distribution>/<channel>.yaml` | 已实现推荐名称，代码接受旧别名 |
| [`DistributionChannel`](./kinds/distribution-channel.zh-CN.md) | Repository source document | Release manager | 现有本地 channel 文件 | 已实现兼容名称 |
| [`DistributionHealthProof`](./kinds/distribution-health-proof.zh-CN.md) | Evidence document | Release automation | CI artifact 或 promotion evidence path | 已实现文件 schema |
| [`ClusterTarget`](./kinds/cluster-target.zh-CN.md) | Repository source document | Cluster owner | `cluster-config/clusters/<scope>/<cluster>/target.yaml` | Proposed |
| [`ComponentInput`](./kinds/component-input.zh-CN.md) | Repository source document | Cluster owner | `cluster-config/clusters/<scope>/<cluster>/inputs/*.yaml` | Proposed |
| [`LocalPatchPolicy`](./kinds/local-patch-policy.zh-CN.md) | Local source document | Platform 或 cluster owner | package source、BOM reference 或 local repo `policy/local-patch-policy.yaml` | 已实现文件 schema |
| [`LocalPatchPolicyGateApproval`](./kinds/local-patch-policy-gate-approval.zh-CN.md) | Evidence document | Policy reviewer | local repo policy evidence 或 CI artifact | 已实现文件 schema |
| [`HydratedBundle`](./kinds/hydrated-bundle.zh-CN.md) | Generated document | Agent 或 CI | render workspace `bundle.yaml` | 已实现生成 schema |
| [`AppliedRevision`](./kinds/applied-revision.zh-CN.md) | Generated runtime document | Agent | cluster runtime state store `applied-revision.yaml` | 已实现 runtime schema |
| [`AppliedInventory`](./kinds/applied-inventory.zh-CN.md) | Generated runtime document | Agent | rendered 或 applied inventory output | Proposed |
| [`PackageAcceptanceReport`](./kinds/package-acceptance-report.zh-CN.md) | Evidence document | Package test automation | smoke 或 acceptance artifact | 已实现文件 schema |
| [`DistributionTarget`](./kinds/distribution-target.zh-CN.md) | Kubernetes CRD | Cluster operator | Kubernetes API，namespaced | 已实现 CRD |
| [`DistributionRolloutPolicy`](./kinds/distribution-rollout-policy.zh-CN.md) | Kubernetes CRD | Cluster operator | Kubernetes API，namespaced | 已实现 CRD |
| [`LocalRepo`](./kinds/local-repo.zh-CN.md) | Local source document | Cluster owner | local repo metadata | 示例性质，未实现 |
| [`LocalRepoRevision`](./kinds/local-repo-revision.zh-CN.md) | Local source document | Cluster owner | local repo revision metadata | 示例性质，未实现 |

## Source Document Kinds

### `ComponentPackage`

目的：定义一个 package revision 及其 render/apply contract。

常见位置：

```text
packages/<category>/<name>/<version>/package.yaml
```

最小契约：

- 声明 package component、version 和 package class
- 声明 rootfs、files、manifests、charts 或 hooks 等 package contents
- 声明支持的 input surface
- 在需要 package-specific build facts 时，声明 package-local build inputs、staging
  rules 或 adapter scripts
- 必要时声明 package dependencies
- 所有引用路径都相对 package root

不能包含：

- cluster-local input values
- 明文 secret 值
- 生成的 render output
- 下载或缓存的 artifact 数据

### `BuildClass`

目的：定义可复用的构建 workflow contract，用来把 package source facts 转成
materialized package payload。

常见解析位置：

```text
Sealos built-in class registry
可选 classes/<name>/<version>.yaml
```

`rootfs/v1` 和 `manifest-bundle/v1` 等标准 class 由 Sealos 实现，不需要出现在每个
distribution 仓库中。Repo-local `BuildClass` 文件是 custom、experimental 或
policy-pinned class 的可选 descriptor。

最小契约：

- output kind，例如 package root、OCI artifact、OCI layout 或 local registry image
- 接受的 package classes
- 支持的平台
- `source.digest` 使用的 source include/exclude 规则
- builder implementation 或 command family
- 必需的确定性、非 secret build options
- 必需 provenance fields
- 不包含属于 `ComponentPackage.spec.build` 的 package-specific asset list 或
  staging rule

规则：

- 未知 class 应 fail closed，除非当前运行的 Sealos binary 或已批准 extension 提供该实现
- build class version 不可变
- 只要变更可能影响 package bytes、被选中的 source files 或 output metadata，就必须发布新的 class version
- builder 不能读取 `cluster-config`、live cluster state、未声明 host files 或 secret values

### `BOM`

目的：定义一个不可变 distribution release revision。

常见位置：

```text
releases/<distribution>/<revision>/bom.yaml
```

最小契约：

- `spec.revision`
- 通过完整 identity 选择 package：`category`、`name`、`version`
- 对可构建 package 锁定 source facts 和 source digest
- 对可构建 package 选择 build class 和 release-level build profile 或 options
- 当预构建 artifact 必需或可用时，锁定 artifact image 和 digest
- package 依赖其他 package 时声明 dependency reference

不能包含：

- channel membership
- 已在 `ComponentPackage` 声明的 package-specific build recipes
- cluster-local inputs
- cluster-local patches
- secret 值
- 生成的 render output

### `ReleaseChannel`

目的：让一个 channel 指向一个已批准的不可变 BOM revision。

常见位置：

```text
channels/<distribution>/<channel>.yaml
```

最小契约：

- distribution 或 line 名称
- channel 名称
- 目标 BOM revision
- 目标 BOM 路径
- 可选 promotion history 和 health proof reference

规则：

- channel 移动是 release intent，应该是一个小型、可审查变更
- 被引用 BOM 的 revision 必须匹配 `spec.targetRevision`
- 新的 repository layout 应优先使用 `ReleaseChannel`

### `DistributionChannel`

目的：当前 guides 和命令使用的已实现 channel document 名称。

规则：

- 作为 channel pointer document 的兼容名称处理
- 新的 git-synced repository layout 应迁移到 `ReleaseChannel`
- 迁移期间 resolver 可以同时接受两个名称

### `DistributionHealthProof`

目的：记录 channel promotion 使用的 health evidence。

最小契约：

- distribution line
- target revision
- `passed`
- 可用时记录 collected time
- signal list，包含名称、pass/fail 状态和 message

规则：

- 本身不会移动 channel
- 可被 channel promotion history 引用
- 不能复制 secret payload、kubeconfig、token 或 host-private files

## Cluster Source Kinds

### `ClusterTarget`

目的：`cluster-config` 中每个 cluster 的稳定入口。

常见位置：

```text
cluster-config/clusters/<scope>/<cluster>/target.yaml
```

最小契约：

- 选中的 distribution
- 选中的 channel 或 revision
- 选中的 profile
- delivery mode
- 配置好的 distribution repository reference
- 相对 cluster root 的 input、patch 和 secret-reference paths

规则：

- 不能复制 BOM package 内容
- 不能嵌入 repository credentials
- local path 不能逃逸 cluster root
- delivery mode 不能改变 package graph、input order 或 patch order

### `ComponentInput`

目的：把 package 声明的 input surface 绑定到 cluster-local 非 secret 值。

常见位置：

```text
cluster-config/clusters/<scope>/<cluster>/inputs/*.yaml
```

最小契约：

- 目标 component 或 package identity
- 对声明 input names 的 values

规则：

- 只能绑定被选中 `ComponentPackage` 声明过的 inputs
- 不能包含明显 secret material
- secret-shaped inputs 应通过 secret references 或 runtime injection points 绑定

### `LocalPatchPolicy`

目的：定义 cluster-local patches 可以修改哪些内容。

常见位置：

```text
local-repo/policy/local-patch-policy.yaml
packages/<category>/<name>/<version>/policy/local-patch-policy.yaml
```

最小契约：

- scope，目前是 `clusterLocal`
- forbidden exact paths
- forbidden metadata keys
- forbidden container fields
- 带 allowed prefixes 的 kind rules

规则：

- 只治理 cluster-local override surfaces
- 不会把 package-owned fields 变成 local
- package-side 和 BOM-side policy source 选择 local patch policy，不引入单独的 package/BOM policy layer

### `LocalPatchPolicyGateApproval`

目的：记录需要 review gate 的 local patch policy 变更的人审批准。

最小契约：

- owner
- approver
- change reference
- expiration
- old 和 new policy references
- gate 需要时记录 expected impact

规则：

- 它是 policy change 的 evidence，不是 runtime policy 本身
- 应基于它批准的实际 policy diff 做校验

### `LocalRepo`

目的：未来用于识别 cluster-local repo 的 metadata document。

状态：仅为示例性质。当前文档用它说明模型，但 schema 未实现。

### `LocalRepoRevision`

目的：未来用于记录当前 cluster-local input 和 patch revision 的 metadata document。

状态：仅为示例性质。当前文档用它说明模型，但 schema 未实现。

## Generated And Runtime Kinds

### `HydratedBundle`

目的：由 BOM、package payload、profile defaults、cluster inputs 和 local patches 生成的
desired-state bundle。

常见位置：

```text
<agent-or-ci-workspace>/bundle.yaml
```

规则：

- 是生成产物，不是主要事实源
- 必须记录 render provenance
- 应包含 BOM revision、channel、local repo revision、package source digests、
  local patch policy source 和 tracked resources
- 可作为 audit evidence 保留，但不应替代 BOM 或 `cluster-config` 作为源输入

### `AppliedRevision`

目的：记录 cluster 当前 rendered 或 applied revision 以及 observed state。

常见位置：

```text
<cluster-runtime-root>/distribution/applied-revision.yaml
```

规则：

- cluster-local mutable runtime state
- 记录 BOM reference、desired state digest、local repo revision、local patch revision、
  observed state 和 conditions
- 不应被当作 release intent 手工编辑

### `AppliedInventory`

目的：未来的 generated inventory document，用于解释某个 rendered revision 期望产生的
具体 objects 和 host paths。

状态：proposed。它增强 `AppliedRevision` 的可观测性，但还没有作为一等持久化 schema 实现。

### `PackageAcceptanceReport`

目的：记录 package lifecycle test evidence，例如 smoke、apply 或 revert 结果。

规则：

- 由 package acceptance automation 生成
- 被 `DistributionHealthProof` generation 消费
- 必须包含足够 BOM、package、local repo 和 desired-state evidence，用来证明测试对象
- 不能包含 secret payloads

## Kubernetes CRD Kinds

### `DistributionTarget`

目的：runtime Kubernetes API object，用来要求 `sealos-agent --controller` reconcile 一个 target。

状态：已实现 CRD。

常见位置：

```text
deploy/distribution-controller/base/crd.yaml
```

最小契约：

- `spec.bomPath` 和 `spec.distributionChannelPath` 二选一
- 可选 local repo path
- 可选 package source overrides
- controller pod 可见的可选 cache、kubeconfig 和 host-root paths
- 可选 rollout policy reference

规则：

- 这是 runtime controller object，不是共享 distribution source of truth
- 未来可以从 `ClusterTarget` 生成，但两者所有权边界不同

### `DistributionRolloutPolicy`

目的：runtime Kubernetes API object，为引用它的 `DistributionTarget` 定义持久 rollout 行为。

状态：已实现 CRD。

最小契约：

- rollout strategy
- batch size
- 可选 canary、pause、health gate 和 failure behavior

规则：

- 只作用于 controller 和 rendered-bundle executor 已覆盖的 rollout 行为
- 不替代所有 multi-node workflow 的 package-level safety design

## 命名和迁移规则

- 不要把所有 distribution document 都称作 CRD。除非该 kind 真的有 Kubernetes
  `CustomResourceDefinition`，否则用 "document kind"。
- 新的 git-synced channel pointer document 使用 `ReleaseChannel`。
- 当前 commands 和 guides 仍使用 `DistributionChannel` 时，继续接受该名称。
- `ClusterTarget` 和 `DistributionTarget` 保持分离：`ClusterTarget` 是 cluster owner 的 Git 意图；
  `DistributionTarget` 是 Kubernetes runtime object。
- `HydratedBundle`、`AppliedRevision` 和 `PackageAcceptanceReport` 这类 generated kind
  不进入 source repositories，除非有明确 audit workflow。

## 推荐 Schema 工作顺序

1. 稳定 source kinds：`ComponentPackage`、`BuildClass`、`BOM`、`ReleaseChannel`。
2. 稳定 cluster-local source kinds：`ClusterTarget`、`ComponentInput`、`LocalPatchPolicy`。
3. 稳定 evidence kinds：`LocalPatchPolicyGateApproval`、`DistributionHealthProof`、
   `PackageAcceptanceReport`。
4. Kubernetes CRD 继续聚焦 runtime reconciliation：`DistributionTarget` 和
   `DistributionRolloutPolicy`。
5. `LocalRepo`、`LocalRepoRevision` 和 `AppliedInventory` 先作为未来 schema 工作处理，
   直到实现依赖它们。
