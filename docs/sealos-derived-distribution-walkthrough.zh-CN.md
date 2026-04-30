# 操作指引：让集群派生出自己的发行版

## 状态

基于当前设计的操作说明

## 概述

这份文档解释的是：当某个集群不想接受，或者确实无法兼容当前全局发行版的
baseline 变更时，应该如何从共享发行线中独立出来。

核心原则是：如果偏离是长期存在的，就不应该在 live cluster 上悄悄改
global-owned 内容并继续跟着同一条上游发行线跑。正确做法是派生出一条新的
distribution line，通常表现为：

- 一份新的 BOM revision
- 必要时，再派生少量新的 ComponentPackage revision
- 如果需要长期维护，再形成自己的 channel lineage

这份文档基于当前设计文档和仓库里的 minimal single-node PoC 资产来写，
不是在描述一个已经完全产品化的一键 fork 命令。

## 相关文档

- 顶层架构：
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md)
- reconcile、ownership 与 drift：
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- release channel 与 promotion：
  [sealos-multi-cluster-release-and-promotion-design.md](./sealos-multi-cluster-release-and-promotion-design.md)
- 组件包契约：
  [sealos-component-package-format-design.md](./sealos-component-package-format-design.md)
- 当前 PoC BOM：
  [scripts/poc/minimal-single-node/bom.yaml](../scripts/poc/minimal-single-node/bom.yaml)

## 这里说的术语

这份指引沿用顶层设计文档里的术语：

- `BOM revision`：一次具体可发布的 baseline snapshot
- `distribution snapshot`：这一份 BOM revision 所代表的完整平台基线
- `distribution line`：集群长期跟随的一条具名发行线，由一串 BOM revisions
  构成

所以这份文档讲的是如何 fork 一条 `distribution line`，而不是如何把一团没被
记录的 live-state mutation 原样保留下来。

## 决策阶梯

不是所有不兼容都应该直接走“派生发行版”。应该按这个顺序判断：

1. 如果根本不需要改，就继续跟当前 baseline。
2. 如果只是合法的 per-cluster variation，就用 local binding。
3. 如果新 baseline 暂时不能接受，但还不需要独立发行线，就先 pin 到旧 BOM。
4. 如果这个偏离需要长期、可追踪地存在，就派生一份新的 BOM。
5. 只有在必要时，才为少量组件发布自己的 package revision。

最重要的设计意图是：偏离必须变成一个显式 revision object，而不是一团没有
记录的 live-state 变异。

## 不应该怎么做

如果一个集群直接修改了 global-owned baseline 内容，却继续跟随同一条上游 BOM
或 channel，那么它实际上已经不在受支持的发行线上了。这类行为在 ownership
模型里会把集群推向 `Orphan` 一类的状态。

因此，真正要 fork 的对象必须是：

- 一份新的 BOM revision
- 必要时，一些新的 component package revisions

而不应该是“集群当前 drift 成了什么样子”。

## 三种偏离层级

### 1. Local Variation

如果差异本来就应该按 cluster 变化，就继续用 local binding：

- CIDR
- endpoint
- mirror setting
- 证书
- MTU
- 环境相关 values

这种情况不需要派生新的 distribution line。

### 2. Shared Policy Difference

如果一组集群希望采用不同的平台策略，应使用 derived BOM，再配合共享 patch
或替换 package：

- 不同的 audit policy
- 不同的 admission 默认策略
- 不同的 Cilium policy profile
- 不同的 hardening overlays

这类差异应该是一个受支持、可追踪的分支，而不是伪装成 local override。

### 3. True Distribution Fork

当一个集群或一组集群必须长期拒绝某个 global baseline 改动，并且要沿着自己
的轨道继续演进时，就应该真正 fork 一条发行线。

典型信号：

- 某个组件版本与本地环境不兼容
- 运维方希望选择不同的 CNI 或 runtime 策略
- 集群需要长期维护另一套 hardening profile
- 集群需要持续吸收部分上游变更，同时永久拒绝另一些变更

