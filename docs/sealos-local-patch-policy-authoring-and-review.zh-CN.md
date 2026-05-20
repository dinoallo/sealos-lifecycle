# 说明文档：Local Patch Policy 的编写与评审

## 状态

面向当前单节点 MVP 的说明文档

## 摘要

这份文档定义谁来编写 `LocalPatchPolicy`、哪些改动算安全或高风险，以及这些改动在
进入 rendered bundle 之前应该如何评审。

它建立在当前已经定下来的 source-and-scope 设计之上：

- policy scope 是 `clusterLocal`
- 当前支持 `localRepo`、`bom`、`package` 和 `builtInDefault`
- rendered bundle 会携带生效中的 policy artifact 及其 provenance

这份文档回答的是下一层更操作化的问题：

- 谁可以改这份 policy
- reviewer 应该重点看什么
- policy 改动在被接受前至少应该做哪些验证

## 相关文档

- Local patch policy 的 source 与 scope：
  [sealos-local-patch-policy-design.md](./sealos-local-patch-policy-design.md)
- local repo 布局与 Secret 处理：
  [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md)
- tracking 与 drift 模型：
  [sealos-materialization-tracking-and-drift-detection-model.md](./sealos-materialization-tracking-and-drift-detection-model.md)
- operator action 速查：
  [sealos-sync-operator-action-reference.md](./sealos-sync-operator-action-reference.md)

## 当前的编写边界

在当前 MVP 里，`LocalPatchPolicy` 可以在这些地方编写：

- `local-repo/policy/local-patch-policy.yaml`
- 由 `BOM.spec.localPatchPolicy` 引用的一份 BOM-selected policy 文件
- 由 `ComponentPackage.spec.localPatchPolicy` 引用的一份 component-package
  policy 文件

这意味着：

- cluster operator 可以在 local repo 里编写或调整 cluster-local policy
- BOM author 可以为一次 rendered revision 选择一份经过评审的 cluster-local
  policy
- package author 可以在 package artifact 里携带一份经过评审的 cluster-local
  policy，但除非 BOM 或 local repo 选择了生效 policy，否则一次只允许一个被选中
  package 这么做
- 当前没有任何 source 可以定义 package/BOM-scoped policy

这直接来自当前设计边界：

- package/BOM 定义 shared baseline
- local patch policy 定义 cluster-local override budget

## 一次 Policy 变更到底意味着什么

每次 policy 变更大体都会落在下面三类之一。

### 1. 收紧型变更

例子：

- 删除一个 `allowedPrefix`
- 删除一个支持的 kind
- 新增一个 forbidden path

运维效果：

- 某些原本能通过校验的 local patch 现在会被拒绝
- 现有 cluster-local drift 可能变成不能再被 commit

评审姿态：

- 从 shared-baseline 角度看相对安全
- 从 operator 连续性角度看有兼容性风险

### 2. 放宽型变更

例子：

- 新增一个 `allowedPrefix`
- 新增一个支持的 kind
- 去掉一个原本 forbidden 的字段

运维效果：

- Sealos 现在允许更大的 local override surface
- 更多 drift path 可能变成 `policyEligible`
- 更多 `Orphan` drift 可能变成可 `promoteToLocalPatch`

评审姿态：

- 这是当前模型里风险最高的一类变更
- 必须给出明确理由

### 3. 重构型变更

例子：

- 调整规则顺序
- 补注释
- 只改 YAML 写法，不改变实际语义

运维效果：

- 预期没有行为变化

评审姿态：

- 最好和放宽/收紧类改动拆开提交，不要混在一起

## 评审问题

每次 policy review 都应该回答下面这些问题。

### Scope 与 Ownership

1. 这次改动是否仍然符合 `spec.scope: clusterLocal`？
2. 这次提议是在放宽 cluster-local override surface，还是只是在收紧？
3. 这个 path 真的是 cluster-local 语义，还是在借 local policy 偷渡一个
   shared baseline 决策？

