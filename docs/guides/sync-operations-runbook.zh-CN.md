# Sync 运维 Runbook

## 状态

当前运维契约

## 目的

这份 runbook 把 `sealos sync diff`、`sealos sync status` 和
`operatorAction` 字段收敛成常规运维入口。当集群出现 drift、controller target
报告 `Degraded`，或告警需要稳定摘要、工单字段和第一条修复命令时，使用这份文档。

## 分诊循环

1. 先抓取紧凑状态：

   ```bash
   sealos sync status \
     --cluster <cluster> \
     --bundle-dir <bundle> \
     --kubeconfig <kubeconfig> \
     --host-root /
   ```

2. 把 `headline` 复制到告警标题、工单标题或 dashboard 行。
3. 把 `summary`、`operatorActionSummary`、`localPatchPolicy` 和
   `topologyStatus` 复制到工单正文。
4. 打开 `sync diff` 查看原始 mismatch path：

   ```bash
   sealos sync diff \
     --cluster <cluster> \
     --bundle-dir <bundle> \
     --kubeconfig <kubeconfig> \
     --host-root /
   ```

5. 按下面第一条匹配的 `operatorAction` 路径处理。
6. 每次执行 `commit`、`revert`、`render` 或 `apply` 后重新运行
   `sync status`。

## 告警标题

告警标题使用顶层 `headline` 字段。它面向短、稳定、基于计数的摘要：

```text
state=Orphan; dirtyObjects=0; orphanObjects=2; dirtyHostPaths=8; orphanHostPaths=48; directCommitEligible=8; directRevertEligible=57; bundleMatchRequired=57
```

如果告警系统需要确定性 severity，从结构化字段推导，不要解析自由文本 remediation：

| 条件 | 建议 Severity | 原因 |
| --- | --- | --- |
| `currentState=Clean` | clear | Desired state 和 live state 匹配。 |
| `currentState=Dirty` 且 `directCommitEligible>0` 或 `directRevertEligible>0` | warning | 已有 operator action，但 live state 偏离 recorded desired state。 |
| `currentState=Orphan` 且 `orphanObjects>0` 或 `orphanHostPaths>0` | warning | global baseline 或未记录 live state 需要 review。 |
| `operatorAction=manualReview` | critical | CLI 不能安全分类或修复该 drift。 |
| `topologyStatus.state!=matched` | critical | bundle 是按另一个集群拓扑渲染的。 |
| `renderInputStatus.state!=matched` | critical | 正在检查的 bundle 已不匹配 recorded render inputs。 |

## Dashboard 摘要

每个集群一行，并保持这些列稳定：

| 列 | 来源字段 |
| --- | --- |
| Cluster | `clusterName` |
| State | `currentState` |
| Headline | `headline` |
| Revision | `bomName`、`revision`、`channel` |
| Desired digest | `desiredStateDigest` |
| Local repo revision | `localRepoRevision` |
| Drift counts | `summary.clean`、`summary.dirty`、`summary.orphan`、`summary.total` |
| Direct actions | `operatorActionSummary.directCommitEligible`、`directRevertEligible`、`bundleMatchRequired` |
| Local patch policy | `localPatchPolicy.source`、`scope`、`name`、`digest` |
| Topology | `topologyStatus.state` |
| Render inputs | `renderInputStatus.state` |
| Last observation | `recordedObservedSummary.lastObservedTime` |

不要把 Secret object data、kubeconfig 内容或 local repo resource 字节放进 dashboard。
object name、kind、namespace、path、digest 和计数可以展示。

## 工单字段

每个工单都要保留足够结构化上下文，确保别人能复现同一个判断：

| 工单字段 | 必填值 |
| --- | --- |
| `cluster` | `clusterName` |
| `state` | `currentState` |
| `headline` | 顶层 `headline` |
| `bom` | `bomName`、`revision`、`channel` 和 `desiredStateDigest` |
| `localRepoRevision` | 顶层 `localRepoRevision` |
| `topologyStatus` | state；不匹配时附 mismatch message |
| `renderInputStatus` | state；不匹配时附 mismatch message |
| `operatorActionSummary` | direct commit、direct revert 和 bundle-match 计数 |
| `primaryOperatorAction` | 按严重程度选出的第一个 issue 的 `operatorAction` |
| `primaryRemediation` | 第一个 issue 的 `remediation.action` 和 `nextSteps[]` |
| `affectedObjects` | kind、namespace、name 和 changed paths |
| `affectedHostPaths` | host、path、component、ownership 和 projection class |
| `evidence` | 抓取 `sync status` 和 `sync diff` 的命令、时间、operator 和 artifact path |

把原始 `sync status -o yaml` 和 `sync diff -o yaml` 作为附件保存。外部系统如果展开了
object payload，需要自行 redaction Secret 值。当前 `sync` 摘要本身应该避免复制
Secret 字节。

## 常见修复路径

| `operatorAction` | 第一反应 | 何时升级 |
| --- | --- | --- |
| `commitOrReapplyLocalOverlay` | 如果 live local object 变更是有意的，运行 `sync commit`；否则运行 `sync revert`。 | object 包含 data-plane state 或 Secret 字节，需要 owner approval。 |
| `commitOrReapplyLocalInput` | 如果 live host file 变更是有意的，运行 `sync commit`；否则运行 `sync revert`。多节点 drift 中同一 input 按 host 不同时，传 `--host`。 | hosts 之间内容不一致且没有 host-scoped input binding。 |
| `promoteToLocalPatch` | 新增或更新 cluster-local patch，rerender 后 apply。 | changed path 不在当前 `LocalPatchPolicy` 覆盖范围内。 |
| `revertOrUpdateGlobalBaseline` | 选中的 baseline 正确时运行 `sync revert`；否则更新 package 或 BOM baseline 并 rerender。 | path 属于 on-call 团队之外的 package owner。 |
| `updateLocalInputAndRerender` | 更新 local bootstrap input，运行 `sync render`，再运行 `sync apply`。 | generated projection 涉及 data-plane sensitive 内容，或没有 owner-approved input model。 |
| `rerenderOrUpdateGlobalBaseline` | 更新 package 或 BOM baseline，运行 `sync render`，再运行 `sync apply`。 | package revision 必须走 release/promotion approval。 |
| `manualReview` | 停止自动修复，检查 live projection、package source 和 local repo owner。 | 所有 mutation 前都要升级处理。 |

## 命令护栏

- `topologyStatus.state` 不是 `matched` 时，不要运行 `sync commit`、`sync revert`
  或 `sync apply`。
- 相关 `commandGuidance` 条目是 `blocked` 时，不要执行 direct command。
- 把 `requiresBundleMatch=true` 当成硬前提：正在检查的 bundle 必须匹配
  `desiredStateDigest`。
- 只想触碰 cluster-local overlay 或 input 时，使用 `--scope local`。
- 用 `--kind`、`--namespace`、`--name`、`--host-path`、`--host` 和
  `--component` 缩小 destructive action 的范围。
- 不要用 `sync commit` 处理 package-owned global baseline drift；这类变更应该通过
  release path 更新 package 或 BOM。

## 应保留的证据

每个 incident 或维护工单都应保留：

- `sync status -o yaml`
- `sync diff -o yaml`
- 如果有状态变更，保留执行的命令
- 变更后的 `sync status -o yaml`
- 引用到的 local repo patch/input paths
- 引用到的 package 或 BOM revision paths
- controller 发起 reconcile 时的 rollout 或 controller target events

这些证据也是 promotion gate 和事后复盘的输入：它证明选中了哪个 revision、当时有什么
drift、走了哪条 owner path，以及集群是否回到预期状态。
