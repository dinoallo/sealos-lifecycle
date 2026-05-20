# 示例：最小化的 `sealos sync` Drift 场景

## 状态

面向当前单节点 MVP 的参考示例

## 概述

这个目录给出了一套最小的本地文件布局，用来对应当前单节点 `sealos sync`
drift 工作流。

它不是一个完全自包含的集群 fixture。它更准确地说是在给你一份具体样本，说明：

- 一个很小的 local repo 可以长什么样
- 哪些文件通常对应 `localOverlay`，哪些通常对应 `localInput`
- 检查、提交、回退 drift 时建议采用什么命令顺序

它应该和下面这些真实对象配合使用：

- 一份真实的 rendered bundle 目录
- 一份真实的 cluster kubeconfig
- 一份真实的 `AppliedRevision`

这个示例现在也额外带了一组和 schema 对齐的示例 fixture，位置在：

- `bundle/bundle.yaml`
- `bundle/components/...`
- `bundle/local-resources/...`
- `bundle/policy/local-patch-policy.yaml`
- `applied-revision.example.yaml`
- `policy-gate-approved.example.yaml`
- `sync-diff.example.yaml`
- `sync-status.example.yaml`

这些 fixtures 的目标是和当前 `sync` drift 模型对齐，而不是假装其中每个路径在
不做调整的前提下都能直接跑起来。

## 目录结构

```text
docs/examples/sync-drift-minimal/
  README.md
  README.zh-CN.md
  applied-revision.example.yaml
  policy-gate-approved.example.yaml
  sync-diff.example.yaml
  sync-status.example.yaml
  bundle/
    bundle.yaml
    components/
    local-resources/
    policy/
      local-patch-policy.yaml
  local-repo/
    inputs/
      kubernetes/
        kubeadm-cluster-config.yaml
    policy/
      local-patch-policy-approval.approved-example.yaml
      local-patch-policy.yaml
      local-patch-policy-approval.yaml
    resources/
      secrets/
        grafana-admin-credentials.yaml
    patches/
      grafana/
        grafana-settings.patch.yaml
```

这里每个文件的语义是：

- `inputs/kubernetes/kubeadm-cluster-config.yaml`
  - 一个 cluster-local input payload，可以对应一份 direct host-side 文件，
    例如 `/etc/kubernetes/kubeadm.yaml`
  - 在当前 remediation 输出里，这类 source 对应 `changeOwner=localInput`
- `resources/secrets/grafana-admin-credentials.yaml`
  - 一个 standalone local-owned Kubernetes 对象
  - 在当前 remediation 输出里，它属于 `localOverlay` 这一侧
- `patches/grafana/grafana-settings.patch.yaml`
  - 一个针对 package-provided `ConfigMap` 的本地 overlay 文档
  - 在当前 remediation 输出里，它也属于 `localOverlay`
- `policy/local-patch-policy.yaml`
  - 一个显式的 cluster-local local patch policy artifact
  - 在当前 rendered 输出里，它会体现成顶层 `localPatchPolicy`
    provenance block
- `policy/local-patch-policy-approval.yaml`
  - 一个给 `sealos sync policy-gate` 使用的可审计例外文件
  - 它现在会同时绑定被比较的 old policy 和 candidate new policy 的
    `name`、`scope`、`digest`
  - 它也会带上一组治理元数据：
    `owner`、`approvedBy`、`changeRef`、`expiresAt`
  - 如果要批准某个 violation，这份文件还必须把预期的 `expectedCount`
    和 `expectedImpact` 一起钉住
  - 这个 example 里它故意保持为空，因此整个示例仍然体现默认的严格 gate 行为
- `policy/local-patch-policy-approval.approved-example.yaml`
  - 一份非空的 approval file 示例，用来说明当 widening 或 incompatible
    patch 影响被有意识接受时，这份文件应该长什么样
  - 它和 `policy-gate-approved.example.yaml` 配套，主要用来展示结构，不是
    当前 strict 示例的默认输入

这些示例 fixture 对应的是：

- `bundle/bundle.yaml`
  - 一份带 tracked objects、tracked host paths 和 rendered component entries
    的 rendered-bundle 样例
- `bundle/components/grafana/local-patches/grafana-settings.patch.yaml`
  - 作为 `localOverlay` 后端的 rendered patch 副本