### 行为意图

4. 到底是什么 operator use case 需要放开这个 path？
5. 如果很多集群都需要这个 path，那它是不是更应该成为 package/BOM baseline
   改进？
6. 这次改动会不会允许 operator 过于宽泛地修改 workload identity、rollout
   语义或安全姿态？

### 现有 Drift 与兼容性

7. 这会不会破坏已经存在于 `local-repo/patches/**` 下的 local patch？
8. 这会不会改变当前 `sync diff` 对 drift 的分类，比如把原来不属于
   `policyEligible` 的 object path 变成可命中的 path？
9. 这会不会以 operator 不容易预期的方式改变 `sync commit` 的可用性？

## 评审 Checklist

当你不想重新读完整篇说明、只想做一轮 yes/no 式评审时，可以直接用这张短表。

| 检查项 | 可以接受时 | 应该拒绝时 |
| --- | --- | --- |
| Scope | 这次改动仍然符合 `spec.scope: clusterLocal`。 | 这份提议其实想要 package/BOM-scoped 行为。 |
| Ownership 边界 | 这个 path 的确属于 cluster-local。 | 这个 path 本质上是在偷渡 shared baseline 决策。 |
| 变更类型 | 评审里明确写了这是放宽、收紧，还是纯重构。 | 这次改动在悄悄放宽 local override surface。 |
| Operator use case | 文档里写清楚了一个具体的 cluster-local 用例。 | 理由只是“也许以后 operator 会想要这个”。 |
| 安全边界 | image、selector、status、server-managed metadata 这些禁区仍然没被放开。 | policy 开始过宽地放开 identity、rollout 或 control-plane 相关字段。 |
| 现有 local patch | 已检查 `local-repo/patches/**` 的兼容性。 | 这次改动可能让现有 local patch 悬空，但评审里没提。 |
| Drift 语义 | 已说明 `policyEligible`、`promoteToLocalPatch` 或 `sync commit` 可用性会不会变化。 | 完全忽略 drift 分类副作用。 |
| Rendered provenance | rendered bundle 里记录的 source、scope、name、path、digest 仍然一致。 | bundle 携带的 policy provenance 缺失或对不上。 |
| 正例验证 | 至少验证了一个本来应该通过的 patch。 | 没有正例验证。 |
| 反例验证 | 至少验证了一个本来应该失败的 patch 仍然会失败。 | 没有反例验证。 |

一个实用规则是：

- 只要是放宽型变更，如果说不出一个具体的 cluster-local 用例，以及一组正例/反例
  验证，就还不应该被接受

## 当前的硬性评审规则

在当前 MVP 里，reviewer 应该直接拒绝那些试图放开下面这些内容的 policy 变更：

- workload container image 修改
- selector 修改
- server-managed metadata
- status 更新
- 任何本质上属于 shared baseline 决策、而不是 cluster-local override 的 path

就算 YAML 形态看起来方便，这些边界也不应该被放开。

## 必需的变更说明

任何非平凡的 policy 变更都应该附一段简短、结构化的说明：

1. 为什么这个 path 必须是 local。
2. 这次改动是在放宽还是收紧 policy。
3. 影响的是哪个 component 或 object kind。
4. 改完之后，预期哪种 local patch 应该通过校验。
5. 改完之后，哪种无效 patch 仍然应该被拒绝。

这段说明可以放在 PR 描述、设计说明或 commit body 里，不需要新增 schema 字段。

为了方便仓库内直接复用，普通 PR 继续使用
[.github/PULL_REQUEST_TEMPLATE.md](../.github/PULL_REQUEST_TEMPLATE.md)，而
`LocalPatchPolicy` 相关 PR 可以改用专门的
[.github/PULL_REQUEST_TEMPLATE/local-patch-policy.md](../.github/PULL_REQUEST_TEMPLATE/local-patch-policy.md)
模板。

## 最低验证要求

在接受一份 policy 改动之前，最低限度应该做这些验证：

