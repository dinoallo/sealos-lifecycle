# Proposal: Git 同步的发行版配置

## 状态

草案

## 摘要

本文提出一种 Git 仓库组织方式，用来同步 Sealos 发行版配置，包括 package manifest、build class 引用、发行版 profile、BOM 文件和发行通道。

推荐模型是：Git 作为发行版配置、发布意图，以及构建 package 所需源事实的事实源。Git 中只保存 `package.yaml`、可选的 repo-local build class descriptor、profile、BOM、channel 指针等小型、可审查的 YAML 文档。标准 build class 由 Sealos 实现，并通过不可变 class identity 引用；distribution 仓库不需要 vendor 这些定义。OCI Registry 仍然是预构建不可变 package artifact 的首选传输和缓存方式，但不是 materialize package 的唯一方式。

本 proposal 优先定义仓库约定和解析规则，不要求 Sealos 在采用该布局前先引入新的 API 类型。`ReleaseChannel`、`ClusterTarget` 等正式 schema 可以后续补齐，而不改变这里定义的路径模型。配套的 [document kind 规范](kinds.zh-CN.md) 负责跟踪哪些 kind 是 Kubernetes CRD、repository source document、generated document、evidence document 或 proposal-only schema。

## 问题

现有 package 和 BOM 模型已经定义了包是什么，以及一个发行版如何锁定 package revision。但团队如果想通过 Git 同步这些文件，还需要一个清晰、实用的仓库布局。

如果没有约定：

- package manifest 和 release BOM 容易与生成的 render 产物混在一起
- 不同 package type 或 provider 可能在 `kubernetes`、`cilium`、`cert-manager` 这类短名称上冲突
- channel promotion 不容易审查，因为 channel 移动没有被单独隔离
- profile 默认值和 promotion policy 可能误混入 package source 定义中
- 操作员很难快速找到影响某个 distribution revision 的文件
- 自动化只能依赖临时的路径约定

## 目标

- 让 package 配置和 distribution BOM 容易通过 pull request 审查。
- 为每个 package 提供稳定、抗重名冲突的 identity。
- 在仓库布局和 BOM 中区分不同 package type。
- 同时支持源优先的本地构建模式和预构建 artifact 消费模式，并且不改变仓库布局。
- 不把构建后的 package payload 放进 Git，无论它最终存放在 OCI、本地 registry、OCI layout 还是 agent cache。
- 区分 package 定义、build class 引用与可选 custom class descriptor、distribution profile、release BOM 和 channel 指针。
- 与独立的 `cluster-config` 仓库建立清晰边界，后者负责集群本地 target、input 和 patch。
- 适合私有集群采用 pull-based 同步。
- 默认不提交生成的 render 产物。

## 非目标

- 替换 OCI 作为 package 传输层。
- 定义完整的 GitOps controller 实现。
- 在共享 Git 仓库中保存 secret 值。
- 让 Git 成为唯一可选的本地配置后端。
- 在本文所需的 identity 和布局约定之外，重新设计现有 package 或 `BOM` schema。
- 定义 Git 仓库托管、认证或分支保护要求。

## 设计原则

- Git 保持可审查：保存源 YAML 和小型 patch，不保存构建后的 package payload 或 rendered bundle。
- Materialization 保持不可变：BOM 应锁定 source digest，并在存在预构建 artifact 时同时锁定 artifact image 和 digest。
- 所有权分离：全局发行版内容属于 `distribution-config`；集群本地 target、input 和 patch 属于独立的 `cluster-config` 仓库。
- Promotion 显式化：移动 channel 应该是一个很小、可审计的变更。
- Render 可复现：一个 Git revision 加上 source 和 artifact digest 应足以复现 desired state。

## 推荐仓库模型

平台团队维护一个 distribution configuration 仓库：

