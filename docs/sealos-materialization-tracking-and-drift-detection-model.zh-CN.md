# 子设计：Materialization Tracking 与 Drift Detection 模型

## 状态

Draft

## 摘要

这份文档定义 Sealos 应该如何追踪 shared package content 和 cluster-local
repo content 最终 materialize 出来的结果，如何把这些结果和 live state
做比对，包括那些通过 kube-apiserver 暴露、底层存进 etcd 的 Kubernetes
资源状态，以及 bootstrap 之后生成出来的 host-side 文件。

核心规则是：漂移检测不能只追踪 source artifact，还必须追踪这些 artifact 在
hydrate、apply 和 bootstrap 之后变成了什么具体投影。

## 相关文档

- 顶层架构：
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md)
- reconcile、ownership 与 drift 语义：
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- package contract 与 content 类型：
  [sealos-component-package-format-design.md](./sealos-component-package-format-design.md)
- local repo 与 Secret 处理模型：
  [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md)
- Local patch policy 的 source 与 scope：
  [sealos-local-patch-policy-design.md](./sealos-local-patch-policy-design.md)
- operator action 速查：
  [sealos-sync-operator-action-reference.md](./sealos-sync-operator-action-reference.md)
- 当前 applied revision schema：
  [pkg/distribution/state/types.go](../pkg/distribution/state/types.go)
- 当前 materialization 路径：
  [pkg/distribution/reconcile/materialize.go](../pkg/distribution/reconcile/materialize.go)
- 当前 apply 行为：
  [pkg/distribution/reconcile/apply.go](../pkg/distribution/reconcile/apply.go)

## 为什么需要这份子设计

ownership 文档定义了谁有权定义 desired state。package-format 文档定义了包里
可以放什么。但这还缺一层很实际的设计：

- 每种 content 最终应该追踪哪个 live object 或哪个 live file？
- 对不同对象应该使用什么 compare 规则？
- 那些并不直接存放在 package 里的生成型输出应该如何追踪？
- 哪些 live object 只是被观察，哪些才是真正的 `globalBaseline`-owned desired
  state？

这份文档就是为了解答这些问题，而不把它们重新塞回 package contract 或
release policy 文档里。

## 范围

### In Scope

- global package content materialize 成 live filesystem 或 Kubernetes state
  之后如何追踪
- local repo content materialize 成 live filesystem 或 Kubernetes state
  之后如何追踪
- 不同 materialization 类型对应的 compare 策略
- 通过 kube-apiserver 暴露、底层持久化在 etcd 中的 Kubernetes API object
  state 应该怎么处理
- kubeadm 生成的 static Pod manifest 这类 host-side generated file 怎么处理
- 除了当前 `AppliedRevision` 之外，最少还需要什么 inventory

### Out Of Scope

- 最终 CRD 或 CLI 形态
- server-side apply 的完整 field-manager 策略
- 为未来所有应用包设计通用 parser
- promotion policy 和 release channel 行为

## 两层追踪

Sealos 应该在两层上追踪状态。

### 1. Source Tracking

它回答的是：哪些输入定义了这次 intended state？

例如：

- 选中的 `BOM` revision
- 选中的 `ComponentPackage` digest
- local repo revision hash
- input payload digest
- local patch digest

当前仓库已经通过 `AppliedRevision` 记录了这层的粗粒度版本，也就是 `BOM`、
一个 `localPatchRevision` 以及 render 后的 `desiredStateDigest`。

### 2. Projection Tracking

它回答的是：这些输入最后在 live system 中变成了什么？

例如：

- 节点上的 `/usr/bin/kubelet`
- `/etc/kubernetes/kubeadm.yaml`
- `DaemonSet/cilium`
- `Deployment/grafana`
- 本地的 `Secret/grafana-admin-credentials`

漂移检测需要这两层同时存在：

- 只有 source tracking，无法解释到底哪个文件或对象 drift 了
- 只有 projection tracking，无法解释是哪一个 revision 或 local input
  产生了期望状态

## Materialization 类型

当前 apply 路径已经区分了 `rootfs`、`file` 和 `manifest`。漂移检测应该沿用这
个方向，但把投影模型再明确一点。

| Projection 类型 | 来源 | live identity | 推荐 compare 策略 | 典型 ownership |
| --- | --- | --- | --- | --- |
| `hostPath` | package 里的 `rootfs/` 或 `files/` content | 节点绝对路径 | 字节 digest + 是否存在 + mode | `global` 或 `local` |
| `k8sObject` | package manifest 或 local repo resource | `group`、`kind`、`namespace`、`name` | 规范化后的 Kubernetes object compare | `global` 或 `local` |
| `generatedHostPath` | 由 tracked inputs 驱动的 hook 或外部生成器 | 节点绝对路径 + generator identity | 对生成意图做语义 compare | 通常是 mixed |
| `runtimeObject` | controller、operator 或运行时副作用 | `group`、`kind`、`namespace`、`name` | 只观察 | runtime-local |

