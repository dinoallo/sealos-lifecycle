# 说明文档：BOM、Revision 与 ReleaseChannel 的语义

## 状态

带实现注释的设计说明

## 概述

这份文档专门解释 Sealos 里这几层对象之间的关系：

- `ComponentPackage` revision
- `BOM` revision
- `distribution line`
- `ReleaseChannel`
- Day 0 / Day 1 的目标版本选择

之所以单独写这一份，是因为这些概念现在分散在几份设计文档里，而当前 PoC
代码仍然使用一个更简单的过渡模型：`spec.channel` 还直接写在 BOM 里。当前仓库
现在也支持一条很窄的本地文件 `ReleaseChannel` 路径，用来在 render 前选择
BOM。

## 相关文档

- 顶层架构：
  [Distribution and config sync](../architecture/distribution-and-config-sync.md)
- release 与 promotion：
  [Release and promotion](../architecture/release-and-promotion.md)
- ownership 与 drift：
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- 派生发行版：
  [Derived distribution](../guides/derived-distribution.md)
- 当前 BOM schema：
  [pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go)
- applied revision state：
  [pkg/distribution/state/types.go](../../pkg/distribution/state/types.go)
- 当前 materialize 路径：
  [pkg/distribution/reconcile/materialize.go](../../pkg/distribution/reconcile/materialize.go)
- 当前本地 `ReleaseChannel` resolver：
  [pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go)

## 为什么需要这份文档

这份文档要把下面这些问题放到一个地方讲清楚：

- 什么是 BOM？
- 什么是 BOM revision？
- BOM 和 distribution line 的区别是什么？
- 集群在 Day 0 应该选择什么？
- 什么是 `ReleaseChannel`，为什么它应该和 BOM 分开？
- 当前仓库已经实现了什么，哪些还只是设计目标？

## 核心对象

| 对象 | 含义 | 可变性 |
| --- | --- | --- |
| `ComponentPackage revision` | 一个通过 OCI digest 标识的不可变组件包 revision。 | 不可变 |
| `BOM component entry` | BOM 里的一个组件选择项，包含 version、artifact 和 dependency。 | 作为 BOM revision 的一部分不可变 |
| `BOM revision` | 一组 digest-pinned 的组件选择，定义一次具体可发布的 baseline snapshot。 | 不可变 |
| `Distribution line` | 一条具名的发行线，由一串 BOM revisions 构成，运维侧把它当成一个持续演进的 release family。 | 通过发布新 BOM revisions 演进 |
| `ReleaseChannel` | 一个可变的发布对象，声明某条 distribution line 上某个 channel 当前指向哪份 BOM revision。 | 可变 |
| `AppliedRevision` | 集群本地记录，保存这个集群最近一次 render / apply 的结果。 | 集群本地可变状态 |

最重要的规则是：

- package 是组件级 building block
- BOM revision 是一次具体 release snapshot
- distribution line 是这些 snapshot 按时间串起来的一条线
- `ReleaseChannel` 是这条线上的移动头，负责告诉集群“这个 rollout stage
  当前该跟哪一版”

## 什么是 BOM

BOM 是 release 对象，它回答的是：

- 这次 baseline 里有哪些组件
- 每个组件具体用哪一个 package image + digest
- 组件之间依赖顺序是什么
- 这一整套 baseline snapshot 的 revision 名是什么

当前 schema 定义在
[pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go)。

今天最关键的字段有：

- `metadata.name`
- `spec.revision`
- `spec.channel`
- `spec.packages[]`

### 推荐语义

这些字段最干净的读法是：

- `metadata.name`
  BOM family 名，或者说面向发行线的名字。正常情况下，同一条
  distribution line 上它应该保持稳定。
- `spec.revision`
  这一次具体 BOM snapshot 的不可变 revision 标识。
- `spec.packages[]`
  真正的组件图和 digest-pinned artifact 集合。
- `spec.channel`
  当前实现里的过渡字段，今天仍有记录价值，但不是最终理想的 release-head
  模型。

所以更推荐的命名方式是：

- `metadata.name: default-platform`
- `spec.revision: rev-007`

之后再变成：

- `metadata.name: default-platform`
- `spec.revision: rev-008`

如果派生出一条新的 distribution line，通常会连 BOM family 名一起变：

- `metadata.name: corp-platform`
- `spec.revision: rev-corp-001`

## BOM 不是什么

BOM 不是：

- local repo
- secret store
- runtime state snapshot
- drift record
- release channel head