- `bundle/local-resources/secrets/grafana-admin-credentials.yaml`
  - local-owned Secret 的 rendered 副本
- `bundle/policy/local-patch-policy.yaml`
  - 这份 bundle revision 实际携带的 local patch policy 的 rendered 副本
- `bundle/components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml`
  - 对应 tracked local input-backed host path 的 rendered 文件
- `applied-revision.example.yaml`
  - 一份和这个 bundle 示例对齐的 recorded-state 对照样例，用来说明
    初次成功 apply 之后、以及后续写回 observed drift 摘要之后，
    `AppliedRevision` 大致会长什么样
- `sync-diff.example.yaml`
  - 一份缩短过、但和当前 schema 对齐的 `sealos sync diff` 输出快照
- `sync-status.example.yaml`
  - 一份缩短过、但和当前 schema 对齐的 `sealos sync status` 输出快照
- `policy-gate-approved.example.yaml`
  - 一份缩短过、但和当前 schema 对齐的 `sealos sync policy-gate` 输出快照
  - 用来展示当前 CLI 里 `approvalSummary`、它携带的生命周期元数据，以及
    `approvedViolations[].impact` 这层信息会出现在哪里
  - 里面也展示了当前的 `followUpAction` 提示，用来告诉 operator 什么时候该
    清理或续签 approval

## Recorded State 对照样例

这个示例目录现在也包含：

- `applied-revision.example.yaml`
- `sync-diff.example.yaml`
- `sync-status.example.yaml`
- `policy-gate-approved.example.yaml`

把它和下面这些对象一起看：

- `bundle/bundle.yaml`
- `local-repo/...`

可以更直观地理解当前单节点 MVP 里的 3 层记录：

- rendered desired state
- cluster-local source inputs 和 overlays
- recorded applied / observed cluster state
- 面向 operator 的 drift 输出快照

`applied-revision.example.yaml` 里的 digest 和 timestamp 都是示意值。它们的
作用是展示 schema 形态和字段之间的关系，不是在声称这份 bundle 示例就是由这些
精确值生成出来的。

对 policy provenance 也是同样的理解：

- `bundle/spec.localPatchPolicy*` 用来展示当前 rendered bundle 如何记录
  生效中的 policy source、scope、name、path 和 digest
- `bundle/policy/local-patch-policy.yaml` 是这组 metadata 指向的 rendered
  artifact
- `local-repo/policy/local-patch-policy.yaml` 是这个示例里被复制进 bundle 的
  cluster-local source artifact

对 `sync-diff.example.yaml`、`sync-status.example.yaml` 和
`policy-gate-approved.example.yaml` 也应该按同样方式理解：它们是缩短过、
但和当前 schema 对齐的输出快照，不是在声称“这个目录不做任何调整就跑一次命
令”必然会逐字得到完全一样的结果。

## 关于 `inputBindings` 的一个重要说明

在真实 rendered bundle 里，`components[].inputBindings` 记录的是 render 当时
使用的 local-repo 绝对路径。

由于文档示例不可能预先知道你的实际文件系统根路径，这份 bundle 示例里用了一个
占位值：

```text
/ABSOLUTE/PATH/TO/local-repo/inputs/kubernetes/kubeadm-cluster-config.yaml
```

如果你真的想拿这份示例去试 `sync commit` 对 local input-backed host file
的那条路径，请先把这个占位值替换成下面这个文件的真实绝对路径：

```text
docs/examples/sync-drift-minimal/local-repo/inputs/kubernetes/kubeadm-cluster-config.yaml
```

## 这个示例里的文件

### `local-repo/inputs/kubernetes/kubeadm-cluster-config.yaml`

这份文件代表一个 cluster-local bootstrap input：

```yaml
apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
clusterName: demo
networking:
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/12
```

在当前单节点 MVP 里，render 后的 bundle 可能会把这类输入投影成一个 tracked
host path，例如 `/etc/kubernetes/kubeadm.yaml`。

### `local-repo/policy/local-patch-policy.yaml`

这份文件代表一个显式的 cluster-local local-patch policy：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: custom-local-patch-policy
spec:
  scope: clusterLocal
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
        - binaryData