那些“底层存放在 etcd 里”的 Kubernetes 资源状态，仍然只是 `k8sObject`
projection，并不需要一个单独的 `etcdYaml` 类型。稳定的 compare target
应该是 kube-apiserver 返回的规范化 API object，而不是 etcd 里的原始序列化结果。

这里最关键的是区分 `hostPath` 和 `generatedHostPath`：

- `hostPath` 表示 Sealos 直接写入了这份文件
- `generatedHostPath` 表示 Sealos 追踪的是输入和生成器，但最终文件字节是别的
  工具生成的

## Compare 策略

不同投影类型应该对应不同的 compare 规则。

| Compare 策略 | 适用对象 | 说明 |
| --- | --- | --- |
| `bytewiseFile` | 二进制、普通配置文件、直接写入的 host file | 比较内容 hash、存在性和文件 mode。 |
| `normalizedK8sObject` | package manifest、local repo resource、允许的本地 Secret 对象 | 忽略 `status`、`managedFields`、`resourceVersion`、`uid` 等 server-assigned 元数据，只比较 ownership 相关字段。这是处理 etcd 中资源状态的主 compare 策略。 |
| `semanticGeneratedFile` | kubeadm 生成的 static Pod manifest 这类 host-side generated file | 解析生成结果，只比较受 tracked intent 约束的字段，忽略格式、key 顺序和非语义重写。 |
| `observeOnly` | operator 运行时生成的连接 Secret 之类对象 | 不把这个对象本身当成 `globalBaseline`-owned desired state。 |

一个实用规则是：

- 如果 Sealos 直接写了字节，可以按字节比较
- 如果字节是别的系统生成的，就应该按语义比较
- 如果对象是别的系统拥有的运行时对象，Sealos 不应该假装自己拥有它的完整期望形态

对于 Kubernetes 资源，Sealos 不应该去比较“etcd 里的 YAML”。Etcd 只是存储后端。
真正的 compare target 应该是 kube-apiserver 返回的规范化 object。

## Global Content 和 Local Repo Content 如何映射进追踪

追踪应该沿着 ownership 边界走，而不是只看它来自哪个目录。

### Global Package Content

global package content 一般会变成下面几种之一：

- `hostPath`
- `k8sObject`
- 或者成为后续生成型投影的输入

例如：

- `rootfs/usr/bin/kubelet` -> `hostPath`
- `files/etc/kubernetes/kubeadm.yaml` -> `hostPath`
- `manifests/cilium.yaml` -> `k8sObject`
- `hooks/bootstrap.sh` + `kubeadm.yaml` -> `generatedHostPath` 的 generator input

### Local Repo Inputs

local repo 下的 `inputs/` 更应该先被当成 source record，而不是直接被当成
live object。

例如：

- cluster-specific `cilium-values.yaml`
- cluster-specific `kubeadm.yaml` payload
- local registry mirror override

这些 input 会影响后续 projection，但除非后续步骤把它 materialize 成对象，
否则它自己不是 live API object。

### Local Repo Resources

local repo 下的 `resources/` 一般应该变成 `k8sObject` projection。

例如：

- `Secret/grafana-admin-credentials`
- `ExternalSecret/grafana-db-root`
- 一个本地 `ConfigMap`

这些是 live object，应该像 package-provided manifest 一样按 Kubernetes
identity 来追踪，只是 ownership scope 是 `local`。

### Local Repo Patches

local repo 下的 `patches/` 应该被追踪两次：

- 一次作为 source record，记录 patch digest 和 target identity
- 一次通过它修改后的最终 projection 来追踪

patch 文件本身不是 live object，目标对象或目标文件才是。

在当前单节点 MVP 里，这个 patch 形态刻意收得很窄：

- `patches/<component>/**/*.yaml`
- 每个 YAML 文档都是一个 partial Kubernetes object overlay
- 目标对象通过 `apiVersion`、`kind`、`metadata.name`，以及通常还需要的
  `metadata.namespace` 来标识
- render 会把 patch merge 到匹配的 package manifest object 上，同时把 patch
  文档本身保留在 bundle 里，作为 `localPatch` fragment 参与 ownership-aware
  compare
- patch 文件不会被当成独立资源直接 apply
- 当前 validator 只放行一小组 local patch path，主要包括
  `ConfigMap.data` / `binaryData`、workload placement 字段、少量
  secret-name reference，以及 ingress / service 暴露相关字段

## 例子：如何追踪存放在 etcd 中的资源状态

如果运维人员说“etcd 里的 YAML”，正确的设计对象其实应该是 Kubernetes API
object，例如：

- `DaemonSet/cilium`
- `Deployment/grafana`
- `Secret/grafana-admin-credentials`