```text
distribution-config/
  packages/
    infra/
      kubernetes/
        v1.30.3/
          package.yaml
          files/
          manifests/
          hooks/
    network/
      cilium/
        v1.15.8/
          package.yaml
          values/
    policy/
      pod-security/
        v1.0.0/
          package.yaml
  classes/                    # 可选：custom 或 policy-pinned class descriptor
    site-overlay/
      v1.yaml
  profiles/
    default-platform/
      prod-amd64/
        defaults.yaml
        feature.mask.yaml
        package.mask.yaml
        support-matrix.yaml
  releases/
    default-platform/
      rev-20240424-prod/
        bom.yaml
  channels/
    default-platform/
      alpha.yaml
      beta.yaml
      stable.yaml
  policy/
    validation/
  README.md
```

该仓库应保存构建、校验、选择和渲染发行版所需的源文件。不应保存生成的 render 结果、下载后的 OCI artifact 内容或本地构建出的 package artifact。它也不需要携带 `rootfs/v1` 或 `manifest-bundle/v1` 等 Sealos built-in build class 的定义。

集群本地配置有意排除在该仓库模型之外。`ClusterTarget`、local inputs、patches、delivery policy 和 secret references 应放在独立的 `cluster-config` 仓库中。

## 目录职责

| 路径 | 职责 |
| --- | --- |
| `packages/<category>/<name>/<version>/` | 一个 package revision 的源配置。 |
| `packages/<category>/<name>/<version>/package.yaml` | package manifest，构建时会被复制到 materialized package root。 |
| `packages/<category>/<name>/<version>/build/` | 可选的 package-local build adapter 或 helper，由 `package.yaml` 引用。 |
| `classes/<name>/<version>.yaml` | 可选的 repo-local `BuildClass` descriptor，用于 custom、experimental 或 policy-pinned class。`rootfs/v1` 等 built-in class 从 Sealos class registry 解析，不需要 vendor 到仓库中。 |
| `profiles/<distribution>/<profile>/` | distribution 级默认值、feature mask、package mask 和 support matrix 规则。 |
| `releases/<distribution>/<revision>/bom.yaml` | 一个 release BOM，按 digest 锁定 source facts、build contract 和可选 package artifact。 |
| `channels/<distribution>/<channel>.yaml` | 从 channel 名称指向已批准 BOM revision 的小型指针文件。 |
| `policy/` | CI 或 promotion 自动化使用的校验规则。 |

## 哪些内容应该放进 Git

Git 适合保存小型、有意图、便于审查的文件：

- package manifest
- `package.yaml` 引用的 package 源文件
- package-local build recipe、adapter 和已声明的 build input metadata
- custom 或 policy-pinned class 使用的可选 repo-local build class descriptor
- distribution profile 默认值、mask 和 support matrix 规则
- release BOM 文件
- release channel 指针文件
- validation policy 和 CI 配置
- package ownership 和 release process 文档

Git 不应该保存：

- 构建后的 OCI image 或本地 package artifact 内容
- rendered desired-state bundle
- 下载后的 package artifact
- node-local cache 目录
- cluster target 文件、cluster-local inputs 和 cluster-local patches
- private key、token、certificate 或 secret 值
- 大型二进制依赖，除非没有实际可用的 artifact store 替代方案

## Package Identity 和 Category

每个 package revision 都应有一个 canonical identity：

```text
<category>/<name>@<version>
```

这些 identity 字段有不同职责：

| 字段 | 目的 |
| --- | --- |
| `category` | 描述 package 在发行版中的角色，并提供第一层名称隔离。 |
| `name` | category 内的短 package 名。 |
| `version` | BOM 选择的 package revision。 |

`category` 和 `name` 应使用小写 DNS-label 风格片段。`version` 应是稳定的 package revision 字符串，通常是 upstream semantic version 或 distribution-owned revision。

建议初始 categories：