1. 先通过现有测试或代码路径验证 policy 文件本身。
2. render 一份携带这份 policy 的 bundle，并检查 rendered provenance：
   - `localPatchPolicySource`
   - `localPatchPolicyScope`
   - `localPatchPolicyName`
   - `localPatchPolicyPath`
   - `localPatchPolicyDigest`
3. 至少验证一个预期的正例。
4. 至少验证一个预期的反例。

按当前仓库的形态，这通常意味着下面这些组合之一：

- 稳定的仓库级入口 `make verify-local-patch-policy`
- 如果只是在排查 schema 或 fixture 失败，再针对
  `pkg/distribution/ownership` 跑定向 `go test`
- 如果只是在排查 provenance 处理，再针对
  `pkg/distribution/hydrate` 跑定向 `go test`
- 如果 operator 可见输出会受影响，再检查 `sync diff` / `sync status`
  的 fixture

当前轻量 CI lane 也直接复用了同一条入口，见
[.github/workflows/local_patch_policy_gate.yml](../.github/workflows/local_patch_policy_gate.yml)。

如果要在仓库本地按同样的 CI 语义做 dry run，可以用：

- `make verify-local-patch-policy-gate OLD_POLICY=... NEW_POLICY=... LOCAL_REPO=...`

按当前仓库实现，这条 gate 现在还会自动覆盖两类补充检查：

- 对 allowed / forbidden surface 的放宽与收紧做 policy impact analysis
- 检查现有 `local-repo/patches/**` 是否仍然和候选 policy 兼容

当前 workflow 还会在 job summary 里写出一份结构化 report，至少包含：

- 被比较的 old/new policy 身份
- 是否检测到 widening 和/或 narrowing
- 详细的 impact diff
- 如果存在的话，哪些现有 local patch 已经不再兼容

现在它还会额外写一段单独的 approval follow-up 摘要，让 reviewer 能一眼看出：

- 这次是否提供了 approval file
- 这次 gate 是否真的靠 approval 才通过
- 这条例外归谁负责、由谁批准
- approval 什么时候过期
- 当前更应该续签，还是应该在变更完成后移除

这份 report 现在通过 CLI 入口生成：

- `sealos sync policy-report --old-policy ... --new-policy ... --local-repo ...`

辅助脚本仍然可以作为仓库内工具保留，但它已经不再是 CI 中的主执行路径。

真正的 gate 现在使用更严格的 CLI 入口：

- `sealos sync policy-gate --old-policy ... --new-policy ... --local-repo ...`

当前 CLI 也把 approval 到期治理显式暴露成了两个开关：

- `--approval-expiry-warning-days`
- `--fail-when-approval-expires-soon`

当前 workflow 还会通过仓库内 helper
`go run ./scripts/local-patch-policy-base ...` 来解析 base policy artifact，
而不是继续在 shell 里内联那段 git lookup 逻辑。

如果这些阻断条件里有某一项是“有意识接受”的，当前可审计的例外路径是把
approval file 放在 cluster-local policy source 旁边，典型位置是：

- `local-repo/policy/local-patch-policy-approval.yaml`

这份 approval file 现在会同时绑定被比较的 old policy 和 candidate new
policy 的：

- `name`
- `scope`
- `digest`

它现在也必须带上一组治理元数据，让这条例外既可审计，又会自然过期：

- `owner`
- `approvedBy`
- `changeRef`
- `expiresAt`

每条被批准的 violation 现在还必须显式带上：

- `code`
- `expectedCount`
- `expectedImpact`
- `reason`

当这份 approval file 真的被 `sealos sync policy-gate` 消费时，当前 CLI 和
CI 输出也会直接把“这次为什么被放行”显式带出来，主要是：

