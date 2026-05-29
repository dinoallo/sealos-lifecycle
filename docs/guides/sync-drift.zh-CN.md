# 说明文档：使用 `sealos sync` 检查和处理 Drift

## 状态

当前仓库 walkthrough

## 概述

这份 walkthrough 解释当前单节点仓库里，`sealos sync` 这一组命令是如何处理
desired-state drift 的。

它刻意描述的是今天已经存在的实现，不是未来的 controller 设计。按当前仓库，
运维人员已经可以：

- 用 `sealos sync diff` 查看原始 drift
- 用 `sealos sync status` 查看按 ownership 分组后的 drift
- 用 `sealos sync commit` 持久化受支持的本地 drift
- 用 `sealos sync revert` 把 live state 拉回当前 desired state

这份 walkthrough 只聚焦当前真正有用的边界：

- `global` object 或 host file drift 会被当成 `globalBaseline`-owned
- `local` object 或 host file drift 会被当成 local-owned
- generated projection 会被报告，但当前不会被直接 `commit` 或 `revert`

## 相关文档

- drift tracking 与 compare 模型：
  [Materialization and drift](../architecture/materialization-and-drift.md)
- ownership 模型：
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- local repo 布局与 Secret 处理：
  [Local repo and secret](../guides/local-repo-and-secret.md)
- 当前 sync CLI：
  [cmd/sealos/cmd/sync.go](../../cmd/sealos/cmd/sync.go)
- 当前 compare 逻辑：
  [pkg/distribution/compare/compare.go](../../pkg/distribution/compare/compare.go)
- 当前 sync 命令测试：
  [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go)
- 最小示例目录：
  [docs/examples/sync-drift-minimal](../examples/sync-drift-minimal/README.zh-CN.md)
- 示例 bundle fixture：
  [docs/examples/sync-drift-minimal/bundle/bundle.yaml](../examples/sync-drift-minimal/bundle/bundle.yaml)
- 示例 applied revision fixture：
  [docs/examples/sync-drift-minimal/applied-revision.example.yaml](../examples/sync-drift-minimal/applied-revision.example.yaml)
- 示例 `sync diff` snapshot：
  [docs/examples/sync-drift-minimal/sync-diff.example.yaml](../examples/sync-drift-minimal/sync-diff.example.yaml)
- 示例 `sync status` snapshot：
  [docs/examples/sync-drift-minimal/sync-status.example.yaml](../examples/sync-drift-minimal/sync-status.example.yaml)

这个示例目录现在也同时带了 cluster-local 和 rendered 两份 policy artifact：

- `local-repo/policy/local-patch-policy.yaml`
- `bundle/policy/local-patch-policy.yaml`

这点很重要，因为当前 `sync diff` / `sync status` 的输出已经会直接暴露当前
rendered `localPatchPolicy` 的 provenance。

## 这份 Walkthrough 的前提

这份 walkthrough 假设：

- 你已经有一份 rendered bundle 目录
- 集群已经有记录下来的 `AppliedRevision`
- 你有可用的 kubeconfig
- 如果你要持久化本地 drift，你还需要这台集群对应的 local repo
- 如果你要检查 host file drift，而且宿主机根目录不是 `/`，你会显式传
  `--host-root`

当前命令族大致是：

```bash
sealos sync diff
sealos sync status
sealos sync commit
sealos sync revert
```

### Runtime Root 覆盖

默认情况下，`sync` 会从 sealos 默认 runtime root 读取目标 cluster 的
recorded state、current bundle 和 Clusterfile inventory。测试、smoke 或脚本化
流程里，可以在具体 `sync` 子命令上显式传 `--runtime-root`，让这些状态读取都固定
到同一个 root：

```bash
sealos sync apply \
  --cluster demo \
  --bundle-dir /tmp/rendered-bundle \
  --runtime-root /tmp/sealos-sync-runtime-root
```

这不会改变 package 语义，只控制 `sync` 从哪里解析 cluster-local runtime state。

## 先用这张判断表

在选命令之前，先按这张表判断：

| Drift 类型 | 典型状态 | 正常 owner | 正常下一步 |
| --- | --- | --- | --- |
| package-owned Kubernetes object | `Orphan` | `globalBaseline` | 回退它，或者去更新 global baseline |
| local-owned Kubernetes object | `Dirty` | `localOverlay` | 如果是有意改动就 commit，否则 revert |
| package-owned direct host file | `Orphan` | `globalBaseline` | 回退它，或者去更新 global baseline |
| local input 绑定出来的 direct host file | `Dirty` | `localInput` | 如果是有意改动就 commit，否则 revert |
| 缺失的 local-owned object 或 file | `Dirty` | `localOverlay` 或 `localInput` | 用 revert 恢复；当前 MVP 不会 commit 缺失 projection |
| generated static Pod projection | 通常是 `Orphan` | `localInput`、`globalBaseline` 或 `manualReview` | 跟着 remediation hint 走；当前 MVP 不会直接 commit/revert |

