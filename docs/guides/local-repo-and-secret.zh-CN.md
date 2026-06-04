# 说明文档：Local Repo 结构与 Secret 初始化

## 状态

带当前单节点 MVP 说明的设计文档

## 概述

这份文档专门解释三件事：

- cluster-local `local repo` 逻辑上应该放什么
- 它和 `spec.inputs` 的关系是什么
- Secret 正确的初始化方式应该是什么

它仍然首先是一份设计说明，而不是在宣称整套最终模型都已经落地。当前仓库已经
有一个可工作的单节点 `pkg/distribution/localrepo` MVP，支持 `inputs/`、
`resources/` 和 `patches/`，但下面的布局依然应该理解成面向 MVP 以后演进的
推荐方向。

## 相关文档

- 顶层分发模型：
  [Distribution and config sync](../architecture/distribution-and-config-sync.md)
- ownership 与 reconcile：
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- rendered file、object 和 generated output 的追踪模型：
  [Materialization and drift](../architecture/materialization-and-drift.md)
- Local patch policy 的 source、scope 与 provenance：
  [Local patch policy](../architecture/local-patch-policy.md)
- 包格式与 `spec.inputs`：
  [Package format](../architecture/package-format.md)
- Grafana + 数据库示例：
  [Grafana with KubeBlocks](../guides/grafana-kubeblocks-example.md)
- BOM 与 `ReleaseChannel`：
  [BOM and channel](../guides/bom-and-channel.md)

## Local Repo 是什么

`local repo` 是 cluster-local 的 source of truth，用来承载那些不应该进入 shared
package artifact 的数据。

它的职责是保存：

- package 已声明 input 的具体值
- 本地 Secret 材料或 Secret 引用
- 被允许的 local-owned 资源
- cluster-local 的 revision 元数据或 bookkeeping

它不应该：

- 取代 BOM
- 取代 package artifact
- 变成一个随便覆盖 package 内部任何路径的大兜底层
- 把运行时生成状态假装成 baseline input 持久化回去

最关键的边界还是：

- package 定义 global contract
- local repo 把 cluster-specific 值绑定进这个 contract

## 哪些东西应该放进 Local Repo

| 类别 | 例子 | 为什么要放这里 |
| --- | --- | --- |
| 已声明 input 的具体 payload | CIDR、endpoint、values 文件、config 片段 | 它们是 package input 的 cluster-specific binding。 |
| Secret 名和带 Secret 字节的资源 | Grafana admin Secret、数据库 root Secret、TLS Secret manifest 或其引用 | Secret 字节必须留在 cluster-local 边界。 |
| 明确允许 local-owned 的资源 | 本地 overlay、环境相关 ingress、允许的 namespace 级策略微调 | 它们不属于 shared package baseline。 |
| 本地 revision 元数据 | local patch revision、local repo revision hash | drift 对比和审计需要这些信息。 |

## 哪些东西不该放进 Local Repo

`local repo` 不该包含：

- 伪装成本地输入的 BOM snapshot 副本
- 解包后被直接编辑的 package baseline
- 从集群里导回来的运行时生成数据库密码
- 针对 global-owned package content 的任意直接覆盖

如果某个改动需要被很多集群共享，它应该进入：

- package baseline
- shared patch package
- 或 derived distribution line

## 推荐的逻辑目录结构

一个合理的第一版布局可以是：

```text
local-repo/
  repo.yaml
  revisions/
    current.yaml
  policy/
    local-patch-policy.yaml
  inputs/
    grafana/
      grafana-values.yaml
    grafana-db/
      grafana-db-values.yaml
    kubernetes/
      kubeadm-config.yaml
      hosts/
        192.168.0.240/
          kubeadm-config.yaml
        192.168.0.238/
          kubeadm-config.yaml
  resources/
    secrets/
      grafana-admin-credentials.yaml
      grafana-db-root.yaml
    external-secrets/
      grafana-admin-credentials.external-secret.yaml
      grafana-db-root.external-secret.yaml
  patches/
    grafana/
      ingress.patch.yaml
```

要点是：

- `inputs/` 保存绑定到 `spec.inputs` 的 payload
- 对多节点建模来说，`inputs/<component>/hosts/<host>/...` 现在也是一个受支持
  的 host-scoped input provenance 目录约定
- `policy/local-patch-policy.yaml` 是当前单节点 MVP 下一个可选的显式
  policy artifact。只要它存在，render 就会把它复制进 bundle；之后 local
  patch validator、drift compare 和 `sync commit` 都会统一消费这份已经渲染
  出来的 policy