这条边界很重要，因为 BOM 必须保持可评审、可复现。Secret 字节、本地 overlay
和运行时生成对象都不该放进去。

## 当前 BOM schema 的形状

当前 PoC 风格的 BOM 大致长这样：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: minimal-single-node
spec:
  revision: rev-poc-001
  channel: alpha
  localPatchPolicy: policy/local-patch-policy.yaml
  packages:
    - name: containerd
      category: infra
      version: v1.7.18
      artifact:
        name: containerd-runtime
        image: registry.example/platform/containerd-runtime:v1.7.18
        digest: sha256:<digest>
    - name: kubernetes
      category: infra
      version: v1.30.3
      dependencies:
        - containerd
      artifact:
        name: kubernetes-rootfs
        image: registry.example/platform/kubernetes-rootfs:v1.30.3
        digest: sha256:<digest>
```

当前重要规则是：

- `spec.revision` 必填
- `spec.channel` 今天仍然是必填
- `spec.packages` 必填
- `spec.localPatchPolicy` 可选；如果设置，它是相对于 BOM 文件所在目录的一份
  `LocalPatchPolicy`，并且优先级高于 package policy source
- 每个组件的 artifact digest 必填
- 依赖名必须引用同一个 BOM 里的其他组件名

这些校验都已经在
[pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go) 里实现了。

## 本地测试 Registry

本地验证 Distribution package 时，可以直接跑 upstream
[`distribution/distribution`](https://github.com/distribution/distribution)
registry，并通过 `registry.sealos.local:5000` 访问。这样 package image reference
会更接近生产里的 OCI 引用，同时不需要远端 registry 账号。

先加本机 hosts：

```bash
echo "127.0.0.1 registry.sealos.local" | sudo tee -a /etc/hosts
```

启动本地 registry 容器：

```bash
docker run -d --restart=always \
  --name sealos-local-registry \
  -p 5000:5000 \
  registry:2
```

上面的本地 registry 是 HTTP。仅用于测试的 `sealos sync package build/push`
可以给 Sealos/buildah 指定一份 insecure registry 配置：

```bash
cat > /tmp/sealos-local-registries.conf <<'EOF'
unqualified-search-registries = ["docker.io"]

[[registry]]
location = "registry.sealos.local:5000"
insecure = true
EOF
```

构建并推送 Kubernetes rootfs component package：

```bash
sealos --registries-conf /tmp/sealos-local-registries.conf \
  sync package build \
  --package-dir scripts/poc/minimal-single-node/packages/kubernetes \
  --image registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3 \
  --platform linux/amd64

sealos --registries-conf /tmp/sealos-local-registries.conf \
  sync package push \
  --image registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3 \
  --destination registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3
```

记录 `sync package push` 输出里的 digest，然后把 BOM 里的组件引用 pin 到本地
registry 镜像：

```yaml
artifact:
  name: kubernetes-rootfs
  image: registry.sealos.local:5000/sealos/kubernetes-rootfs:v1.30.3
  digest: sha256:<digest>
```

共享环境或生产环境应该使用 TLS 和正式 registry policy。这里的 insecure registry
配置只用于本地开发验证。

## 关于 `baseArtifacts`

BOM schema 里还有 `spec.baseArtifacts`，但当前 PoC 和大多数现有文档都围绕
`spec.packages` 在讲。

所以现在更实用的理解是：

- `components` 是当前第一层的主 release graph
- `baseArtifacts` 虽然存在于 schema 里，但还不是当前仓库文档的主线

这就是为什么大多数例子和设计讨论都集中在 `components` 上。

## 为什么说 BOM 里的 `spec.channel` 只是过渡字段

当前代码里 `spec.channel` 还直接挂在 BOM 里。这对 MVP 来说很简单，但不是长
期最干净的模型。

原因其实很直接：

- 同一份 BOM revision 可能先在 `alpha` 验证
- 之后升到 `beta`
- 最后再升到 `stable`

如果 `channel` 是 BOM 内部不可变属性，系统就会被推向两种都不太优雅的结果：

- 要么去改一份本来应该不可变的 BOM
- 要么把同一套 BOM 内容复制多份，只为了改 `channel`

这两种都不理想。

所以设计方向应该是：

- BOM revision 保持不可变
- 可变的 channel head 迁到 `ReleaseChannel`

## `ReleaseChannel` 应该表示什么

`ReleaseChannel` 这个对象只回答一个问题：

在这条 distribution line 上，这个 channel 当前指向哪份 BOM revision？

这是“发行线级”的决策，不是“package 级”的决策。

例如：

- `default-platform / stable` -> `rev-007`
- `default-platform / beta` -> `rev-009`
- `default-platform / alpha` -> `rev-012`

然后每一份 BOM revision 里面，仍然继续列出完整组件图和 package digests。

### 建议形状

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: default-platform-stable
spec:
  distribution: default-platform
  channel: stable
  targetRevision: rev-007
  bomPath: bom.yaml
  bomDigest: sha256:<bom-digest>
```