| Category | 示例 | 职责 |
| --- | --- | --- |
| `infra` | `kubernetes`、`etcd` | 创建或维护集群所需的核心基础设施。 |
| `runtime` | `containerd`、`cri-o` | 节点或 workload runtime package。 |
| `network` | `cilium`、`calico` | CNI、networking 和 traffic infrastructure。 |
| `addon` | `cert-manager`、`metrics-server` | 可选或可替换的集群服务。 |
| `policy` | `pod-security`、`baseline` | policy、admission 和 compliance package。 |
| `tooling` | `sealctl`、`netshoot` | 支撑 lifecycle workflow 的运维工具。 |
| `patch` | `kubeadm-hardening` | 平台拥有的可复用 overlay，不是 cluster-local patch。 |

`category/name` 这个 tuple 在仓库内必须唯一。两个 package 只有在 `category` 不同时才可以共享同一个 `name`。BOM、profile、mask 和 validation rule 都应使用完整 identity 引用 package，而不是只使用短名称。

Provider、owner、ecosystem 和 upstream project name 在有用时应记录为 package metadata，但不应进入默认 package identity。

## Package 源目录

每个 package revision 都应在 `packages/<category>/<name>/<version>/` 下自包含。

示例：

```text
packages/infra/kubernetes/v1.30.3/
  package.yaml
  files/
    etc/kubernetes/kubeadm.yaml
    etc/kubernetes/audit-policy.yaml
  manifests/
    bootstrap/
    healthcheck/job.yaml
  hooks/
    preflight.sh
    bootstrap.sh
    healthcheck.sh
  build/
    package-build.sh
```

这个目录是 materialize package 的源目录。在预构建 artifact 模式下，CI 从该目录构建 package，推送到 registry，并在 release BOM 中记录 artifact digest。在源优先本地构建模式下，agent 可以从同一份源事实构建 package，而不依赖远程 OCI artifact。

Package-specific build knowledge 应留在 package source 附近。如果一个
package 需要 stage binaries、解包 archives、选择生成文件或运行 imperative
adapter，这个契约应声明在 `package.yaml` 中；对应的 package-specific helper
应放在该 package 的 `build/` 目录下。仓库级 `scripts/` 可以提供
`build-package --package infra/kubernetes/v1.30.3` 这样的通用入口，但不应成为唯一记录
Kubernetes 专属 staging 规则或 asset 名称的地方。

## Package-local Build Contract

`BuildClass` 是可复用的构建机制；package-local build contract 是 package owner
声明“这个机制应该如何处理当前 package source 目录”的地方。

如果 package 可以仅通过复制已声明 content 构建，它可以直接使用所选 `BuildClass`
的默认行为。对于需要外部资产或自定义 staging 的 package，source
`ComponentPackage` 应声明 `spec.build`：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: kubernetes-rootfs
spec:
  component: kubernetes
  version: v1.30.3
  class: rootfs
  build:
    class: rootfs/v1
    inputs:
      - name: kubeadm
        type: file
        required: true
        sourceRef: kubernetes-release:v1.30.3/bin/linux/amd64/kubeadm
        digest: sha256:...
      - name: kubelet
        type: file
        required: true
        sourceRef: kubernetes-release:v1.30.3/bin/linux/amd64/kubelet
        digest: sha256:...
      - name: kubectl
        type: file
        required: true
        sourceRef: kubernetes-release:v1.30.3/bin/linux/amd64/kubectl
        digest: sha256:...
    staging:
      - input: kubeadm
        path: rootfs/usr/bin/kubeadm
        mode: "0755"
      - input: kubelet
        path: rootfs/usr/bin/kubelet
        mode: "0755"
      - input: kubectl
        path: rootfs/usr/bin/kubectl
        mode: "0755"
    script:
      path: build/package-build.sh