- `resources/` 保存 local-owned 的 Kubernetes 对象，尤其是带 Secret 的资源
- 在当前单节点 MVP 里，`resources/` 的相对目录结构会在 render 后继续保留。
  例如 `resources/secrets/grafana-admin-credentials.yaml` 会变成
  `local-resources/secrets/grafana-admin-credentials.yaml`
- `patches/` 只能用来修改被允许的 local-owned surface，不是通用 override 层
- 在当前单节点 MVP 里，`patches/` 是按 component 分目录的：
  `patches/<component>/**/*.yaml`
- 每个 patch 文档都是一个 partial Kubernetes object overlay，通过
  `apiVersion`、`kind`、`metadata.name`，以及通常还需要的
  `metadata.namespace` 来绑定目标对象
- 这些 patch 文档会在 render 时 merge 到匹配的 package manifest object 上，
  而不是被当成独立 live resource 直接 apply
- 当前 ownership validator 只放行一小组 local patch surface：
  这组规则现在已经被收成一个带 schema 的 `LocalPatchPolicy` artifact，
  不再只是零散的 allowlist 备注
  `ConfigMap.data`、`ConfigMap.binaryData`、workload placement 字段

当前 host-scoped input 的边界：

- render 和 bundle 现在会把这类按 host 细分的 binding 作为显式 provenance
  保留下来，字段名是 `hostInputBindings`
- 这些 `hostInputBindings` 现在会指向 bundle 内部渲染出来的副本路径，
  例如 `components/<component>/host-inputs/<host>/...`，这样即使原始 local repo
  路径不可见，bundle 仍然是自描述的
- 但今天 render 真正会自动 overlay 的，仍然只有默认的
  `inputs/<component>/<file>` payload
- multi-node `sync apply` 现在会在 local-input-backed direct `file` content
  上消费 host-scoped input payload；其他 content type 仍然使用默认 rendered
  payload
- `sync commit --host <host>` 会沿用同一份 provenance：如果被选中的 host
  有 `hostInputBindings` 条目，commit 会把 live file 回写到
  `inputs/<component>/hosts/<host>/<input-file>`，并同步更新 bundle 里的
  `components/<component>/host-inputs/<host>/...` 副本；它不会覆盖其他 host
  继续使用的默认 input
- 如果被选中的 host 没有 host-scoped input，并且多个 host 的 live 内容已经
  分叉，commit 会拒绝把这个 host 的值写进默认 input；应先初始化对应的
  host-scoped input，或者把目标值统一成所有 host 共用的值
- `sync diff/status` 会在 tracked host-path 摘要里暴露这层 provenance：
  命中 host-scoped payload 的条目会带 `usesHostScopedInput` 和
  `hostInputBindingPath`，split 摘要也会列出哪些分叉 host 已经有 scoped
  payload，哪些还没有
  （例如 `nodeSelector` / `tolerations` / `affinity`）、少量 secret-name
  binding，以及 ingress / service 暴露相关字段（例如 `spec.rules`、
  `spec.tls` 和部分 metadata annotations）
- 如果 `local-repo/policy/local-patch-policy.yaml` 缺失，render 仍然会把一份
  显式 policy 写进 bundle；它可能来自选中的 BOM、唯一选中的 package，或者
  内置默认值
- 所以当前这套 ownership 模型现在已经是显式的：
  package 和 BOM 可以选择当前 rendered revision 的 cluster-local policy，但
  它们还不能定义 package/BOM-scoped policy surface
  render 后的 bundle 会把 policy provenance 标成下面几种之一：
  `localPatchPolicySource: localRepo`
  `localPatchPolicySource: bom`
  `localPatchPolicySource: package`
  `localPatchPolicySource: builtInDefault`
- 这份 policy artifact 本身现在也会显式带上 `spec.scope: clusterLocal`，
  无论它来自哪种 source；当前还不支持 package/BOM-scoped 的 local-patch
  policy，也不支持多层 policy merge
- 如果 bundle 声称来自其他 source，或者它记录下来的 policy
  name/path/digest 和渲染出来的 artifact 对不上，当前 policy consumer
  会直接拒绝这个 bundle，而不是猜测应该用哪份规则