这个形状的职责边界很清楚：

- BOM 定义一个不可变 snapshot
- `ReleaseChannel` 告诉集群在某个 rollout stage 该跟哪个 snapshot

## 当前实现 vs 目标模型

| 主题 | 当前仓库行为 | 目标设计方向 |
| --- | --- | --- |
| 集群怎么选择目标 | 显式 BOM 文件、本地 `ReleaseChannel` 文件，或 release metadata source 加 `distribution line + channel` | 显式选 BOM revision，或做 `distribution line + ReleaseChannel` lookup |
| channel 元数据放哪里 | `BOM.spec.channel`；render provenance 会记录本地或 release-source channel 选择元数据 | 独立的 `ReleaseChannel` 对象 |
| `sync render` 今天解析什么 | 直接传入的 BOM 文档、本地 `ReleaseChannel`，或通过 `--release-source --release-line --channel` 解析出的 `ReleaseChannel` | 先做可选的 channel lookup，再落到具体 BOM revision |
| applied state 今天记录什么 | BOM name、revision、digest、`requestedTarget` 和 `resolvedTarget`；rendered bundle 还会记录 BOM 和 `ReleaseChannel` provenance | 同一份契约，release history 和 promotion evidence 由 release service 存储 |

这一点很关键，因为当前
[pkg/distribution/bom/channel.go](../../pkg/distribution/bom/channel.go)
会校验 channel 的 distribution 是否匹配目标 BOM 的 `metadata.name`，
`targetRevision` 是否匹配 BOM 的 `spec.revision`，然后 render 这份具体 BOM。
本地 `ReleaseChannel` 文件仍然支持；release lookup 路径也可以从 release metadata
source 按 `distribution line + channel` 解析，并要求 `spec.bomDigest`，确保 render
前的 BOM 是 digest-pinned。一个本地 release 目录可以通过只读 HTTP lookup API 暴露：

```bash
sealos sync release-metadata serve \
  --release-source /var/lib/sealos/distribution/releases \
  --listen 127.0.0.1:8080
```

这个 service 会通过 `GET /v1/distributions/{line}/channels/{channel}` 返回
`ReleaseChannel` 文档，并通过 `GET /v1/distributions/{line}/revisions/{revision}/bom`
返回选中的 BOM 文档。它也接受 health-gated promotion 请求：
`POST /v1/distributions/{line}/channels/{channel}/promotions`；请求里指定
`targetRevision`、提供通过的 `DistributionHealthProof`，service 会使用和
`sealos sync promote` 相同的 promotion policy 后再推进 channel 文件。

### Applied State Target 契约

每次新 render 生成的 `AppliedRevision` 都会同时记录运维侧请求的目标和最终解析到的
具体 BOM：

```yaml
spec:
  bom:
    name: default-platform
    revision: rev-007
    channel: stable
    digest: sha256:<resolved-bom-digest>
  requestedTarget:
    kind: releaseChannelLookup
    releaseSource: https://release.sealos.example
    distributionLine: default-platform
    channel: stable
    releaseChannelPath: https://release.sealos.example/v1/distributions/default-platform/channels/stable
  resolvedTarget:
    bom:
      name: default-platform
      revision: rev-007
      channel: stable
      digest: sha256:<resolved-bom-digest>
    releaseChannel:
      distributionLine: default-platform
      channel: stable
      targetRevision: rev-007
      source: https://release.sealos.example/v1/distributions/default-platform/channels/stable
```

`requestedTarget.kind` 只有三种取值：

- `bom`：显式 BOM 文件目标
- `releaseChannelFile`：本地 `ReleaseChannel` 文件
- `releaseChannelLookup`：通过 `distribution line + channel` 做 registry/API-backed lookup