## 什么叫派生发行版

在这套设计里，派生发行版通常意味着：

- 一个新的 BOM `metadata.name` 和/或 `spec.revision`
- 一组 digest-pinned 的组件引用，其中大部分仍可复用上游组件 digest
- 可选地，再形成自己的 channel 或 release namespace

最关键的性质是：这条派生线必须是显式的、可复现的。
更准确地说，派生发行版是一条派生出来的 `distribution line`，而它的具体发布
落点则是一份或多份派生 `BOM revisions`。

## 标准 fork 模式

标准做法应该是“选择性 fork”，而不是“整体全 fork”：

1. 先从一个上游 BOM revision 出发。
2. 拷贝这份 BOM，形成自己的 derived BOM。
3. 大多数组件仍保留上游 artifact digest。
4. 只有不兼容的组件才替换成自己的 package revision。
5. 发布这份新 BOM，并让目标集群以后跟它。

这比把所有 package 全部复制一遍便宜得多。

## Step-by-step

### Step 1：先识别差异类型

先问自己：

- 这是不是只是 cluster-specific value？
- 这是不是一组共享的平台策略差异？
- 这是不是一个真正不兼容的组件或发行选择？

如果只是 cluster-specific value，就停在 local binding。

如果它已经改变了 global-owned package intent，就不要再把它当成 local 了，
而应该进入 derived BOM 路径。

### Step 2：选定上游起点

确定你要从哪个上游 BOM revision 出发。

在当前 PoC 里，这个起点就是：

- BOM name: `minimal-single-node`
- revision: `rev-poc-001`
- 文件：
  [scripts/poc/minimal-single-node/bom.yaml](../scripts/poc/minimal-single-node/bom.yaml)

这个起点会成为你之后 rebase 时的对照基准。

### Step 3：判断是不是只 fork BOM 就够了

很多时候，你只需要 fork BOM，不需要 fork package。

例如：

- 只是 pin 到一个已经存在的旧 component digest
- 只是切换到另一个已发布的 package revision
- 只是替换某一个组件的 artifact reference，而其他组件不变

只有当你需要的 package revision 本身还不存在时，才需要一起 fork package。

### Step 4：创建 derived BOM

复制上游 BOM，并赋予它新的身份。

通常要改：

- `metadata.name`
- `spec.revision`
- 必要时 `spec.channel`
- 明确保留依赖图
- 未变更的组件继续引用上游 artifact digest

例如：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: corp-minimal-single-node
  labels:
    distribution.sealos.io/profile: corp
spec:
  revision: rev-corp-001
  channel: alpha
  components:
    - name: containerd
      kind: infra
      version: v1.7.18
      artifact:
        name: containerd-runtime
        image: local/poc/containerd-runtime:v1.7.18
        digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
    - name: kubernetes
      kind: infra
      version: v1.30.3
      dependencies:
        - containerd
      artifact:
        name: kubernetes-rootfs
        image: local/poc/kubernetes-rootfs:v1.30.3
        digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
    - name: cilium
      kind: infra
      version: v1.15.0-corp.1
      dependencies:
        - kubernetes
      artifact:
        name: cilium-cni
        image: registry.example.io/corp/cilium-cni:v1.15.0-corp.1
        digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