- 当前单节点 MVP 已经支持一条很窄的
  `sealos sync commit --local-repo ...` 路径：
  它可以把已经被 tracked `localPatch` fragment 覆盖的 `Dirty` live drift
  持久化回已有的 `patches/` 文件
- 它现在也能把“纯 local-owned resource object”的 `Dirty` drift
  持久化回原始 `resources/` 文件
- 它现在也能把一类 tracked local-owned host file 的 `Dirty` drift
  持久化回 local repo：前提是这个 host file 来自已声明的 local input
  binding。当前 MVP 只支持 regular file，并且会把 live file 的内容同时回写到
  绑定的默认或 host-scoped `inputs/` payload，以及 bundle 里的渲染副本
- 它仍然不会自动提交 `Orphan` drift、package + resource 混合对象、基于
  symlink 的本地 host path，或任意 input 变化
- 在当前 `sync diff` / `sync status` 输出里，这种 local repo 的分工现在也会
  体现成 remediation ownership：
  `changeOwner=localOverlay` 一般会指回 `patches/` 或 `resources/`，
  而 `changeOwner=localInput` 会指回绑定某个 direct host-side file 的
  `inputs/` payload
- 同时，`sync diff` / `sync status` 现在也会在顶层额外暴露
  `localPatchPolicy`，把当前 rendered bundle 实际生效的 policy source、name、
  path 和 digest 直接带出来
- `repo.yaml` 和 `revisions/current.yaml` 是由 `sealos sync local-repo init`
  写入的 schema-backed 元数据文件

## 元数据文件

`sealos sync local-repo init` 会写入一个小的 `LocalRepo` 文档，用来说明这份
repo 属于哪个 cluster、哪条 distribution line。

例如：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepo
metadata:
  name: poc-minimal-default-platform
spec:
  cluster: poc-minimal
  distributionLine: default-platform
  channel: alpha
  bom: default-platform
  bomRevision: rev-poc-001
```

它还会写入 `revisions/current.yaml`，作为一个 `LocalRepoRevision` 审计对象。
这个对象记录 cluster、distribution line、BOM identity、本地输入 revision
digest、完整 local repo digest 和审计字段，但不携带 Secret payload。

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepoRevision
metadata:
  name: current
spec:
  cluster: poc-minimal
  distributionLine: default-platform
  channel: alpha
  bom:
    name: default-platform
    revision: rev-poc-001
    digest: sha256:<bom-digest>
  localInputRevision: sha256:<inputs-digest>
  digest: sha256:<local-repo-digest>
  audit:
    createdAt: "2026-06-03T00:00:00Z"
    command: sealos sync local-repo init
```

当前行为：

- `localrepo.Load` 仍然接受没有这些文件的旧 local repo 目录
- 如果存在旧版或无效元数据文件，`sync local-repo doctor` 会报告 warning，并
  建议用相同 target 重新运行 init
- render/apply/status 继续使用当前 local repo 内容的实时 digest，所以 init 之后
  编辑 input payload 不会暴露 Secret payload，也不要求在 render 前重写审计对象

## `spec.inputs` 怎么映射到 Local Repo

如果 package 声明了：

```yaml
inputs:
  - name: grafana-values
    type: valuesFile
    path: files/values/basic.yaml
```

它表达的意思是：

- 有一个 cluster-local binding surface，名字叫 `grafana-values`
- package baseline 自己带了一份默认文件 `files/values/basic.yaml`
- 集群可以在 hydration 时把自己的具体值绑定进去

那 local repo 就应该提供这份 cluster-specific payload，例如：

```text
local-repo/
  inputs/
    grafana/
      grafana-values.yaml
```

具体文件命名规范今天还没完全定，但语义规则应该是稳定的：

- package input 名或 component 名，应该能确定性地映射到一个本地 payload 位置

## 推荐的 Secret 处理模型

对于 Secret，推荐始终使用两层模式：

1. 在 input payload 里放 secret reference 或 secret name。
2. 在 cluster-local Secret 资源或 cluster-local secret system 里放真正的
   secret bytes。

这样 package 仍然可复现，而 local repo 仍然是 secret material 的本地权威。

### Grafana 数据库例子

input payload：

```yaml
clusterName: grafana-db
systemAccounts:
  postgres:
    secretName: grafana-db-root
```

本地 Secret 资源：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: grafana-db-root
type: Opaque
stringData:
  username: postgres
  password: <cluster-local-password>