这些资源在底层可能物理存储在 etcd 里，但 Sealos 不应该追踪 etcd 原始字节。
它应该追踪 object identity，并比较 kube-apiserver 暴露出来的规范化 API object。

### 应该追踪什么

对于一个存在 etcd 里的 Kubernetes object，例如 `DaemonSet/cilium`，Sealos
至少应该追踪：

- 引入这个对象的 component 和 package revision
- 对象 identity：
  `group`、`kind`、`namespace`、`name`
- 被追踪字段对应的 ownership scope
- 规范化后的 desired object digest 或字段级 inventory
- 如果 local binding 或 local-owned overlay 参与了这个对象，还要记录
  local repo revision
- 上次成功 apply 时这份对象形态的 normalized digest

### 不应该假设什么

Sealos 不应该假设：

- etcd 里存着这份对象的 canonical YAML
- etcd 原始字节是一个稳定的 compare surface
- kube-apiserver 返回的对象会和最初提交的 manifest 在字节上完全一致
- 同一个 Kubernetes object 里的所有字段都属于同一个 ownership scope

### 一个 API Object 里的 ownership 也可能是混合的

一个存在 etcd 里的 Kubernetes object 正好说明为什么有时候“按整个对象判
ownership”是不够的。

在同一个 API object 里，不同字段可能来自不同 ownership。

典型例子：

- `global-owned`
  - package-owned label 和 annotation
  - 主 container image
  - shared command、volume 和 probe 结构
- `local-owned`
  - 被允许的本地 overlay，例如 `nodeSelector`、toleration 或 secret-name
    binding
  - 通过 declared input 传进来的 cluster-specific reference
- `runtime-owned`
  - `status`
  - `managedFields`
  - server-assigned metadata
  - controller 产生的观测字段

所以 drift classification 应该是：

- 只是 defaulting 或 status 变化 -> 仍然 `Clean`
- local-owned 字段被手工改了但没写回 local repo -> `Dirty`
- global-owned 字段被直接改了 -> `Orphan`

## 单独的补充例子：如何追踪生成型 Host 文件

生成型 host file 是另一类对象，它和 etcd 中的资源状态不是一回事，但同样需要
被追踪。

当前仓库里的 Kubernetes PoC package 携带的是：

- `files/etc/kubernetes/kubeadm.yaml`
- 一个执行 `kubeadm init` 的 bootstrap hook

见：

- [scripts/poc/minimal-single-node/packages/kubernetes/package.yaml](../scripts/poc/minimal-single-node/packages/kubernetes/package.yaml)
- [scripts/poc/minimal-single-node/packages/kubernetes/hooks/bootstrap.sh](../scripts/poc/minimal-single-node/packages/kubernetes/hooks/bootstrap.sh)

所以像下面这些文件：

- `/etc/kubernetes/manifests/kube-apiserver.yaml`
- `/etc/kubernetes/manifests/kube-controller-manager.yaml`
- `/etc/kubernetes/manifests/kube-scheduler.yaml`

都应该被当成 `generatedHostPath` projection。

对于这种 generated host file，Sealos 至少应该追踪：

- `kubernetes-rootfs` package revision
- bootstrap hook identity
- hydrate 后 `kubeadm.yaml` 的 render digest
- 提供 cluster-specific 值的 local repo revision
- 生成目标路径
- 这份 static Pod manifest 在上次成功 apply 时的 normalized digest

当前单节点 MVP 说明：

- 仓库现在会追踪这条链路里的 3 个已知 generated host file：
  `/etc/kubernetes/manifests/` 下的 `kube-apiserver.yaml`、
  `kube-controller-manager.yaml` 和 `kube-scheduler.yaml`
- 它会把这些路径都记录成由 Kubernetes bootstrap hook 通过 `kubeadm`
  产出的 `generatedHostPath`
- 当前 compare 语义是刻意收窄的：
  只校验它是不是 namespace `kube-system` 下的 Kubernetes `Pod`，
  并额外检查是否存在预期的 control-plane container
- 它现在还会从 render 后的 `kubeadm.yaml` 推导一个字段级期望：
  每个被追踪 static Pod 对应 control-plane container 的预期镜像
- 它现在还会从同一份 rendered input 里推导一小组已知字段：
  预期的 command 名、部分 flags
  （例如 `--service-cluster-ip-range`、`--cluster-cidr`），以及少量预期的
  volume mount（例如 `/etc/kubernetes/pki`）
- 当前 parser 也兼容 kubeadm 里两种常见的 `extraArgs` 形态：
  map 形态和 `name`/`value` 列表形态；同时也会从 `extraVolumes`
  推导额外的预期 mount
- 它仍然不会把整份 generated manifest 按完整的字段级 desired intent
  模型做比较
