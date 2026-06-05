# Sealos Sync Operator Action 速查

## 状态

Draft

## 摘要

这份文档是当前单节点 `sync diff` / `sync status` 摘要输出的 operator
速查表。它解释每个 `operatorAction` 的含义、是否允许直接走
`sync commit` 或 `sync revert`，以及什么时候当前 CLI 仍然依赖
`bundleMatchesRecordedDesiredStateDigest`。

把这份文档当成快速查表即可。更完整的 tracking / ownership 设计背景，请看
[Materialization and drift](../architecture/materialization-and-drift.md)。

## 相关文档

- Tracking 与 drift 模型：
  [Materialization and drift](../architecture/materialization-and-drift.md)
- Ownership 与 reconcile 模型：
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- Local repo 布局与 patch/input 规则：
  [Local repo and secret](../guides/local-repo-and-secret.md)
- Local patch policy 的编写与评审 checklist：
  [Local patch policy authoring](../guides/local-patch-policy-authoring.md)
- 当前 operator loop walkthrough：
  [Sync drift](../guides/sync-drift.md)
- 告警、dashboard、工单和修复入口 runbook：
  [Sync operations runbook](../guides/sync-operations-runbook.md)
- 示例 `sync diff` snapshot：
  [docs/examples/sync-drift-minimal/sync-diff.example.yaml](../examples/sync-drift-minimal/sync-diff.example.yaml)
- 示例 `sync status` snapshot：
  [docs/examples/sync-drift-minimal/sync-status.example.yaml](../examples/sync-drift-minimal/sync-status.example.yaml)

## 什么时候看 `sync diff`，什么时候看 `sync status`

- 当你需要原始 compare payload 时，看 `sync diff`：
  tracked object、mismatch、remediation 细节都在这里。
- 当你需要 ownership 摘要、mixed-ownership grouping，以及 cluster recorded
  state 并排查看时，看 `sync status`。
- 在这两个命令里，`operatorAction` 是紧凑的摘要动作名。
- `operatorActionMetadata` 是更窄的能力视图：
  - `allowsDirectCommit`
  - `allowsDirectRevert`
  - `requiresBundleMatch`
- `operatorActionSummary` 是当前 drift 集合的顶层计数视图：
  - `directCommitEligible`
  - `directRevertEligible`
  - `bundleMatchRequired`
- `headline` 是最短的一层 operator 摘要，目标是稳定到可以直接复用在
  告警标题、工单标题或 dashboard 标签里。
- `localPatchPolicy` 是顶层的 provenance block，用来说明当前 rendered
  local-patch policy artifact 到底是哪一份。判断某个 drift 是否能走
  `promoteToLocalPatch` 时，这个字段会告诉你当前 bundle 实际携带的是哪个
  policy source、scope、name 和 digest。

## 去哪里看这些字段长什么样

如果你想直接看具体 YAML，而不是只看抽象字段名，可以把下面两份 fixture
配合起来看：

- [docs/examples/sync-drift-minimal/sync-diff.example.yaml](../examples/sync-drift-minimal/sync-diff.example.yaml)
- [docs/examples/sync-drift-minimal/sync-status.example.yaml](../examples/sync-drift-minimal/sync-status.example.yaml)

它们是缩短过、但和当前 schema 对齐的输出快照，专门用来说明：

- `headline` 出现在什么位置
- `operatorActionSummary` 出现在什么位置
- `policyEligibleOrphanObjects` 出现在什么位置
- `localPatchPolicy` 出现在什么位置
- `operatorAction`、`operatorActionMetadata` 和 remediation block
  在 object / host-path issue 下是怎么对应在一起的

## 当前 `operatorAction` 矩阵

| `operatorAction` | 常见 drift 面 | 含义 | 直接 `sync commit` | 直接 `sync revert` | 需要 Bundle Match | 常见下一步命令路径 |
| --- | --- | --- | --- | --- | --- | --- |
| `commitOrReapplyLocalOverlay` | local-owned object drift | live object 偏离了 Sealos 已经在追踪的本地 overlay。 | yes | yes | yes | `sync commit` 或 `sync revert` |
| `promoteToLocalPatch` | 变动 path 已经符合 `LocalPatchPolicy` 的 package-owned object drift | 这类 drift 当前仍是 global-owned，但支持的长期修复路径已经可以收敛成 local repo patch。 | no | no | no | 编写或调整 `local-repo/patches`，然后 `sync render` 和 `sync apply` |
| `revertOrUpdateGlobalBaseline` | package-owned object drift 或 direct global host-file drift | 这类 drift 属于当前选中的发行版 global baseline。 | no | yes | yes | `sync revert`，或者修改 package/BOM global baseline |
| `commitOrReapplyLocalInput` | local-owned direct host-file drift | 这类 drift 属于已经被追踪的 local input-backed 文件。 | yes | yes | yes | `sync commit` 或 `sync revert` |
| `updateLocalInputAndRerender` | 由本地 bootstrap input 驱动的 generated drift | 正确修复路径是改 local input，再重新生成 desired state。 | no | no | no | 更新 local input，然后 `sync render` 和 `sync apply` |
| `rerenderOrUpdateGlobalBaseline` | 由 package/BOM global baseline 驱动的 generated drift | 正确修复路径是改 global baseline，再重新生成 desired state。 | no | no | no | 更新 package/BOM global baseline，然后 `sync render` 和 `sync apply` |
| `manualReview` | semantic parse 失败或当前不支持自动路由的 generated drift | Sealos 还不能安全地把它放进自动 ownership 流程。 | no | no | no | 手工检查后再决定是否修改 desired 或 live state |