```

这里 input payload 是 binding contract，真正的 Secret 对象才是 secret source。

## 两种正确的 Secret 初始化路径

Secret 初始化合理的方式只有两种。

### 路径 A：直接本地 Secret manifest

这是最简单的 bootstrap 路径，也最适合作为 MVP 第一版。

local repo 直接带一份 Secret manifest：

```text
local-repo/
  inputs/
    grafana/
      grafana-values.yaml
  resources/
    secrets/
      grafana-admin-credentials.yaml
```

适用场景：

- 环境规模小、相对自包含
- 需要低摩擦 bootstrap
- local repo 不会被同步到共享远端服务

它适合：

- 实验环境
- 小型私有环境
- 第一版 MVP

但它不应该成为生产环境的唯一答案。

### 路径 B：cluster-local Secret 引用

这是更适合长期生产的模式。

local repo 里存的不是原始 Secret 字节，而是引用对象，例如：

- `ExternalSecret`
- `SecretProviderClass`
- SOPS 加密后的 Secret manifest
- 其他 cluster-local secret manager 引用对象

示意例子：

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: grafana-admin-credentials
spec:
  secretStoreRef:
    name: cluster-vault
    kind: ClusterSecretStore
  target:
    name: grafana-admin-credentials
  data:
    - secretKey: admin-user
      remoteRef:
        key: grafana/admin-user
    - secretKey: admin-password
      remoteRef:
        key: grafana/admin-password
```

适用场景：

- 环境里已经有 secret manager
- Git-backed local repo 不适合存放原始 Secret 字节
- rotation 和审计应该由专门的 secret 系统负责

## 推荐的初始化流程

正确的运维流程应该是：

1. 先选定 BOM revision，或者 `distribution line + ReleaseChannel`。
2. 根据 BOM 和 package input contract 为这个集群初始化 local repo skeleton。
3. 在 `inputs/` 下填非 Secret 的 input 值。
4. 在 `resources/` 下创建需要的 Secret 资源或 Secret 引用。
5. 在 hydrate 前先校验所有 required input 和 required secret reference 都存在。
6. 用 `BOM + local repo` hydrate 成最终 desired state。
7. 用 `sync plan` 预览 rendered apply 意图，包括 target 解析、local resource 和
   Secret object 摘要。
8. 如果 Secret 属于 local-owned resource set，就先 apply Secret，再 apply 依赖
   它的 package content。

当前 CLI 初始化入口是：

```bash
sealos sync local-repo init \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --output-dir ./local-repo \
  --output yaml
```

initializer 会创建 `inputs/` 模板、`resources/` 和 `patches/` 目录、
`policy/local-patch-policy.yaml`，以及最小 local repo 元数据。它不会生成真实
Secret 字节。看起来像 Secret 的 input 会使用私有文件权限写出，并在输出里作为
hint 提醒运维去 `resources/` 下创建对应的 Secret manifest 或 external-secret
reference。

运维填完 local repo 之后，先跑 local repo doctor，再进入更宽的 validate：

```bash
sealos sync local-repo doctor \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --local-repo ./local-repo \
  --output yaml
```

doctor 同样是只读命令，但它聚焦 local repo 自身。它会报告未替换的
`local-repo init` 模板、缺失的 required input、`inputs/` 或 `patches/` 下的
stale component 目录、缺失的 `policy/local-patch-policy.yaml`、`resources/`
下的非 manifest 文件，以及看起来像 Secret 的文件是否存在 kind 或权限问题。
它只输出文件路径和修复建议，不会打印 input 或 Secret payload 内容。

如果需要在 render 前给 CI 或运维脚本一个单命令 gate，可以使用 source
preflight：

```bash
sealos sync preflight \
  --cluster default \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --local-repo ./local-repo \
  --output yaml
```

带 `--file` 时，preflight 会在设置了 `--local-repo` 的情况下先跑 local-repo
doctor，然后再跑更宽的 `sync validate` 契约检查。通过时，输出里会带下一步应
执行的 `sealos sync render ...` 命令。不带 `--file` 时，同一个
`sync preflight` 仍然保持 rendered-bundle 模式，用来检查 `--bundle-dir` 是否
能通过 apply gate。这个 rendered-bundle 模式会检查 topology/render-input
freshness 和本机 runtime readiness，包括 host mutation 权限、systemd 是否
可用、swap、已有 Kubernetes node 状态、bootstrap 端口、已存在的 runtime
binary、kubeconfig/client 可用性，以及受管 service 状态。runtime warning
只会出现在结构化输出的 `runtimeStatus` 下；blocking runtime check 会阻止
`sync apply`。

