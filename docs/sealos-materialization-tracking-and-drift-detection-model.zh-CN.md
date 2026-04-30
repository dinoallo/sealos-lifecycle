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
- 哪些 live object 只是被观察，哪些才是真正的 baseline-owned desired
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
| `observeOnly` | operator 运行时生成的连接 Secret 之类对象 | 不把这个对象本身当成 baseline-owned desired state。 |

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

所以像 `/etc/kubernetes/manifests/kube-apiserver.yaml` 这样的文件，就应该被
当成一个 `generatedHostPath` projection。

对于这种 generated host file，Sealos 至少应该追踪：

- `kubernetes-rootfs` package revision
- bootstrap hook identity
- hydrate 后 `kubeadm.yaml` 的 render digest
- 提供 cluster-specific 值的 local repo revision
- 生成目标路径
- 这份 static Pod manifest 在上次成功 apply 时的 normalized digest

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

## Open Questions

- generated output 应该在哪里声明：package metadata、ownership policy，还是
  单独的 tracking manifest？
- 要表达 generated file 和 Kubernetes object 的字段级 ownership 规则，最小
  的 selector 语言应该长什么样？
- 这份细粒度 inventory 应该放进一个新的 state object 里，还是作为当前
  applied revision 旁边的一份 bundle-local 文件？
- 除了 kubeadm 的 static Pod manifest，第一版 MVP 还应该显式支持哪些
  generated output？