```

`spec.build.class` 是 package 期望的默认 class。BOM 仍然 pin 某个 release
实际使用的 class；如果 `BOM.build.class` 与 `ComponentPackage.spec.build.class`
冲突，除非有明确策略允许覆盖，否则 validation 应失败。

Build inputs 是非 secret 的 package build assets，不是 cluster inputs。它们可以在足够小
时直接放入 package source，也可以通过 `sourceRef` 和 digest 从 local mirror、artifact
cache 或 upstream artifact store 解析。源优先本地构建模式必须在执行 build 前让这些
assets 在本地可用；它不能依赖未声明的网络 fetch。

`staging` 把已声明 input 映射到 materialized package root 中的路径。路径必须是相对路径，
不能逃逸 package root，也不能覆盖未声明的 content path。`script.path` 如果存在，必须指向
package source 目录内的文件，通常位于 `build/` 下；它只是已声明契约的 adapter，不是另一份事实源。

## BOM 布局

每个 release revision 应有一个 BOM 文件：

```text
releases/default-platform/rev-20240424-prod/bom.yaml
```

BOM 应锁定每个 package 不可变的 source 和 build contract。当存在预构建 artifact 时，同一个条目也可以锁定 artifact image 和 digest：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: default-platform-production
spec:
  revision: rev-20240424-prod
  packages:
    - category: infra
      name: kubernetes
      version: v1.30.3
      source:
        path: packages/infra/kubernetes/v1.30.3
        digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
      build:
        class: rootfs/v1
        profile: prod-amd64
      artifact:
        name: kubernetes-production-rootfs
        image: registry.example.io/sealos/kubernetes-production-rootfs:v1.30.3
        digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
        optional: true
      required: true
```

这样 review release 时可以聚焦在“哪些 source revision、build contract 和可选的预构建 package digest 进入了本次发行”。

BOM 不应记录当前指向它的 channel。Channel 归属是可变的发布意图，而 BOM revision 是不可变的发布内容。

当 `source` 和 `artifact` 同时存在时，artifact 必须被视为 pinned source 和 build contract 的 materialized result，而不是另一个事实源。

## Channel 布局

Channel 应该是很小的指针文件，而不是重复的 BOM：

```text
channels/default-platform/beta.yaml
```

示例：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ReleaseChannel
metadata:
  name: beta
spec:
  distribution: default-platform
  targetRevision: rev-20240424-prod
  bomPath: releases/default-platform/rev-20240424-prod/bom.yaml
```

从 `alpha` promotion 到 `beta`，或从 `beta` promotion 到 `stable`，就会变成一个只修改 channel target 的小 pull request。这样 package build review 和 promotion review 可以分开。

## 与 Cluster Configuration 的边界

`distribution-config` 和 `cluster-config` 应是两个独立的 Git 仓库：

| 仓库 | 所有者 | 内容 |
| --- | --- | --- |
| `distribution-config` | 平台团队 | package 源、build class、profile、BOM、channel、共享 validation policy |
| `cluster-config` | 集群或环境 owner | cluster target、private inputs、local patches、不对外暴露的环境配置 |

本文只定义 `distribution-config` 仓库。配套的 [cluster configuration proposal](cluster-config.zh-CN.md) 定义 `cluster-config` 仓库，包括 `ClusterTarget`、delivery policy、local inputs、patches 和 secret references。

Sealos agent 可以 clone 或 fetch 两个仓库，先从 `cluster-config` 读取集群本地意图，再从 `distribution-config` 解析 distribution channel、BOM、profile 和 package materialization 数据。

这保留了现有 global/local 所有权边界：

- global baseline：一次审查，通过共享 release channel promotion
- local patch：靠近集群保存，允许按环境差异化

仓库 URL、凭据和默认 Git ref 应由 agent 配置或部署 bootstrap 提供，不应嵌入共享 package 定义中。

## Source 和 Artifact Fulfillment 模式

仓库模型应支持两种主要 package fulfillment 模式，以及一种运维便利模式：

| 模式 | 行为 |
| --- | --- |
| `artifact` | 从 OCI 或其他配置的 artifact store 按 digest 拉取预构建 package artifact。 |
| `localBuild` | 从 pinned source facts 和 build contract 本地构建 package。 |
| `preferArtifact` | 当预构建 artifact 可用且策略允许时优先拉取；否则回退到 local build。 |

这些模式共享同一套 package source tree、build class、profile、BOM 和 channel。它们只在 agent 执行 render/apply 前如何 materialize package 上有区别。

BOM 应始终为可构建 package 标识 source facts：

```yaml
source:
  path: packages/infra/kubernetes/v1.30.3
  digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