现在 `sealos sync render` 默认也会在 materialize bundle 之前运行同一套 source
preflight。source 侧存在 blocking issue 时，render 会停止，并在结构化
`sourcePreflight` 输出里返回具体问题。`--skip-source-preflight` 只建议用于开发
或调试，也就是你明确想用不完整或不安全的 source input 强制 render 的场景。
成功 render 后，rendered bundle 还会写入一份经过脱敏的
`spec.sourcePreflight` 摘要。它只记录状态、blocking reason、聚合计数以及
doctor/validate 两个阶段的结果；不会把 input 或 Secret payload 内容复制进
bundle metadata。

当前 CLI 校验入口是：

```bash
sealos sync validate \
  --cluster default \
  --file bom.yaml \
  --package-source grafana=./packages/grafana \
  --package-source grafana-db=./packages/grafana-db \
  --local-repo ./local-repo \
  --output yaml
```

这个 validator 是只读的。它在 render/apply 前检查 BOM、package 和 local repo
之间的契约，包括 package source 是否有效、required input 是否已经绑定、
host-scoped input 里的 host 是否属于当前 cluster inventory、local patch policy
是否兼容、target 是否能解析，以及明显的 Secret manifest 文件权限问题。
在测试和脚本化 smoke 场景里，可以加 `--runtime-root <dir>` 指向特定的
Clusterfile inventory。

render 之后，用 `sync plan` 作为只读的运维 review 步骤：

```bash
sealos sync plan \
  --cluster default \
  --bundle-dir <rendered-bundle-dir> \
  --output yaml
```

plan 输出会解析 `allNodes`、`firstMaster` 和 `cluster` target，并汇总 component
steps、local resources、tracked Kubernetes objects 和 tracked host paths。
Secret object 只会以 sensitive object summary 出现；命令不会打印 `data` 或
`stringData` 这类 Secret payload 字段。如果旧 bundle 没有
`spec.sourcePreflight` metadata，`sync plan`、`sync apply`、`sync diff` 和
`sync status` 都会在输出里给出 warning，让运维知道这份 bundle 没有记录
render 时的 source readiness 结果。`sync diff` 和 `sync status` 也会把记录的
`sourcePreflight` 摘要放在 live drift summary 旁边，方便把现场变化和生成这份
rendered bundle 时通过的 source check 对起来看。

关键点是：

- Secret 必须在依赖它的 workload 被期待收敛之前就存在

## 哪些做法是不对的

不正确的初始化方式包括：

- 直接去改 package baseline 文件，把 Secret 字节塞进去
- 把 Secret 字节写进 BOM
- 让 local repo 变成一个可以任意覆盖 package 任意路径的自由 overlay 层
- 把运行时自动生成的 Secret 导出回 shared package content

这些做法都会破坏可复现性、ownership，或者两者一起破坏。

## Day 0 的 Secret 初始化

在 Day 0，Secret 初始化应该被视为所有依赖它的 package 的前置条件。

例如：

- `grafana-db` 依赖 `grafana-db-root`
- `grafana` 依赖 `grafana-admin-credentials`

因此 bootstrap 顺序应该是：

1. 初始化或引用这些 local Secret 对象
2. apply 数据库 package
3. 等数据库 ready，或者等它生成自己的运行时凭证
4. 再 apply 依赖它的应用 package

这也是为什么数据库 package 和应用 package 应该保持拆分。

## 运行时生成 Secret 的边界

local repo 应该初始化那些 bootstrap 前必须存在的 cluster-owned Secret。

但它不应该试图拥有所有后续运行时才出现的 Secret。

应该继续留在 runtime-local 的例子包括：

- KubeBlocks 运行时生成的账号 Secret 内容
- operator 自动生成的一次性 bootstrap token
- 应用自动生成的内部 Secret，除非 Sealos 明确决定要外置并管理它

这些对象可以被观察、被引用，但不应该被盲目复制回 local repo，并假装它们是
稳定的 input。

## 最后的经验规则

如果一个 package 需要 Secret：

- 在 `spec.inputs` 里声明 binding surface
- 在 local input payload 里放 secret name 或 reference
- 通过本地 Secret 对象或本地 secret system 初始化真正的 secret bytes
- 在期待 package 收敛前先校验 Secret 已存在

这是 package baseline、local repo 和 runtime state 三者之间最干净的边界。