一个实用规则是：

- `commit` 用来接受受支持的、local-owned 的有意 drift
- `revert` 用来把 live state 拉回当前记录的 desired state

## 第 1 步：先用 `sync diff` 看原始 Drift

先看原始 compare 视图：

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf
```

如果你还想连 host file drift 一起看：

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

`sync diff` 最擅长的是：

- 展示原始 `currentCompare` payload
- 列出精确的 mismatch path，例如
  `spec.template.spec.containers[name=cilium-agent].image`
- 把 object 或 host path 级的 remediation guidance 一起带出来
- 告诉你这次是否把 observed summary 写回了 state

当前输出里最关键的顶层字段是：

- `currentState`
- `localPatchPolicy`
- `currentCompare`
- `observationPersisted`
- `persistedObservedSummary`
- `recordedRevision`

当你最想回答下面这些问题时，用 `sync diff`：

- 到底什么 drift 了
- drift 在哪条 path 上
- 这条 path 归谁拥有
- 现在安全可用的命令是什么

如果你想对照一份和当前 schema 对齐的快照，可以直接看：

- [docs/examples/sync-drift-minimal/sync-diff.example.yaml](../examples/sync-drift-minimal/sync-diff.example.yaml)

## 第 2 步：再用 `sync status` 看 Ownership 摘要

接着切到按 ownership 分组的视图：

```bash
sealos sync status \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

`sync status` 最擅长的是：

- 汇总 `Dirty` 和 `Orphan`
- 把 drift 分成：
  - `dirtyObjects`
  - `orphanObjects`
  - `dirtyHostPaths`
  - `orphanHostPaths`
- 单独指出 `mixedOwnershipObjects`
- 把当前 live 摘要和记录中的摘要并排放出来

当前输出里最关键的顶层字段是：

- `recordedState`
- `recordedObservedSummary`
- `currentState`
- `localPatchPolicy`
- `summary`
- `mixedOwnershipObjects`
- 带 remediation 的分组问题列表

当你最想回答下面这些问题时，用 `sync status`：

- 这次 drift 主要是本地 drift 还是 `globalBaseline` drift
- 哪些对象是 mixed ownership
- 我现在应该想的是 `commit` 还是 `revert`

如果你想对照一份和当前 schema 对齐的快照，可以直接看：

- [docs/examples/sync-drift-minimal/sync-status.example.yaml](../examples/sync-drift-minimal/sync-status.example.yaml)

## 第 3 步：动作前先看 Remediation Block

当前 `sync diff` 和 `sync status` 的输出里，受支持的 drift projection 都会带
一个 remediation block。

你最该看的字段是：

- `action`
- `changeOwner`
- `nextSteps[]`
- `allowedCommands[]`
- `commandGuidance[]`

今天的 ownership 路由规则是：

- `changeOwner=globalBaseline`
  - 说明 drift 应该通过恢复或更新选中的 package/BOM global baseline
    来解决
- `changeOwner=localOverlay`
  - 说明 drift 属于 local repo 里的 `patches/` 或 `resources/`
- `changeOwner=localInput`
  - 说明 drift 属于某个已声明的 local input binding，常见于 host-side 文件
- `changeOwner=manualReview`
  - 说明 Sealos 还不能安全自动归类这个 generated projection

这里的 command guidance 不是静态列表，它会被实际求值。当前单节点 MVP 里，
最重要的前提是：

- `bundleMatchesRecordedDesiredStateDigest`

如果这个前提不满足，`sync commit`、`sync revert`、`sync apply`
这些命令可能会显示为 `blocked`。

一个实用规则是：

- 如果 `sync diff/status` 说这个命令是 `blocked`，先停下来，看清楚为什么
  当前 bundle 已经和 recorded desired state 脱节，再决定是否修改 live state

## 第 4 步：接受有意的本地 Drift

只有在 drift 是受支持的、local-owned、而且你明确想接受时，才用
`sync commit`：