`requestedTarget` 和 `resolvedTarget` 必须成对写入。旧的 state 文件如果没有这些字段
仍然可以加载，但新的 render/apply 路径会持久化两者；apply 成功后，
`status.lastSuccessfulRevision` 也会保留同一份 target metadata。

成功 apply 还会维护一个有上限、最新优先的 `status.successfulRevisions` 历史。
rollback 使用最后一次成功 revision snapshot，而不是失败的 desired target；因此只要集群
runtime store 里还保留对应 revision bundle，一次失败 upgrade 可以跨 BOM name、
distribution line、channel 和 local revision metadata 回滚到上一份成功目标。

同一套 policy 既可以通过本地 `sealos sync promote` 使用，也可以通过 release
metadata service 的 promotion endpoint 使用。两条路径都会把一份 `ReleaseChannel`
文件推进到目标 BOM，在推进前检查目标 channel policy，对 beta/stable 目标要求
health proof，并记录 approver、reason、timestamp、proof digest 和 promotion
history entry。

## Day 0 怎么选

在 Day 0，集群不应该从 package 内容或 live state 里反推 release target。它
应该被明确分配成下面两种之一：

1. 一个显式 BOM revision
2. 一个 `distribution line + channel`

### 推荐决策顺序

1. 先选 distribution line。
2. 决定这个集群是 pin 到一个显式 revision，还是跟着某个 channel 走。
3. 如果它跟 channel，就把这个 channel 解析成一份具体 BOM revision。
4. render 并 apply 这份 resolved BOM revision。
5. 把最终落地的 revision 记进 applied state。

### 实用的 cohort 建议

| 集群类型 | 常见 Day 0 选择 |
| --- | --- |
| 内部 bring-up / 激进试验 | `alpha` |
| canary / pilot | `beta` |
| 普通生产集群 | `stable` |
| 强监管或精确受控 rollout | 显式 pin 一个 BOM revision |

### 当前实现的边界

今天这个仓库实现了三种 target 路径：

- 选择一个具体 BOM 文件，传给 `sealos sync render --file`
- 选择一个本地 `ReleaseChannel` 文件，传给
  `sealos sync render --release-channel`
- 选择 release metadata source、distribution line 和 channel，传给
  `sealos sync render --release-source --release-line --channel`

本地 `ReleaseChannel` 必须声明 distribution line、channel、target
revision，以及目标 BOM 的 `spec.bomPath`。CLI 会先把 channel 解析到这份本地
BOM，再进入 materialization。

对于 HTTP(S) release source，Sealos 会请求
`/v1/distributions/{line}/channels/{channel}`，并期望返回一份
`ReleaseChannel` 文档。`ReleaseChannel` 必须包含 `spec.bomDigest`；如果获取到的
BOM digest 不匹配，lookup 会失败。

## 本地 Channel Promotion

在当前本地文件模型里，promotion 的含义是更新一份 `ReleaseChannel` 文档，
让它的 `spec.targetRevision` 和 `spec.bomPath` 指向同一条 distribution line 上
的另一份 BOM revision。

用法：

```bash
sealos sync promote \
  --release-channel channels/default-platform-stable.yaml \
  --target-bom boms/default-platform/rev-008.yaml \
  --health-proof proofs/default-platform-rev-008-health.yaml \
  --reason "beta cohort passed source preflight and rollout validation" \
  --approved-by release-team
```

这个命令会校验：

- channel 文档是有效的 `ReleaseChannel`
- 目标 BOM 是有效 BOM
- `ReleaseChannel.spec.distribution` 匹配 `BOM.metadata.name`
- 默认 promotion policy 允许目标 channel 前进到候选 BOM 的 source channel
- 如果目标 channel 要求 proof，`--health-proof` 必须指向有效的
  `DistributionHealthProof`，它必须指向同一条 line 和目标 BOM revision，并且
  `spec.passed: true`、包含 health signals，并满足 required-signal 和最少通过
  signal 数量阈值

然后它会写回更新后的 channel 文件，并追加一条
`spec.promotionHistory[]`，内容包括：

- 上一个 revision
- 新 revision
- 写入 channel 的 BOM path
- promotion reason
- approver
- approval timestamp
- 使用 `--health-proof` 时的 health proof path、digest 和 summary

当前本地文件 promotion policy 很小且确定：

| 目标 channel | 允许的候选 `BOM.spec.channel` | Health proof |
| --- | --- | --- |
| `alpha` | `alpha` | 不要求 |
| `beta` | `alpha`, `beta` | 要求 |
| `stable` | `beta`, `stable` | 要求 |