- `gate.approvalSummary.approvalProvided`
- `gate.approvalSummary.approvalApplied`
- `gate.approvalSummary.owner`
- `gate.approvalSummary.approvedBy`
- `gate.approvalSummary.changeRef`
- `gate.approvalSummary.expiresAt`
- `gate.approvalSummary.expiresSoon`
- `gate.approvalSummary.daysUntilExpiry`
- `gate.approvalSummary.followUpAction`
- `gate.approvalSummary.approvedViolationCodes`
- `gate.approvedViolations[].impact`

这份文件当前只能显式批准两类 gate violation：

- `wideningChange`
- `incompatiblePatches`

当前 gate 还会直接执行 approval 的生命周期约束：

- 缺 `owner`、`approvedBy` 或 `changeRef` 会直接判 invalid
- 缺 `expiresAt` 会直接判 invalid
- `expiresAt` 已经过期时，即使 impact 本身匹配，也会直接拒绝

如果 approval 还没过期，但已经接近到期，当前 gate 还会额外给出 warning，并通
过 `gate.approvalSummary.followUpAction` 提示后续动作，例如在到期前续签或移
除这份 approval。

按当前仓库自动化的设定，轻量 CI lane 已经打开了更严格的 near-expiry 模式，
所以一旦 approval 接近到期，就会被当成 blocking condition，必须先续签或移
除，才能过 gate。

如果想看当前输出长什么样，可以直接看：

- [examples/sync-drift-minimal/policy-gate-approved.example.yaml](./examples/sync-drift-minimal/policy-gate-approved.example.yaml)

approval 的卫生状态现在也会在 policy 变更之外被持续检查。当前仓库还提
供了一个按时间巡检的 scanner：

- `sealos sync policy-approval-scan --root ...`
- `make verify-local-patch-policy-approvals APPROVAL_SCAN_ROOT=...`

它当前的语义是：

- 非法 approval 文件是 blocking
- 已过期 approval 文件是 blocking
- 临近过期 approval 默认只给 warning
- 开启 `--fail-when-approval-expires-soon` 后，临近过期 approval 也会变成
  blocking

仓库还会通过下面这条 workflow 定时跑这条 scanner：

- [.github/workflows/local_patch_policy_approval_scan.yml](../.github/workflows/local_patch_policy_approval_scan.yml)

这条定时 lane 当前启用了严格的 near-expiry 模式，所以即使没有人在改
policy，接近过期的 approval 也会被持续暴露出来。

按当前 MVP，默认 gate 语义是：

- 只要候选 policy 扩大了 cluster-local override surface，就 fail
- 只要候选 policy 会让现有 `local-repo/patches/**` 失效，就 fail
- narrowing change 仍然会作为 warning 暴露出来，但不会单独导致失败

## 当前推荐的编写闭环

当前 MVP 下最小且安全的闭环是：

1. 编辑 `local-repo/policy/local-patch-policy.yaml`。
2. render 一份 bundle。
3. 检查 render 后的 `bundle/policy/local-patch-policy.yaml`。
4. 确认 bundle 里的 provenance 字段和 rendered artifact 一致。
5. 验证一个本来应该通过的 patch 仍然能通过。
6. 验证一个本来应该失败的 patch 仍然会失败。
7. 再检查一次：这个改动真的应该留在 local policy，还是其实应该进 shared
   baseline 设计。

## 什么情况下不该改 Policy

下面这些问题，不应该通过改 `LocalPatchPolicy` 来解决：

- package 缺了一个正式 extension point
- shared baseline 默认值本来就应该在中心侧改好
- package/BOM release 决策本身有问题
- generated projection 的问题本来应该通过改 input 或 baseline 解决，而不是通过
  扩大 local patch surface 来掩盖

遇到这些情况，改 policy 只会把真正的边界问题藏起来。

## 当前最实用的规则

把 `LocalPatchPolicy` 理解成 cluster-local override budget，而不是 package 或 BOM
ownership 的逃生门。

如果某个提议给人的感觉只是“也许哪天 operator 会需要这个 path”，那在当前 MVP
里还不够。放宽 local policy 必须绑定到一个明确的 cluster-local use case，
以及一组具体可验证的例子。