- 今天只有 `sync diff` 和 `sync status` 会直接报告这类 projection；
  `sync revert` 和 `sync commit` 还不会直接管理它
- 当前 CLI 输出还会带一个 generated-projection remediation hint：
  语义字段 drift 会把运维人员指回 rendered `kubeadm` input，而 parse 级失败
  会被归类成需要人工 review 的情况
- 这个 hint 现在还会区分“谁应该改 source of truth”：
  `changeOwner=localInput` 表示这类 drift 可以通过 cluster-local bootstrap
  input 收敛，`changeOwner=globalBaseline` 表示问题应回到选中的 BOM/package
  global baseline，而 `changeOwner=manualReview` 表示 Sealos 目前无法安全自动归类
- 当前 CLI payload 里还会带一份很小的 operator playbook：
  `nextSteps[]` 给出顺序化的后续动作，`allowedCommands[]` 列出当前状态下
  可以安全使用的 Sealos 命令
- 对 generated projection，`commandGuidance[]` 现在还会带命令级 precondition
  和求值后的 `availability`，这样 `sync diff/status` 不只知道“哪个命令相关”，
  还知道它当前是否被像 bundle digest 不匹配这样的前提挡住

## 当前普通 Drift 的 Remediation 模型

现在带 operator guidance 的不只是 generated projection。在当前单节点 MVP 里，
`sync diff` 和 `sync status` 也会给普通 `k8sObject` drift 和 direct
`hostPath` drift 带上 remediation block。

这里的核心还是让 `changeOwner` 和真正应该吸收这个修改的 ownership 边界保持一致：

| Projection | 典型 drift owner | 当前 `changeOwner` | 典型 action |
| --- | --- | --- | --- |
| package-owned `k8sObject` | 选中的 package 或 BOM global baseline | `globalBaseline` | `reviewDistributionBaselineForAppliedObject` |
| local-owned `k8sObject` | local repo patch 或 local resource | `localOverlay` | `reviewLocalObjectOverlayAndCommitOrReapply` |
| package-owned direct `hostPath` | 选中的 package 或 BOM global baseline | `globalBaseline` | `reviewDistributionBaselineForHostPath` |
| local-owned direct `hostPath` | local repo input binding | `localInput` | `reviewLocalHostInputAndCommitOrReapply` |
| generated `generatedHostPath` | local bootstrap input、global baseline 或人工 review | `localInput`、`globalBaseline` 或 `manualReview` | 上一节已经覆盖 |

### 普通 Kubernetes 对象

对普通 `k8sObject` projection，当前 CLI 行为是：

- `global` object 只要是 `Drifted` 或 `Missing`，都会被当成
  `Orphan` 类问题，并指回选中的 package/BOM global baseline
- `local` object 只要是 `Drifted` 或 `Missing`，都会被当成 `Dirty`
  的 cluster-local overlay 问题，并指回 local repo
- remediation payload 里现在会带：
  - `action`
  - `changeOwner`
  - `source`
  - 可选的 `policyName` 和 `policyEligiblePaths[]`
    当 drift 的 object field 已经落在当前默认 `LocalPatchPolicy` 的允许面内时
  - `nextSteps[]`
  - `allowedCommands[]`
  - `commandGuidance[]`

当前单节点 MVP 对普通对象的 guidance 仍然刻意收得很窄：

- `globalBaseline` object drift 会给出 `sync diff`、`sync status`、
  `sync revert`、`sync render`、`sync apply`、`sync package build`、
  `sync package push` 这类命令
- 如果 package-owned object 的 drift 只发生在默认 `LocalPatchPolicy`
  已覆盖的字段上，remediation block 仍然会把当前 live state 归到
  `globalBaseline`，但现在会额外给出 `policyName` 和
  `policyEligiblePaths[]`，明确表示长期修复可以落成 local repo patch，
  不一定非得 fork package/BOM
- `localOverlay` object drift 在 live object 还存在时，会给出
  `sync diff`、`sync status`、`sync commit`、`sync revert`、
  `sync render`、`sync apply`
- 如果 local-owned object 已经缺失，当前 guidance 会移除 `sync commit`，
  改为把运维人员指向 `sync revert` 或本地修改 local repo

### Direct Host Path

对 direct `hostPath` projection，当前 CLI 也是按同样的 ownership split
来处理：

- `global` direct host path 会指回选中的 package/BOM global baseline
- `local` direct host path 会指回当初绑定这份文件的 cluster-local input
- 当前单节点 MVP 只会在“这个 local-owned direct host file 来自已声明的 input
  binding，且 live 文件仍然存在”时，给出 `sync commit`
- 如果 local-owned host file 已经缺失，当前 guidance 会指向 `sync revert`，
  不会建议 `sync commit`

### 命令前提

这个 remediation payload 不只是静态 playbook。`commandGuidance[]` 现在还会带
已经求值过的命令可用性。

