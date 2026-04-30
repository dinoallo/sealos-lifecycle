# 说明文档：Local Repo 结构与 Secret 初始化

## 状态

设计说明

## 概述

这份文档专门解释三件事：

- cluster-local `local repo` 逻辑上应该放什么
- 它和 `spec.inputs` 的关系是什么
- Secret 正确的初始化方式应该是什么

它明确是设计说明，不是在描述一个已经完整实现的代码路径。当前仓库里还没有
完成版的 `pkg/distribution/localrepo` 包，因此下面的布局应该理解成推荐方向，
适用于 MVP 和后续演进。

## 相关文档

- 顶层分发模型：
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md)
- ownership 与 reconcile：
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- 包格式与 `spec.inputs`：
  [sealos-component-package-format-design.md](./sealos-component-package-format-design.md)
- Grafana + 数据库示例：
  [sealos-grafana-kubeblocks-example.md](./sealos-grafana-kubeblocks-example.md)
- BOM 与 `DistributionChannel`：
  [sealos-bom-and-distribution-channel-guide.md](./sealos-bom-and-distribution-channel-guide.md)

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
  inputs/
    grafana/
      grafana-values.yaml
    grafana-db/
      grafana-db-values.yaml
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
- `resources/` 保存 local-owned 的 Kubernetes 对象，尤其是带 Secret 的资源
- `patches/` 只能用来修改被允许的 local-owned surface，不是通用 override 层
- `repo.yaml` 和 `revisions/current.yaml` 只是推荐元数据文件，不代表今天已经定
  死 schema

## 推荐的元数据文件

最终 `local repo` 最好至少有一个很小的元数据文件，说明这是谁的 repo。

例如：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepo
metadata:
  name: poc-minimal-local
spec:
  clusterName: poc-minimal
  line: default-platform
```

再加一个 revision bookkeeping 文件，例如：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalRepoRevision
metadata:
  name: current
spec:
  revision: local-20260501-001
  inputsHash: sha256:<hash>
```

这些对象今天还没实现，但模型很有价值：

- 一个文档说明 local repo 是谁的
- 一个文档说明当前 cluster-local input revision 是什么

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

1. 先选定 BOM revision，或者 `distribution line + DistributionChannel`。
2. 为这个集群生成或初始化 local repo skeleton。
3. 在 `inputs/` 下填非 Secret 的 input 值。
4. 在 `resources/` 下创建需要的 Secret 资源或 Secret 引用。
5. 在 hydrate 前先校验所有 required input 和 required secret reference 都存在。
6. 用 `BOM + local repo` hydrate 成最终 desired state。
7. 如果 Secret 属于 local-owned resource set，就先 apply Secret，再 apply 依赖
   它的 package content。

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