```bash
sealos sync commit \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --local-repo docs/examples/sync-drift-minimal/local-repo \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

当前单节点 MVP 能 commit 的有：

- 来自 tracked `localPatch` fragment 的 `Dirty` local-owned Kubernetes
  object drift
- 来自 `local-repo/resources/**` 的 `Dirty` standalone local-owned
  resource drift
- 来自已声明 local input binding 的 `Dirty` local-owned direct host-file
  drift

当前 multi-node 边界额外要注意的是：

- 同一个 local-input host file 在 `sync diff/status` 里可能会按 host
  出现多次
- 如果这些 drifted host 上的 live 内容其实相同，`sync commit` 仍然可以
  安全地选出这一个值
- 如果不同 host 上的 live 内容不同，`sync commit` 不会替你猜，而是要求
  显式指定 `--host`
- 当使用 `--host` 且该 host 有 host-scoped input binding 时，commit 会回写到
  host-scoped local repo input，以及 bundle 内的
  `components/<component>/host-inputs/<host>/...` 副本
- 当使用 `--host` 但该 host 没有 host-scoped input binding 时，commit
  仍会拒绝 divergent multi-host content，而不是把某一个节点的值写进默认 input
- `sync diff/status` 现在也会把这类情况单独汇总成
  `localInputHostSplits`，不用再从重复的 dirty host-path 行里自己推断
- 对这些路径，`sync diff/status` 还会暴露 host-scoped input provenance：
  `dirtyHostPaths` 可能会带 `usesHostScopedInput` 和 `hostInputBindingPath`，
  `localInputHostSplits` 也会区分哪些 host 已经有 scoped payload，哪些仍然
  使用默认 rendered payload

示例：

```bash
sealos sync commit \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --local-repo docs/examples/sync-drift-minimal/local-repo \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --host 192.168.0.240:22
```

当前不会 commit 的有：

- `Orphan` drift
- generated projection
- 缺失的 local-owned projection
- 基于 symlink 的 local host path
- 任意 global baseline 变更
- 没有 `--host` 的歧义 multi-node local-input host drift

适合用 `commit` 的场景是：

- “是的，这个本地改动本来就是有意的”
- “现在应该让 local repo 接住这个更新后的值”

## 第 5 步：回退不想要的 Drift

如果正确答案是：

- “不，这个 live state 应该回到当前记录的 desired state”

那就用 `sync revert`。

### 回退当前追踪到的全部 Drift

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### 只回退 Local-Owned Drift

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --scope local
```

### 只回退一个 Object

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --kind Secret \
  --namespace default \
  --name grafana-admin-credentials
```

### 只回退一个 Host Path

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --host-path /etc/kubernetes/kubeadm.yaml \
  --scope local
```

当前 MVP 里要记住几件事：

- 缺失的 local-owned object 或 file 可以被 `revert` 恢复
- generated projection 会被报告，但不会被直接回退
- local-scope revert 会拒绝明显属于 global-owned 的选择

## 第 6 步：再跑一遍 `diff` 或 `status`

无论你执行了 `commit` 还是 `revert`，都应该再跑一次只读命令：

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

或者：

```bash
sealos sync status \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

这之后你想看到什么，取决于刚才做了什么：

- 如果刚刚成功 `revert`，被选中的 drift 应该从 raw compare 和 summary 里消失
- 如果刚刚成功 `commit`，同一份 local-owned drift 也应该消失，因为现在 local
  repo 已经和 live state 对齐了

如果 drift 还在，接下来的 source of truth 就还是 remediation block：

- 它可能继续把你指回 `localOverlay`
- 也可能把你指回 `localInput`
- 也可能告诉你其实这已经是 `globalBaseline` 的问题

## 一条最小的 Operator 闭环

在当前 MVP 里，最短也最安全的闭环是：

1. 先跑 `sync diff`，看原始 mismatch path。
2. 再跑 `sync status`，看 ownership 摘要。
3. 按 remediation 决定动作：
   - `localOverlay` 或 `localInput` -> 如果是有意改动就 `commit`，否则
     `revert`
   - `globalBaseline` -> 通常直接 `revert`；只有你真的在修 package/BOM
     global baseline 时才去改 global baseline
4. 再跑一次 `sync diff` 或 `sync status`。

这条闭环已经足够覆盖当前单节点场景里的：

- local Secret 或 ConfigMap drift
- local patch drift
- 来自已声明 local input 的 direct host-file drift
- 本来就应该被丢弃的 `globalBaseline`-owned object 或 file drift

如果你想直接看一套和这条闭环对应的最小样例目录，可以用：
[docs/examples/sync-drift-minimal](../examples/sync-drift-minimal/README.zh-CN.md)。

## 当前 MVP 的边界

这份 walkthrough 只描述当前仓库行为。当前 MVP 仍然没有：

- controller 驱动的多节点 rollout 和持续 reconcile
- 对 generated projection 的直接 `commit` 或 `revert`
- 通过 `commit` 去接受任意 global baseline 变更
- 完整的、后台持续运行的 controller reconcile loop

所以更准确的心智模型是：

- 仓库里已经有一个可工作的单节点 operator loop
- 也有一条边界很窄的 CLI 驱动 multi-node `sync apply` 路径
- 但它仍然不是完整自治的 distribution agent