在当前单节点 MVP 里，最主要的前提是：

- `bundleMatchesRecordedDesiredStateDigest`

它的意思是：

- `sync diff` 和 `sync status` 总能解释当前 drift
- 那些会改变 live state 或推动当前 desired state 的命令，尤其是
  `sync revert`、`sync commit` 和 `sync apply`，会根据“当前检查的 bundle
  是否仍然等于集群记录下来的 desired-state digest”被标成 `available`
  或 `blocked`

这层区分很重要，因为如果当前 bundle 已经和集群记录下来的 desired state
脱节，operator guidance 就不应该继续建议直接执行 live repair 命令。

## 超出 `AppliedRevision` 的 Inventory

当前 `AppliedRevision` 仍然有价值，但它太粗了，无法解释到底预期有哪些投影，
以及它们应该如何比较。

Sealos 之后应该在 revision snapshot 旁边保留一份更细粒度的
materialization inventory。

一个示意形态可以是：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedInventory
metadata:
  name: default
spec:
  bom:
    name: default-platform
    revision: rev-007
  localRepoRevision: sha256:...
  entries:
    - component: kubernetes
      ownership: global
      projectionClass: hostPath
      compareStrategy: bytewiseFile
      target:
        path: /etc/kubernetes/kubeadm.yaml
      source:
        kind: packageContent
        packageDigest: sha256:...
        bundlePath: files/etc/kubernetes/kubeadm.yaml
      desiredDigest: sha256:...
    - component: cilium
      ownership: global
      projectionClass: k8sObject
      compareStrategy: normalizedK8sObject
      target:
        group: apps
        kind: DaemonSet
        namespace: kube-system
        name: cilium
      source:
        kind: packageManifest
        packageDigest: sha256:...
        bundlePath: manifests/cilium.yaml
      desiredDigest: sha256:...
    - component: kubernetes
      ownership: mixed
      projectionClass: generatedHostPath
      compareStrategy: semanticGeneratedFile
      target:
        path: /etc/kubernetes/manifests/kube-apiserver.yaml
      source:
        kind: generated
        generator:
          component: kubernetes
          hook: bootstrap
          tool: kubeadm
        inputs:
          - sha256:...
          - sha256:...
      lastAppliedNormalizedDigest: sha256:...