build:
  class: rootfs/v1
  profile: prod-amd64
artifact:
  image: registry.example.io/sealos/kubernetes-production-rootfs:v1.30.3
  digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
  optional: true
```

`source.digest` 应是 build 所使用的标准化 source facts 的确定性 digest。`artifact.digest` 在 `artifact` 模式下必需，在 `localBuild` 模式下可选。如果存在，`artifact.digest` 是 pinned source 和 build contract 的缓存或分发句柄。

Delivery policy 可以由 distribution profile 默认值设置，也可以在策略允许时由 `cluster-config` 选择或覆盖：

```yaml
delivery:
  mode: artifact
```

改变 delivery mode 不应改变 package graph、feature resolution、profile defaults、input merge order 或 patch order。如果同一个 BOM 下 local build 和预构建 artifact fulfillment 生成了不同 desired state，应视为校验失败。

## Package 构建流程

Fulfillment mode 不是构建流程本身。package 构建流程是从 package source
revision 到 materialized package payload 的标准转换。CI 预构建、源优先本地构建、
离线镜像构建以及未来的 build service 都应使用同一套流程。

构建流程输入如下：

| 输入 | 是否必需 | 目的 |
| --- | --- | --- |
| package identity | 是 | BOM 选择的 `category`、`name` 和 `version`。 |
| `source.path` | 是 | 仓库内相对 package 源目录。 |
| `source.digest` | 是 | build 使用的标准化 source facts digest。 |
| `build.class` | 是 | 选择 builder 实现和 output kind 的版本化 build class。 |
| `build.platform` | 平台相关时必需 | 目标平台，例如 `linux/amd64`。 |
| `build.profile` | class 使用时必需 | 会影响 package payload 的 distribution profile 或 build profile 数据。 |
| `build.options` | 可选 | BOM 中显式记录的确定性、非 secret 构建选项。 |
| `ComponentPackage.spec.build` | 存在 package-specific build facts 时必需 | Package-local build inputs、staging rules 和可选 adapter script。 |

构建流程应按以下顺序执行：

1. 通过完整 package identity 解析 BOM package entry。
2. 解析 `source.path`，加载 `source.path/package.yaml`，并校验它是 source-form
   `ComponentPackage`。
3. 解析被引用的 `build.class`，并在 `ComponentPackage.spec.build.class` 存在时与它比较。
4. 解析 build contract 中声明的 build profile 或 platform 字段。
5. 从 package source、local mirror、artifact cache 或 digest-pinned asset store
   解析已声明的 package-local build inputs。
6. 标准化 build 使用的 source facts。它包括 `package.yaml`、
   `ComponentPackage.spec.build`、被引用的 package-local `build/` adapter、
   已声明 package source files，以及 digest-pinned build input metadata；排除
   generated output、local cache、downloaded artifacts 和被 ignore 的 workspace state。
7. 基于标准化 source facts 计算 `source.digest`，并与 BOM 中的值比较。
8. 在运行 builder 之前校验 `package.yaml`、package paths、package class、已声明 build
   inputs、staging rules 和依赖声明。
9. 在干净 workspace 中执行 build class；只有当 `ComponentPackage.spec.build` 中声明时，
   才应用 package-local staging rules 或 package-local adapter scripts。
10. 校验 materialized package root：它必须包含 `package.yaml`，manifest identity
   必须匹配 BOM entry，所有路径都必须留在 package root 内，且不能包含 cluster-local
   secret values。
11. 计算 materialized package digest，并记录 build provenance。
12. 根据选中的 fulfillment mode 存储或暴露输出。

build class 是让这套流程不绑定具体实现的可复用契约。标准 class 应内置在
Sealos 的 versioned class registry 中，因此每个 distribution 仓库都可以引用同一套
`rootfs/v1` 或 `manifest-bundle/v1` 行为，而不用复制 class 定义。Repo-local
`BuildClass` 文件是可选 extension descriptor：它可以描述由已安装 extension backing 的 custom
class、限制一个仓库允许的 built-in class，或 pin policy metadata，但不应成为执行 built-in
class 的前提。

未知 class 应 fail closed，除非当前运行的 Sealos binary 或已批准 extension 明确提供该 class
implementation。一个 build class version 应声明：

- builder implementation 或 command family
- output kind，例如 package root、OCI artifact、OCI layout 或 local registry image
- 支持的 package classes 和 platforms
- 会影响 `source.digest` 的 source include/exclude 规则
- 必需的非 secret build options
- 必须写入 output metadata 的 provenance fields

Package-specific asset names、binary staging maps 和 imperative helper paths
属于 `ComponentPackage.spec.build`，不属于可复用 build class。这样
`rootfs/v1` 这样的 class 可以同时复用于 Kubernetes、containerd 和其他 rootfs
packages，而不需要编码每个 package 的 asset layout。

建议的初始 built-in build classes：

| Class | 目的 |
| --- | --- |
| `rootfs/v1` | 复制或组装 rootfs package payload，包括已声明 binary staging。 |
| `manifest-bundle/v1` | 把已提交的 manifests、values 和 hooks 复制进 package artifact。 |
| `helm-render/v1` | 将已声明 Helm chart 和 values 渲染成 manifest bundle package。 |
| `patch-overlay/v1` | 对 base package 或 manifest bundle 应用已声明 overlays/patches。 |

不建议把 `script/v1` 作为默认 class。Package-local scripts 可以作为已声明 adapter 存在，
但 class taxonomy 应描述可复现 source shapes，而不是任意 shell entrypoints。

build class version 应被视为不可变。只要 class 的改变可能影响 package bytes、source
selection 或 output metadata，就应发布新的 class version。可复用行为由 class
implementation 负责，而不是由 distribution 仓库负责。Package 仓库只负责
`ComponentPackage.spec.build` 中声明的 package-specific source facts、build inputs、staging
rules 和可选 adapter。builder 不能读取 `cluster-config`、live cluster state、未声明的
host files 或 secret values。只有在网络输入被声明并按 digest pin，或者已经预先纳入
source facts 时，才允许使用网络输入。源优先本地构建模式必须能在不进行未声明远程 fetch 的情况下运行。

构建输出应携带足够 provenance，用来证明它由哪些输入产生：

```yaml
provenance:
  package: infra/kubernetes@v1.30.3
  source:
    path: packages/infra/kubernetes/v1.30.3
    digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
  build:
    class: rootfs/v1
    profile: prod-amd64
    platform: linux/amd64
  output:
    digest: sha256:3333333333333333333333333333333333333333333333333333333333333333