generated host-path remediation block 还会带 projection 级路由 metadata：
`projectionClass`、`generator`、`generatedKind`、`generatedName` 和
`repairable`。`repairable` 是比 `operatorAction` 更窄的 projection 信号：
它只表示这一个 generated projection 当前是否有已知 CLI repair path。摘要层的
`operatorAction` 仍然描述常规 source-of-truth 修复方向，例如 local input、
package/BOM baseline 或 manual review。

## 快速判断规则

### 如果 `allowsDirectCommit: true`

- 说明当前单节点 MVP 已经支持这类 drift 的直接 `sync commit` 路径。
- 今天主要意味着：
  - 本地对象 overlay
  - local input-backed direct host file

### 如果 `allowsDirectRevert: true`

- 说明当前单节点 MVP 已经支持这类 drift 的直接 `sync revert` 路径。
- 今天通常覆盖：
  - 支持的本地 overlay drift
  - 支持的 local input-backed host-file drift
  - 应该被拉回 desired state 的 direct global baseline drift
  - remediation 带 `repairable=true`，且 bundle 中保留 kubeadm input 的部分
    已建模 generated control-plane host path

### 如果 `requiresBundleMatch: true`

- 说明这条直接命令路径仍然受
  `bundleMatchesRecordedDesiredStateDigest` 护栏约束。
- 实际上也就是：你当前检查的 rendered bundle 必须仍然等于 cluster recorded
  desired-state digest，Sealos 才会把这条直接命令路径当成安全的。

### 如果三个字段全是 `false`

- 说明当前 CLI 不支持这类 action 的直接 `commit` / `revert` 路径。
- 修复必须先通过修改 desired state 输入来完成：
  - 本地 patch 编写
  - local input 更新
  - package/BOM global baseline 变更
  - 或手工 review

## 怎么读 `operatorActionSummary`

- `directCommitEligible` 统计当前摘要动作里支持直接 `sync commit` 的
  dirty/orphan issue 数量。
- `directRevertEligible` 统计当前摘要动作里支持直接 `sync revert` 的
  dirty/orphan issue 数量。
- `bundleMatchRequired` 统计那些直接路径仍然受
  `bundleMatchesRecordedDesiredStateDigest` 护栏约束的 dirty/orphan issue 数量。

这些计数只从主要的摘要 issue 列表推导，不会把更窄的
`policyEligibleOrphanObjects` 子集重复算进去。

同一组计数现在也会体现在 `Observed` condition 的 message 里，所以就算
operator 还没展开结构化摘要字段，也能先从一句紧凑文案里读到当前的
direct-action 概况。

## 常见 operator 模式

### 1. 本地 overlay drift

- 看：
  - `operatorAction: commitOrReapplyLocalOverlay`
- 常见路径：
  - 如果当前 live 变更是你想保留的，就用 `sync commit`
  - 如果当前 live 变更应该丢弃，就用 `sync revert`

### 2. policy-eligible orphan drift

- 看：
  - `operatorAction: promoteToLocalPatch`
  - 通常会出现在 `policyEligibleOrphanObjects`
- 常见路径：
  - 不要把它当成直接 `commit` / `revert` 工作流
  - 正确路径是写或调整 local repo patch，再 rerender / reapply

### 3. 直接 global baseline drift

- 看：
  - `operatorAction: revertOrUpdateGlobalBaseline`
- 常见路径：
  - 如果当前发行版 global baseline 仍然正确，就用 `sync revert`
  - 否则就修改 package 或 BOM global baseline，再重新生成 desired
    state

### 4. 由本地 bootstrap input 驱动的 generated drift

- 看：
  - `operatorAction: updateLocalInputAndRerender`
- 常见路径：
  - 更新 feeding generator input 的本地 bootstrap input
  - 运行 `sync render`
  - 运行 `sync apply`

### 5. 由 package 或 BOM global baseline 驱动的 generated drift

- 看：
  - `operatorAction: rerenderOrUpdateGlobalBaseline`
- 常见路径：
  - 修改选中的 global baseline
  - 重新 render desired state
  - 重新 apply bundle

### 6. 需要手工 review 的 drift

- 看：
  - `operatorAction: manualReview`
- 常见路径：
  - 先手工检查 generated projection 或当前不支持自动路由的 drift，再决定是否改 desired 或 live state

## 当前边界

这份速查只描述当前单节点 MVP。

它不意味着已经支持：

- 多节点 target resolution
- controller-driven continuous reconciliation
- 超出 `repairable=true` control-plane host-path 子集的 direct generated-projection revert
- 完全外置化的 ownership-policy 对象

这些仍然是后续工作。