```

这个 schema 只是示意，不是最终 API 承诺。真正重要的是数据形态，而不是字段名。

## 建议的 Drift Detection 流程

一个合理的 first-pass control loop 可以是：

1. 从 `BOM + local repo` materialize 出 bundle。
2. 基于下面这些来源构建 projection inventory：
   - render 后的 `rootfs` step
   - render 后的 `file` step
   - render 后的 `manifest` step
   - local repo resources
   - 已声明或已知的 generated outputs
3. apply desired state。
4. apply 成功后记录：
   - revision snapshot
   - projection inventory
   - generated projection 的 normalized digest
5. 后续 reconcile 时，按 entry 自己的 compare strategy，把 live state 和
   inventory 做比对。
6. 最后结合：
   - ownership scope
   - projection compare 结果
   来做 drift classification。

这样 revision tracking 和 live-object tracking 就能分层，但仍然互相关联。

## MVP 边界

第一个 MVP 不需要一个适配所有 operator 和所有应用包的通用追踪引擎。

但它至少需要为下面几类对象定义清楚 first-pass 模型：

- 直接的 `rootfs` 和 `file` projection
- 直接来自 manifest、底层 live state 存在 etcd 里的 Kubernetes 对象
- 一小组已知生成型输出，尤其是 kubeadm 生成的 static Pod manifest
- local repo 里的 Secret 和 `ExternalSecret` resource

这样才足以让 Kubernetes bootstrap 路径、Cilium 包流程和最初的 stateful
例子在概念上闭合起来。

## CLI 输出里的 Observation Layers

CLI 不应该把所有和 drift 相关的语义都压成一个字段。

至少要明确区分 3 层：

1. 当前 compare 结果
   - 也就是 `sync diff` 此刻把 rendered bundle 和 live tracked object 做比较后
     直接看到的结果。
   - 这里是原始 compare payload，包含逐对象的 mismatch path。
   - 当前 CLI 里，这层体现在 `sync diff.currentCompare`。

2. 已持久化的 observed snapshot
   - 只有在 rendered bundle digest 和 cluster 当前记录的 desired-state digest
     一致时，才可以安全写回 `AppliedRevision.status.observedSummary` 的摘要。
   - 这里适合承载稳定计数，例如 `dirty`、`orphan` 和
     `mixedOwnershipObject`。
   - 当前 CLI 里，这层体现在
     `sync diff.persistedObservedSummary` 和
     `sync status.recordedObservedSummary`。

3. 记录中的 revision state
   - 也就是 `AppliedRevision.status.state` 这个 cluster 级状态。
   - 它回答的是一个更粗的问题：当前记录的 revision 现在到底是
     `Clean`、`Dirty`、`Orphan` 还是 `Degraded`。
   - 当前 CLI 里，这层体现在 `sync status.recordedState`，以及
     `sync diff.recordedRevision.state`。

把这三层分开，可以避免两类常见误解：

- 把当前原始 compare 结果误当成已经持久化的 observation snapshot
- 把持久化的 observed snapshot 误当成完整的 recorded revision state

这层区分也让临时 bundle 检查更安全。如果 `sync diff` 或 `sync status`
指向了一个临时的 `--bundle-dir`，而这个 bundle 又和 cluster 当前记录的
desired-state digest 对不上，Sealos 仍然应该返回当前 compare 结果，但不应
默默覆盖已经记录的 observed snapshot 或 revision state。

## Local Patch Policy Artifact

当前单节点 MVP 里，local-patch policy 已经不再只是一个隐含在代码里的常量。

现在每个 rendered bundle 都会显式携带一份 policy artifact：

- `bundle.spec.localPatchPolicySource`
- `bundle.spec.localPatchPolicyScope`
- `bundle.spec.localPatchPolicyName`
- `bundle.spec.localPatchPolicyPath`
- `bundle.spec.localPatchPolicyDigest`

并且 policy 文档本身会被写到这个路径上；当前默认路径是
`policy/local-patch-policy.yaml`。

如果 local repo 自己提供了 `policy/local-patch-policy.yaml`，render 会把它复制进
bundle，并让它成为下面这些路径共享的 source of truth：

- render 阶段的 local patch validation
- compare 阶段的 `policyEligible` mismatch annotation
- `sync commit` 阶段的 local patch overlay 提取

如果 local repo 没提供，render 仍然会把一份显式的默认 policy 写进 bundle，
这样这个 rendered revision 仍然是自描述的。

换句话说，当前单节点 MVP 现在已经把 policy ownership 说死成了显式模型：

- `localPatchPolicySource: localRepo` 表示这份 policy 由 cluster-local repo 定义
- `localPatchPolicySource: builtInDefault` 表示 Sealos 把内置默认 policy 渲染进了
  bundle
- `localPatchPolicyScope: clusterLocal` 表示这份 rendered artifact 只治理
  cluster-local override surface；当前还不支持 package/BOM-scoped 的
  local-patch policy
- package 和 BOM 目前都还不是 local-patch policy 的合法来源

## 当前 `sync diff` 和 `sync status` 的输出形态

当前单节点 MVP 已经会在 CLI YAML 里直接暴露这些层次。下面的示例刻意做了缩写，
只保留最能表达状态模型的字段，不试图把每个计数器和每条 mismatch 都完整列出来。

### 例子：`sync diff`

```yaml
clusterName: demo
bomName: minimal-single-node
revision: rev-poc-001
channel: alpha
bundlePath: /var/lib/sealos/demo/distribution/current
appliedRevisionPath: /var/lib/sealos/demo/distribution/applied-revision.yaml
localPatchPolicy:
  source: builtInDefault
  scope: clusterLocal
  name: defaultLocalPatchPolicy
currentState: Orphan
headline: state=Orphan; dirtyObjects=0; orphanObjects=1; dirtyHostPaths=0; orphanHostPaths=0; directCommitEligible=0; directRevertEligible=0; bundleMatchRequired=0; policyEligibleOrphanObjects=1
observationPersisted: true
persistedObservedSummary:
  total: 2
  matched: 1
  drifted: 1
  clean: 1
  orphan: 1
  directCommitEligible: 0
  directRevertEligible: 1
  bundleMatchRequired: 1
operatorActionSummary:
  directCommitEligible: 0
  directRevertEligible: 0
  bundleMatchRequired: 0
recordedRevision:
  desiredStateDigest: sha256:...
  localRepoRevision: sha256:...
  state: Orphan
  observedSummary:
    orphan: 1
    directCommitEligible: 0
    directRevertEligible: 1
    bundleMatchRequired: 1
policyEligibleOrphanObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: kube-system
    name: cilium-config
    operatorAction: promoteToLocalPatch
    operatorActionMetadata:
      allowsDirectCommit: false
      allowsDirectRevert: false
      requiresBundleMatch: false
    paths:
      - data.enable-hubble
    remediation:
      action: reviewDistributionBaselineForAppliedObject
      changeOwner: globalBaseline
      policyName: defaultLocalPatchPolicy
      policyEligiblePaths:
        - data.enable-hubble