这会阻止未经验证的 `alpha` 候选直接跳到 `stable`，也会把 beta/stable
promotion 缺失 proof 视为 policy failure，而不是默认批准。

### 从 acceptance report 生成 proof

对于 package lifecycle automation，`sealos sync health-proof` 可以把最小
single-node smoke 流程产出的 `PackageAcceptanceReport` 转成 promotion 可用的
`DistributionHealthProof`：

```bash
sealos sync health-proof \
  --file boms/default-platform/rev-008.yaml \
  --acceptance-report workdir/acceptance-report.yaml \
  --output-file proofs/default-platform-rev-008-health.yaml \
  --summary "beta cohort passed apply and drift recovery validation"
```

生成出来的 proof 会使用 `--file` 指向的 BOM 里的 line 和 revision。它是保守
判定：只有 report 本身通过且 exit code 为 `0`、report 里的 BOM file、已
render 的 BOM line/revision 和 BOM digest 都匹配目标 BOM、已 render 的
`desiredStateDigest` 和 `localRepoRevision` 存在且是合法 digest、
source/runtime preflight 没有 blocking、确实跑过 mutating apply、post-apply
state 是 `Clean`、`revertCheck: true` 时 post-revert state 也是 `Clean`，并且
没有失败 stage，并且预期的 smoke/apply/revert acceptance stages 都存在、通过且
把会修改环境的步骤标记为 mutating，proof 才会是 `spec.passed: true`。只跑
safe smoke、没有 mutating apply 的 report 仍然可以生成 evidence，但会得到
`spec.passed: false`，不应该满足 beta/stable promotion policy。

生成出的 proof 也会对 promotion evidence 做规范化。每个阻断性 signal 都会带
`required: true`、`source: PackageAcceptanceReport`，以及指向 acceptance
report 字段或 stage 的 `evidenceRef`。`spec.thresholds.requiredSignals`
列出必须存在且通过的 signals，`spec.thresholds.minPassedSignals` 记录最少通过
signal 数量阈值，`spec.signalSummary` 保存评估后的计数。没有 thresholds 的旧
proof 仍然按严格兼容规则处理：所有 signals 都必须通过。

最小 health proof 形态如下：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionHealthProof
metadata:
  name: default-platform-rev-008-health
spec:
  line: default-platform
  targetRevision: rev-008
  passed: true
  summary: beta cohort passed rollout health checks
  collectedAt: "2026-05-20T10:30:00Z"
  thresholds:
    requiredSignals:
      - reconcile
      - node-readiness
    minPassedSignals: 2
  signalSummary:
    totalSignals: 2
    passedSignals: 2
    failedSignals: 0
    requiredSignals: 2
    passedRequiredSignals: 2
    minPassedSignals: 2
  signals:
    - name: reconcile
      passed: true
      required: true
      source: PackageAcceptanceReport
      evidenceRef: spec.stages[name=reconcile]
      message: all canary targets reconciled
    - name: node-readiness
      passed: true
      required: true
      source: PackageAcceptanceReport
      evidenceRef: spec.stages[name=node-readiness]
```

当 `sealos sync promote` 接受这份 proof 后，被推进的 channel 会尽量把目标
BOM path 写成相对于 channel 文件的路径。现有 render、validate、agent 和
controller 路径继续通过 `--release-channel` 或 `releaseChannelPath`
消费同一份 channel 文件。

`sealos sync promote` 的结构化输出也会返回 `policyDecision`。这个 decision
记录已评估的 transition、目标 channel rule、health-proof requirement、
required/missing/failed health signals、最少通过 signal 数量阈值，以及 policy
engine 给出的 warning 或 violation 字段。失败的 decision 会在 channel 文件写入前阻塞。

## Day 1 到 Day N 应该怎么表现

Day 0 完成后，集群的后续行为应该取决于它是 pin 模式还是 channel-following
模式。

### Pin 到显式 revision

如果一个集群 pin 到了某个 BOM revision：

- channel 前进不会自动推动它
- 只有运维显式切换到新 BOM revision，它才会动

### 跟随 channel

如果一个集群跟随 `ReleaseChannel`：

- `sealos-agent` 可以在每次进程级 reconcile pass 里重新解析本地
  `ReleaseChannel` 文件
- `sealos-agent --controller` 也可以从被 watch 的 `DistributionTarget`
  对象里重新解析它
- 只有当 `ReleaseChannel` 指向的新 revision 发生变化时，它才会前进
- 但它仍然应该把自己最终实际 apply 的 BOM revision 记下来

这样“意图”和“具体结果”就能分开：

- 意图：跟 `default-platform/stable`
- 结果：当前落在 `rev-007`

### 最小 controller target

当前 controller 化路径刻意保持很小。它 watch `DistributionTarget` 对象，
并把每个对象映射成一次现有 agent reconcile pass：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: DistributionTarget
metadata:
  name: default-platform
  namespace: sealos-system
spec:
  clusterName: default
  releaseChannelPath: /var/lib/sealos/distribution/default-platform-stable.yaml
  localRepoPath: /var/lib/sealos/distribution/local-repo
  kubeconfigPath: /host/etc/kubernetes/admin.conf
  hostRoot: /host
  requeueAfter: 1m
```