```

这个例子表达的是：

- `containerd` 和 `kubernetes` 继续复用上游 artifact
- 只有 `cilium` 被替换成自家的 package revision
- distribution identity 已经属于派生线，而不是上游线

### Step 5：只为必要组件发布新的 package

如果某个组件必须变化，就只为它构建新的 package revision，并发布到自己的
registry namespace。

仓库里已经有 package build/push 的基本形态：

- `sealos sync package build`
- `sealos sync package push`

如果偏离发生在 Cilium，通常就是：

1. 从 `scripts/poc/minimal-single-node/packages/cilium` 出发
2. 修改 package payload 或 packaged defaults
3. build 新的 OCI package image
4. push 到自己的 registry path
5. 把新的 image + digest 写回 derived BOM

关键点是：派生发行版引用的必须是不可变 package revision，而不是 live cluster
上某次人工改动后的状态。

### Step 6：让集群跟随新的 BOM

一旦 derived BOM 准备好，目标集群就应该停止跟随原来的上游发行线，转而以这份
derived BOM 作为目标 baseline。

也就是说，从设计语义上看，它已经不是“同一条发行线上的特例集群”，而是
“跟随另一条显式发行线的集群”。

### Step 7：后续变更仍然走 revision object

fork 完以后，后续演进仍然应该通过 revision object 来做：

- forked 组件继续发新的 package revision
- 组件组合变化就发新的 derived BOM revision
- 不要通过直接改 live global-owned 内容去绕过派生线

这样 fork 出来的发行线才是可复现、可审查的。

## 一个最小 PoC 例子

当前 PoC BOM 在：

- [scripts/poc/minimal-single-node/bom.yaml](../scripts/poc/minimal-single-node/bom.yaml)

如果某个集群不能接受上游 Cilium 选择，但其他都没问题，那么最小支持路径就是：

1. 保持 `containerd` 不变
2. 保持 `kubernetes` 不变
3. 只发布一个新的 `cilium-cni` package revision
4. 创建一份新的 BOM revision，把 `cilium` 指向新的 digest

这就是一条“最小偏离”的派生发行线。

## Rebase 策略

fork 发行线不代表从此拒绝所有上游变化。

更健康的维护模式是“选择性 rebase”：

1. 审核新的上游 BOM revision
2. 把仍然接受的组件 digest 向前带
3. 对仍不兼容的部分保留自家 forked digest
4. 发布新的 derived BOM revision

这样集群仍能吸收兼容的上游修复，同时保留它真正需要的分歧。

## 对 release 和 promotion 的影响

一旦派生发行版存在，它就应该被当成自己的一条可审查 release line：

- BOM revision 必须 digest-pinned
- 被替换的组件必须可审计
- health evidence 应该按它自己的发行线来评估
- 不能把不兼容的 local change 静默地回灌进上游 `Stable`

如果后来发现这条派生线对很多集群都有价值，它可以进一步演化成：

- 一个上游 candidate revision
- 一个共享 patch package
- 或一个正式支持的 baseline variant

## 实用 guardrails

- 不要把本该是 `input` 的值直接升级成发行线 fork。
- 不要在改了 global-owned package intent 后还继续跟同一条上游线。
- 不要只因为一个组件不兼容就把所有组件全 fork。
- 不要把一个 drift 过的 live cluster 当成 fork 的 source of truth。
- 不要在 derived BOM 里丢掉 digest pinning。

## 当前仓库的边界

这份文档描述的是设计上支持的 workflow，但当前仓库还没有一个完全产品化的
“一键把集群 fork 成新发行版”的命令。

今天仓库已经具备的是：

- digest-pinned BOM
- OCI component package build/push 命令
- 消费 BOM 的 render/apply 路径
- ownership、promotion、review 的设计指导

还没有具备的是一个一等公民 CLI，可以自动：

- clone 一份 BOM
- 只改动变化过的 artifact reference
- 分配新的 release metadata
- 把 derived line 持久化成受管的 release object

所以当前阶段，fork 更像一种受约束的文档化 artifact workflow，而不是内建单命令。

## 结论

当某个集群真的无法接受 global baseline 改动时，受支持的路径是 fork 一条新的
distribution line，而不是长期背着一团没有记录的 live-state 分歧。

落地上通常就是：

1. 先确认它是不是已经超出 local variation
2. 从上游 BOM 拷贝出一份新的 derived BOM
3. 尽量复用未变化组件的 digest
4. 只替换不兼容的组件 package revision
5. 让集群以后跟随新的 BOM 线
6. 继续通过新的 BOM 和 package revision 来维护这条线