currentCompare:
  summary:
    total: 2
    matched: 1
    drifted: 1
    clean: 1
    orphan: 1
  objects:
    - tracked:
        apiVersion: apps/v1
        kind: DaemonSet
        namespace: kube-system
        name: cilium
      state: Orphan
      comparison: drifted
      mismatches:
        - path: spec.template.spec.containers[name=cilium-agent].image
          reason: valueMismatch
          ownership: global
          state: Orphan
      remediation:
        action: reviewDistributionBaselineForAppliedObject
        changeOwner: globalBaseline
        allowedCommands:
          - sync diff
          - sync status
          - sync revert
          - sync render
          - sync apply
          - sync package build
          - sync package push
        commandGuidance:
          - command: sync revert
            preconditions:
              - bundleMatchesRecordedDesiredStateDigest
            availability: available
```

这份输出应该这样读：

- `headline` 是这次 compare 结果里最短、最适合复用的一层 operator 摘要。
  它的目标是稳定到可以直接放进告警标题、工单标题或 dashboard 标签里。
- `localPatchPolicy` 表示这次被检查的 rendered bundle 实际携带了哪份
  ownership policy provenance。对于还没记录这组 metadata 的 legacy bundle，
  CLI 仍然会展示当前生效的内置默认 policy 名字，但 `path` 和 `digest`
  会保持为空。
- `currentCompare` 是这次具体拿来检查的 rendered bundle 的原始 compare 结果。
- `policyEligibleOrphanObjects` 是一个顶层快捷视图，用来直接暴露
  `currentCompare` 里那些“当前仍是 `Orphan`，但已经落在默认
  `LocalPatchPolicy` 允许面内”的对象子集。
- `operatorAction` 是更紧凑的 operator 动作名。对这类子集来说，
  `promoteToLocalPatch` 的意思是：它今天仍然是 global-owned drift，
  但长期收敛路径已经可以明确落成 local repo patch。
- `persistedObservedSummary` 是因为“当前 bundle 仍然等于 recorded desired-state
  digest”，所以 Sealos 愿意写回去的摘要。
- `recordedRevision` 是 cluster 记录下来的状态对象，里面包含上次持久化的
  `observedSummary`。
- 这两处 recorded 摘要现在也会带同一组 direct-action 计数，所以它们不只
  记录“当时 drift 有多少”，也记录“当时其中有多少 drift 可以直接
  commit/revert”。
- 对象级 `remediation` 同时解释了 ownership 该回到哪里
  （这里是 `globalBaseline`）以及当前安全可用的命令。

### 例子：`sync status`

```yaml
clusterName: demo
bomName: minimal-single-node
revision: rev-poc-001
channel: alpha
bundlePath: /var/lib/sealos/demo/distribution/current
localPatchPolicy:
  source: localRepo
  scope: clusterLocal
  name: custom-local-patch-policy
  path: policy/local-patch-policy.yaml
  digest: sha256:...
desiredStateDigest: sha256:...
localRepoRevision: sha256:...
localPatchRevision: patch-rev-1
recordedState: Orphan
recordedObservedSummary:
  total: 3
  clean: 1
  dirty: 1
  orphan: 1
  mixedOwnershipObject: 1
  directCommitEligible: 1
  directRevertEligible: 2
  bundleMatchRequired: 2
currentState: Orphan
headline: state=Orphan; dirtyObjects=1; orphanObjects=2; dirtyHostPaths=1; orphanHostPaths=0; directCommitEligible=2; directRevertEligible=2; bundleMatchRequired=2; policyEligibleOrphanObjects=1
summary:
  total: 3
  clean: 1
  dirty: 1
  orphan: 1
operatorActionSummary:
  directCommitEligible: 2
  directRevertEligible: 2
  bundleMatchRequired: 2
mixedOwnershipObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: default
    name: grafana-settings
    ownerships:
      - global
      - local
dirtyObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: default
    name: grafana-settings
    operatorAction: commitOrReapplyLocalOverlay
    paths:
      - data.adminUser
    remediation:
      action: reviewLocalObjectOverlayAndCommitOrReapply
      changeOwner: localOverlay
      commandGuidance:
        - command: sync commit
          preconditions:
            - bundleMatchesRecordedDesiredStateDigest
          availability: available
orphanObjects:
  - apiVersion: apps/v1
    kind: DaemonSet
    namespace: kube-system
    name: cilium
    operatorAction: revertOrUpdateGlobalBaseline
    paths:
      - spec.template.spec.containers[name=cilium-agent].image
    remediation:
      action: reviewDistributionBaselineForAppliedObject
      changeOwner: globalBaseline
policyEligibleOrphanObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: kube-system
    name: cilium-config
    operatorAction: promoteToLocalPatch
    paths:
      - data.enable-hubble
    remediation:
      action: reviewDistributionBaselineForAppliedObject
      changeOwner: globalBaseline
      policyName: defaultLocalPatchPolicy
      policyEligiblePaths:
        - data.enable-hubble