直接用 controller mode 启动 agent：

```bash
sealos-agent --controller --controller-namespace sealos-system
```

也可以从 [`deploy/distribution-controller/base`](../../deploy/distribution-controller/base)
安装 CRD、RBAC 和 deployment manifests。集群内安装流程和 sample targets 见
[`../guides/controller-install.zh-CN.md`](../guides/controller-install.zh-CN.md)。

这个模式目前提供 watched API、status condition 和可安装 manifests，也包含用于 host
batch size、第一批 canary size、可选 post-canary pause、可选逐批 health gate，以及
stop-or-rollback failure behavior 的持久 `DistributionRolloutPolicy` 对象。
controller target 暂停或回滚完成后，会等待显式更新 target 或 policy 才重新进入 apply。
`sync plan` 也会为 rootfs、host-file、manifest、chart、patch、values、各类 package hook
phase、local patch approval 和 generated host projection 报告 package/phase safety
profiles，让 operator 可以区分 host-wave rollout steps 和 cluster-wide barriers。
health-gated promotion automation 已通过 `sync release-metadata serve` 和
`sealos sync promote` 实现；durable per-package rollout cursor 仍然不在已实现范围内。

## Applied revision state

当前 applied-state 模型会记录：

- BOM name
- BOM revision
- BOM channel

rendered bundle 还会记录 render provenance；如果使用了 channel 文件，其中会包含
本地 `ReleaseChannel` path、digest、distribution line、BOM path 和 BOM
digest。

见 [pkg/distribution/state/types.go](../../pkg/distribution/state/types.go)。

即使 channel resolver 仍然限定在本地文件范围内，这也仍然有价值，因为集群需要有
一个稳定记录，说明自己最近一次 materialize 的精确 baseline 是什么，以及是哪份本
地选择文档导向了它。

更长期、更理想的状态模型应该同时记住两层：

- 请求目标的形式
  - 显式 revision pin，或
  - `distribution line + channel`
- 最终真正 render / apply 的具体 BOM revision

## 派生发行版怎么落在这里

派生发行版不是 fork live drift，而是 fork release lineage。

翻到 BOM 这一层，通常意味着：

- 发布一个新的 BOM family 名或 release namespace
- 在这条新线上继续发布一份或多份新的 BOM revisions
- 必要时，再为这条线配自己的 `ReleaseChannel`

这就是为什么“派生发行版”本质上更应该理解成一条新的 distribution line，
而不是“集群现在 drift 到了什么状态”。

## 最后的经验规则

- 如果你要一个完全可复现的固定 baseline，就直接指向一个 BOM revision。
- 如果你要可控 rollout，就跟 `ReleaseChannel`。
- 如果你需要长期偏离上游 baseline，就 fork 一条新的 distribution line，
  并在那条线上继续发 BOM revisions。
- 不要把今天 BOM schema 里的 `spec.channel` 当成最终 release architecture；
  它更像一个过渡字段，直到 live channel lookup 和 promotion 被显式建模。

## 仍然需要继续设计或实现的部分

- API-backed 的最终版 `ReleaseChannel` schema 和存储契约
- API-backed 的 channel advancement history 怎么存、怎么审计；当前只有本地
  `spec.promotionHistory[]`
- health proof 的 ingestion 或 collection；当前只有 `sealos sync promote` 的本地
  `DistributionHealthProof` 文件 gate
- `BOM.spec.channel` 是先变 optional，还是以后直接移除
- API-backed pin 模式和 channel 模式在 Day 0 的最终 operator interface 长什么样

这份文档不要求这些东西今天已经全部实现。它只是先把它们应该如何拼在一起讲
清楚。