```

三种 fulfillment mode 以不同方式消费这套构建流程：

| 模式 | 与构建流程的关系 |
| --- | --- |
| `artifact` | 使用已经由该流程产出的 artifact。agent 在 render/apply 前校验 artifact digest 和 provenance。 |
| `localBuild` | 从 pinned source facts 和 build contract 在本地运行该流程，然后使用本地输出执行 render/apply。 |
| `preferArtifact` | 先尝试 `artifact`。如果 artifact 不可用、是 optional，并且策略允许 fallback，则运行 `localBuild`。 |

对于同一个 BOM package entry 和 build contract，`artifact` 与 `localBuild` 必须生成等价的
materialized package payload。如果两条路径都可用，但 package digest 或 render-visible
payload 不一致，则 release 无效，直到 source、build class 或 artifact provenance 被修正。

Cluster-local inputs、patches、secret bindings 和 delivery policy 不属于 package build。
它们在 package materialization 之后，作为 render/apply workflow 的一部分再应用。

## Distribution 解析契约

给定 `cluster-config` 选择的 distribution、channel、profile 和 delivery mode 后，agent 或 operator 应按确定性顺序解析 distribution 内容：

1. 将选中的 channel 解析到 `channels/<distribution>/<channel>.yaml`。
2. 用 channel 中的 `targetRevision` 和 `bomPath` 定位唯一 BOM 文件。
3. 校验 BOM 的 `spec.revision` 与 channel 的 `targetRevision` 一致。
4. 在 `profiles/<distribution>/<profile>/` 下解析选中的 profile。
5. 通过 built-in class registry 或由已安装 implementation backing 的已批准 extension descriptor
   校验被引用的 build class，并校验 package source path。
6. 对每个 package，按策略拉取 pinned artifact、从 pinned source facts 构建，或使用 `preferArtifact` fallback 规则完成 materialization。
7. 将 package defaults、profile defaults 和 materialized package payload 暴露给 cluster render/apply workflow。

如果引用文件缺失、必需的 source digest 缺失、拉取到的 artifact 与 artifact digest 不匹配、local build 无法证明 source digest、策略要求 artifact 但 artifact 不可用，或必需的 secret 值不可用，解析应拒绝继续。

## 同步流程

推荐 Day 0 和 Day N 流程：

1. package author 更新 `packages/<category>/<name>/<version>/`。
2. CI 校验 `package.yaml`，计算 source digest，检查 build contract，并在干净 workspace 中运行标准 package 构建流程。
3. 在预构建 artifact 模式下，CI 发布构建出的 package artifact 并记录 digest。在源优先本地构建模式下，CI 可以在 validation 和 test-build evidence 后停止，因为集群可以从 pinned source facts 重新构建。
4. release automation 在 `releases/<distribution>/<revision>/` 下写入或更新带 source digest 和可选 artifact digest 的 BOM。
5. reviewer 审批带 source 和 artifact digest pin 的 BOM。
6. promotion 更新 `channels/<distribution>/` 下的 channel 指针。
7. cluster agent 从两个仓库拉取 Git 变更，从 `cluster-config` 读取集群本地意图，从 `distribution-config` 解析选中的 channel 和 BOM，按 delivery policy materialize packages，然后执行 render/apply。

关键拆分是：Git 同步 source facts 和发布意图，OCI 同步可选的预构建 package 内容。

## 生成产物

默认不应提交 rendered bundle。它是以下输入的确定性构建结果：

- 选中的 channel 或 BOM revision
- package source facts 和 materialized package payload
- distribution profile defaults 和 masks
- 来自 `cluster-config` 的 cluster-local inputs 和 patches

生成产物应放在 agent workspace、local cache、local registry、OCI layout 或 CI artifacts 中。rendered bundle 或本地构建的 package 可以作为调试材料附加到 release，但不应成为主要事实源。

## Secret 处理

Secret 不应保存在 distribution 仓库中。

可接受模式：

- package manifest 可以声明必需的 secret-shaped inputs
- secret 值和集群特定 secret reference 属于 `cluster-config` 或 runtime secret store
- hydration 阶段应从集群内 secret store 注入敏感值
- certificate 和 private key 不进入 package artifact，而是作为 local input 提供

distribution 仓库只定义“需要某类 secret”，不定义具体值或集群本地绑定。

## 校验

distribution configuration 仓库的 CI 应校验：

- 每个 `package.yaml` 都能按 `ComponentPackage` 解析
- `package.yaml` 引用的 package path 存在
- 每个 BOM 都能按 `BOM` 解析
- 每个 package identity 都使用合法的 `category`、`name` 和 `version`
- 每个 `category/name` tuple 都唯一
- 每个可构建 BOM package 都指向 source path 和 source digest
- 每个可构建 BOM package 都声明受支持的 build class
- 每个被引用的 build class 都由 Sealos built-in class registry，或由已安装 implementation backing 的已批准 extension descriptor 提供
- 每个已批准 build class version 都声明 output kind、支持的 package classes、支持的平台，以及必需 provenance fields
- 每个 package-local build input 都是非 secret；如果解析到 package source 之外，必须按 digest pin
- 每个 package-local staging path 都相对于 materialized package root，并引用已声明的 build input
- 每个 source digest 都可以从标准化 source facts 重新计算
- BOM 中记录的每个 release-level build option 都是确定性、非 secret 的
- 每个 package-local build adapter 都被 `ComponentPackage.spec.build` 引用，并位于 package source 内
- 每个可构建 package 都可以在干净 workspace 中构建，且不读取 `cluster-config`、live cluster state、未声明 host files 或 secret values
- 每个 `artifact` 模式 package 都指向 image 和 digest
- 每个 digest 格式正确，并且不接受缺少 digest pin 的可变 tag
- 每个 BOM package 都有匹配的 package source 或已批准的外部 artifact
- 每个产出或批准的 artifact 都记录与 BOM source 和 build contract 匹配的 provenance
- 当 local build 和 artifact 两条路径都可用时，它们解析到等价的 materialized package payload
- 每个 channel target revision 都与被引用 BOM 的 revision 一致
- 每个 channel pointer 都引用已存在的 BOM path
- 每个 profile 引用已存在的默认值、mask、完整 package identity 和受支持 feature
- 每个 build class 引用都指向受支持的 built-in class，或由已安装 implementation backing 的已批准 custom class
- delivery mode 变化不能改变解析后的 package graph 或 patch ordering
- generated output path 和 local cache 已被 Git ignore

这些校验可以先作为仓库本地脚本存在，之后再演进成一等的 `sealos sync` 命令。

## 备选方案

### 所有内容放在一个扁平目录

不推荐，因为 package source、release intent、channel promotion 和 cluster-local patch 有不同 owner 和 review cycle。

### 把构建后的 package 直接放进 Git

不推荐，因为 Git 不适合传输大型不可变 payload。OCI 已经提供 digest-addressed artifact 分发和本地缓存。

### 每个 channel 保存一份 BOM

不推荐，因为会复制 release 定义。Channel 文件应该指向 BOM revision，这样 promotion 只是一个小型元数据变更。

### 把集群 override 放进组件目录

不推荐，因为这会混合全局 package ownership 和环境特定配置，使 package promotion 变得不安全。

## 开放问题

- Sealos 是否应定义一等的 `ReleaseChannel` schema，还是先把 channel pointer 作为仓库约定？
- 应使用什么精确 canonicalization algorithm 计算 package source tree 的 `source.digest`，才能跨平台保持确定性？
- 是否允许 custom repo-local build class 执行任意代码，还是应限制为描述随 Sealos 安装的 extension implementation？
- validation 如何发现没有 source under `packages/` 的外部 package artifact？
- 对于强监管环境，是否允许将 rendered bundle snapshot 放入专用 audit repository？

## 建议

新工作可以先使用以下路径约定：

```text
packages/<category>/<name>/<version>/package.yaml
profiles/<distribution>/<profile>/defaults.yaml
releases/<distribution>/<revision>/bom.yaml
channels/<distribution>/<channel>.yaml
```

这样平台团队可以获得稳定的 review、automation、promotion 和 pull-based cluster synchronization 路径，同时不需要改变现有 OCI package 和 BOM 模型。集群本地路径由配套的 [cluster configuration proposal](cluster-config.zh-CN.md) 定义。