dirtyHostPaths:
  - path: /etc/kubernetes/kubeadm.yaml
    operatorAction: commitOrReapplyLocalInput
    operatorActionMetadata:
      allowsDirectCommit: true
      allowsDirectRevert: true
      requiresBundleMatch: true
    reasons:
      - contentMismatch
    remediation:
      action: reviewLocalHostInputAndCommitOrReapply
      changeOwner: localInput
```

这份输出应该这样读：

- `headline` 是最压缩的一层 operator 视图。它足够稳定，也足够机读，
  可以直接拿去做告警标题、工单摘要或 dashboard 标签，而不必重新解析
  完整的 object / host-path 列表。
- `summary` 是当前这份 inspected bundle 的 live 汇总。
- `localPatchPolicy` 让 operator 可以直接看到：这份 rendered bundle 到底携带了
  哪个 ownership policy artifact。因为 local patch validation、compare
  阶段的 `policyEligible` 标注，以及 `sync commit` 的 overlay extraction，
  现在都统一消费 bundle 里的这份 policy，所以这层 provenance 本身也是排障面
  的一部分。
- `recordedObservedSummary` 是上一次已经写回 `AppliedRevision` 的摘要。
- 这份 recorded 摘要现在也会带同样的 direct-action 计数，所以它不只记录
  “当时 drift 有多少”，也记录“当时其中有多少 drift 可以直接
  commit/revert”。
- `mixedOwnershipObjects` 会单独指出那些同时包含 `global` 和 `local`
  fragment 的对象，即使这一次真正 drift 的只是一侧字段。
- `policyEligibleOrphanObjects` 是 `orphanObjects` 的一个更窄子集：
  它会单独指出那些目前仍然属于 `Orphan`，但变动 path 已经落在默认
  `LocalPatchPolicy` 允许面内的 package-owned object drift。
- `operatorAction` 会把这层 routing 再压成稳定的摘要动作名，比如
  `commitOrReapplyLocalOverlay`、`revertOrUpdateGlobalBaseline`、
  `promoteToLocalPatch`。同一套模式也适用于 host path 摘要，例如
  `commitOrReapplyLocalInput`、`rerenderOrUpdateGlobalBaseline`。
- `operatorActionMetadata` 则在这个动作名之上，再补一层窄而稳定的结构化信息：
  它会回答“是否支持直接 `sync commit`”、“是否支持直接 `sync revert`”，以及
  这些直接路径是否受 `bundleMatchesRecordedDesiredStateDigest` 约束。
- `operatorActionSummary` 是当前 drift 集合的顶层计数视图。它只统计主要的
  dirty/orphan object 和 host-path 列表，不会把更窄的
  `policyEligibleOrphanObjects` 子集重复算进去。
- `Observed` condition 的 message 现在也会用一句更紧凑的话带出同样的
  direct-action 计数，这样 operator 不先展开完整摘要对象，也能先看到
  commit/revert 姿态。
- `dirtyObjects`、`orphanObjects` 和 `dirtyHostPaths` 已经按 ownership
  state 分好组，所以 remediation 可以直接把你指向 `localOverlay`、
  `localInput` 或 `globalBaseline`。

一个实用的 operator 规则是：

- 需要看完整原始 compare payload 时，用 `sync diff`
- 需要把 ownership 摘要和 cluster 记录中的状态并排看时，用 `sync status`

## 当前 `operatorAction` 矩阵

这份子设计在这里真正需要固定的只有一件事：当前单节点 MVP 已经把 ownership
routing 压成一小组固定的 `operatorAction`，而这些动作名现在已经属于 CLI
输出契约的一部分。

把下面这些名字当成 `sync diff` / `sync status` 当前稳定输出的 surface 即可：

- `commitOrReapplyLocalOverlay`
- `promoteToLocalPatch`
- `revertOrUpdateGlobalBaseline`
- `commitOrReapplyLocalInput`
- `updateLocalInputAndRerender`
- `rerenderOrUpdateGlobalBaseline`
- `manualReview`

至于它们各自的含义、是否支持直接命令，以及 bundle-match 护栏的细节，当前
canonical 矩阵已经单独放在：
[sealos-sync-operator-action-reference.md](./sealos-sync-operator-action-reference.md)

对会修改 live state 或写回状态的动作，当前 CLI 仍然受前面那套 digest
护栏约束：`bundleMatchesRecordedDesiredStateDigest` 会决定对应命令 guidance
是 `available` 还是 `blocked`。

## Open Questions

- generated output 应该在哪里声明：package metadata、ownership policy，还是
  单独的 tracking manifest？
- 要表达 generated file 和 Kubernetes object 的字段级 ownership 规则，最小
  的 selector 语言应该长什么样？
- 这份细粒度 inventory 应该放进一个新的 state object 里，还是作为当前
  applied revision 旁边的一份 bundle-local 文件？
- 除了 kubeadm 的 static Pod manifest，第一版 MVP 还应该显式支持哪些
  generated output？