```

在当前单节点 MVP 里，这份 policy 会：

- 在 render 时从 `local-repo/policy/local-patch-policy.yaml` 读入
- 被复制到 `bundle/policy/local-patch-policy.yaml`
- 以 `localPatchPolicySource`、`localPatchPolicyScope`、
  `localPatchPolicyName`、`localPatchPolicyPath` 和
  `localPatchPolicyDigest` 的形式记录进 `bundle.yaml`
- 当前只支持 `spec.scope: clusterLocal`；这个 MVP 里故意不支持
  package/BOM-scoped 的 local-patch policy
- 在后续的 local patch validation、compare 阶段 `policyEligible`
  标注，以及 `sync commit` 中被统一消费

`localPatchPolicyDigest` 是 rendered
`bundle/policy/local-patch-policy.yaml` artifact 的 digest。policy-gate
approval 文件则用 `sync policy-gate` 输出的规范化 policy-document digest
绑定 policy 身份，所以这两组示例 digest 不能互相替换。

### `local-repo/resources/secrets/grafana-admin-credentials.yaml`

这份文件代表一个 local-owned Secret：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
  namespace: default
type: Opaque
stringData:
  username: admin
  password: passw0rd
```

这是当前本地 drift 的一种典型形态，它可以被：

- 通过 `sync diff` / `sync status` 检查
- 通过 `commit` 写回 `resources/`
- 或通过 `revert` 拉回 recorded desired state

### `local-repo/patches/grafana/grafana-settings.patch.yaml`

这份文件代表一个被允许的 local patch：

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-settings
  namespace: default
data:
  adminUser: root
```

它对应当前 MVP 的 patch contract：

- 目录按 `patches/<component>/` 分组件
- 文档本身是 partial Kubernetes object overlay
- 只能修改那些被允许的 local-owned path

## 最小命令顺序

假设：

- bundle 目录：`docs/examples/sync-drift-minimal/bundle`
- local repo：`docs/examples/sync-drift-minimal/local-repo`
- kubeconfig：`/etc/kubernetes/admin.conf`

这一节只负责把“在这个示例目录里命令该怎么写”钉住。至于为什么该用这个命令、
`operatorAction` 是怎么来的、`bundleMatchesRecordedDesiredStateDigest`
这类护栏具体怎么起作用，请直接看：

- [sealos-sync-drift-walkthrough.md](../../sealos-sync-drift-walkthrough.md)
- [sealos-sync-operator-action-reference.md](../../sealos-sync-operator-action-reference.md)
- `sync-diff.example.yaml`
- `sync-status.example.yaml`

### 1. 先检查原始 Drift

```bash
sealos sync diff \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### 2. 再看 Ownership 摘要

```bash
sealos sync status \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### 3. 接受有意的本地 Drift

只有在 remediation 指向：

- `changeOwner=localOverlay`
- 或 `changeOwner=localInput`

并且这个命令当前是 `available` 时，才用 `commit`。

```bash
sealos sync commit \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --local-repo docs/examples/sync-drift-minimal/local-repo \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root /
```

### 4. 回退不想要的 Drift

如果 remediation 指向：

- `changeOwner=globalBaseline`
- 或者这份 local drift 根本不是有意的

那就用 `revert`。

例子：只回退一个 local host file：

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --host-root / \
  --scope local \
  --host-path /etc/kubernetes/kubeadm.yaml
```

例子：只回退一个 object：

```bash
sealos sync revert \
  --cluster demo \
  --bundle-dir docs/examples/sync-drift-minimal/bundle \
  --kubeconfig /etc/kubernetes/admin.conf \
  --kind Secret \
  --namespace default \
  --name grafana-admin-credentials
```

### 5. 再跑一遍只读命令

执行完 `commit` 或 `revert` 之后，再跑一次：

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

你想看到的结果应该是：

- 已提交的本地 drift 消失，因为现在 local repo 已经和 live state 对齐
- 已回退的 drift 消失，因为现在 live state 已经和 desired state 对齐

## 这个示例刻意不覆盖什么

这个示例刻意保持最小，不尝试展示：

- 一整套带真实 digest、timestamp 和 recorded state path 的可直接运行 runtime
  root
- generated static Pod remediation
- 多节点 apply 编排示例
- package rebuild 或 BOM fork 流程

这些内容在别的文档里已经有。这个示例目录唯一的目标，就是把当前单节点
`diff -> status -> commit/revert -> diff` 这条 operator loop 钉死。
