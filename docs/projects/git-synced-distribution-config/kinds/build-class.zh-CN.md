# Kind: BuildClass

## 状态

提议中的契约。当前实现已经在 `BOM` entry 中记录 `build.class`，但 Sealos 还没有实现 class
registry 或独立的 repo-local `BuildClass` 文档加载。

## 类别

Built-in contract。

## 维护方

Sealos distribution implementation 维护标准 build class。Distribution 平台 owner 审批哪些
class 可用，也可以维护 custom 或 policy-pinned class 的 repo-local descriptor。包 owner
可以选择 build class，但不应在包内重新定义 class 的语义。

## 解析位置

Build class 按以下顺序解析：

1. Sealos built-in class registry。
2. 随 Sealos 安装的已批准 extension class implementation。
3. 可选的 repo-local `classes/<name>/<version>.yaml` descriptor，用于 custom、
   experimental 或 policy-pinned class，但必须由已安装 implementation backing。

`rootfs/v1`、`manifest-bundle/v1`、`helm-render/v1` 和 `patch-overlay/v1` 等 built-in
class 不需要保存在每个 distribution 仓库中。Repo-local descriptor 可以记录或约束 built-in
class 的使用，但不能替代 Sealos 随 binary 提供的 implementation。

## 用途

`BuildClass` 定义 package source 的可复现构建契约。它让源优先本地构建模式和非本地构建模式共享同一个包模型：

- 源优先模式使用 build class 从仓库事实构建 artifact。
- 非本地模式消费已经构建好的 artifact，但仍通过同一个 class 名称保留 provenance。

Class 是包元数据和构建执行之间的边界。包声明自己包含什么，build class implementation
声明这种 source shape 如何构建。

Package-specific build details 仍然属于 `ComponentPackage.spec.build`。例如，需要 stage
到 `rootfs/usr/bin/` 的 Kubernetes binaries 清单是 package source metadata，不是可复用的
class 语义。

## 可选 Descriptor Envelope

Built-in class 只通过 identity 引用，不要求这个文件。仓库如果为 custom、experimental 或
policy-pinned class 携带 descriptor，应使用这个 envelope：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs
spec:
  version: v1
```

Canonical build class identity 是：

```text
<metadata.name>/<spec.version>
```

例如，上面的 descriptor 在 `ComponentPackage.spec.build.class` 或
`BOM.spec.packages[*].build.class` 中被引用为 `rootfs/v1`。

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `version` | 是 | Build class contract version，例如 `v1`。 |
| `driver` | 是 | 逻辑构建 driver，例如 `copy-rootfs`、`copy-manifest`、`helm` 或 `patch`。 |
| `output` | 是 | 构建产物类型，例如 `ociImage`、`filesystem`、`chart` 或 `manifestBundle`。 |
| `packageClasses` | 是 | 该 build class 接受的 `ComponentPackage.spec.class` 值。 |
| `platforms` | 否 | 支持的目标平台。为空表示平台无关。 |
| `source` | 否 | source 文件 include/exclude 规则。 |
| `parameters` | 否 | 已声明的非 secret 构建参数和默认值。 |
| `provenance` | 否 | artifact metadata 中必须写入的 provenance 字段。 |

## 校验规则

- Class identity `metadata.name/spec.version` 必须解析到唯一已批准 implementation：Sealos
  built-in class、已批准 extension，或指向已安装 custom extension 的 repo-local descriptor。
- 未知 class 应 fail closed。
- 必须设置 `metadata.name` 和 `spec.version`。
- 必须设置 `driver` 和 `output`。
- `packageClasses` 中的每个值都必须是支持的 `ComponentPackage` class。
- `parameters` 必须是非 secret。Secret 只能由运行时或 CI 环境提供，并且只能通过名称引用。
- Build class 被采用后应保持不可变。需要改变 class 行为时，应创建新的 class 名称或版本化 class 名称。
- Repo-local descriptor 不应开启任意 package-specific 行为，除非该行为由已批准 extension
  implementation 表示。

## 构建输入

包构建流程在调用 build class 前会解析这些值：

- package identity：`category`、`name`、`version`
- source path 和 digest
- `build.class`
- 目标平台
- build profile
- 已声明的 build options
- 来自 `ComponentPackage.spec.build` 的 package-local build inputs 和 staging rules

Build class implementation 不能读取未声明的 host path、cluster 配置、运行时状态或 secret 内容。

在调用 class 前，构建流程应从被选择的 `ComponentPackage` 加载 package-local contract。
Class 提供 driver 语义；package-local contract 提供 per-package build facts。

## 构建输出

被 `BOM.spec.packages[*].build.class` 选中的 build class 必须生成 materialized package payload，而不是新的 document kind。Payload 可以存储为 OCI image、filesystem directory、OCI layout 或其他支持的 transport，但它的 root 必须能作为 package root 加载。

Materialized package root 必须包含：

- `package.yaml`
- `package.yaml` 引用的每个 content path
- `package.yaml` 引用的每个 hook path
- `package.yaml` 引用的可选 local patch policy files

输出中的 `package.yaml` 必须能通过 `ComponentPackage` 校验。Build 可以规范化路径或补充 build provenance，但必须保留 BOM 选择的 component 和 version：

```text
BOM package.name == output ComponentPackage.spec.component
BOM package.version == output ComponentPackage.spec.version
```

这个要求让源优先本地构建和非本地 artifact 消费可以共享同一套 downstream loader 和 hydration workflow。

## 本地与非本地模式共存

`BuildClass` 是让两种构建模式更容易维护的共享抽象：

- 在源优先本地构建模式中，controller 或本地构建工具针对 source 文件执行 class，并记录生成的 artifact。
- 在非本地构建模式中，artifact 已经可用，但 class 仍保留在 `BOM` provenance 中，用于说明 artifact 是如何生成的。

这样可以避免为本地和非本地 delivery 维护两套 package schema。

仓库级 `scripts/` 可以为了 operator 便利包装 build class，但它们应保持为通用 dispatcher。
它们不应该携带每个 distribution 仓库都必须复制的可复用 class implementation。只有当
package-specific script 被 package-local build contract 引用，并且位于 package source 目录内时，
才应使用它。

## Descriptor 示例

下面的 descriptor shape 用来记录契约。对于 `rootfs/v1` 这样的 built-in class，可执行行为仍然来自
Sealos class registry。

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs
spec:
  version: v1
  driver: copy-rootfs
  output: ociImage
  packageClasses:
    - rootfs
  platforms:
    - linux/amd64
    - linux/arm64
  source:
    include:
      - package.yaml
      - rootfs/**
      - files/**
      - hooks/**
    exclude:
      - "**/.git/**"
      - "**/*.tmp"
  provenance:
    required:
      - sourceDigest
      - buildClass
      - platform
```

## 初始 Built-in Build Classes

初始 built-in class 集合应保持小。只有当 source shape 或 output semantics 差异足够大，需要独立
validation 和 provenance contract 时，才新增 class。

| Class | Driver | Output | Package classes | 适用场景 |
| --- | --- | --- | --- | --- |
| `rootfs/v1` | `copy-rootfs` | `ociImage` | `rootfs` | 将文件 materialize 到 `rootfs/` 下的 package，可先 stage 已声明 build inputs。Kubernetes 和 containerd rootfs package 属于这一类。 |
| `manifest-bundle/v1` | `copy-manifest` | `ociImage` | `application`、`policy` | 直接把已提交的 manifests、values 和 hooks 复制进 package artifact，不渲染 chart。当前 Cilium package 形态属于这一类。 |
| `helm-render/v1` | `helm` | `ociImage` | `application` | Source 包含 Helm chart 或 chart reference 以及 values，build output 是渲染后的 manifest bundle。 |
| `patch-overlay/v1` | `patch` | `ociImage` | `patch` | 对 upstream package 或 manifest bundle 应用声明式 overlays/patches，并发布结果 package payload。 |

初始默认集合里不建议放通用 `script/v1` class。Package-local scripts 可以作为
`ComponentPackage.spec.build` 中声明的 adapter 使用，但 catch-all script class 会让 build
behavior 更难校验和复现。Custom script-like behavior 应通过已批准 extension class 引入，
而不是把任意可复用 scripts 放进每个 distribution 仓库。

### `rootfs/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs
spec:
  version: v1
  driver: copy-rootfs
  output: ociImage
  packageClasses:
    - rootfs
  source:
    include:
      - package.yaml
      - rootfs/**
      - files/**
      - manifests/**
      - hooks/**
      - build/**
    exclude:
      - "**/.git/**"
      - "**/tmp/**"
  provenance:
    required:
      - sourceDigest
      - buildClass
      - platform
```

### `manifest-bundle/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: manifest-bundle
spec:
  version: v1
  driver: copy-manifest
  output: ociImage
  packageClasses:
    - application
    - policy
  source:
    include:
      - package.yaml
      - manifests/**
      - files/**
      - hooks/**
    exclude:
      - "**/.git/**"
      - "**/tmp/**"
  provenance:
    required:
      - sourceDigest
      - buildClass
```

### `helm-render/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: helm-render
spec:
  version: v1
  driver: helm
  output: ociImage
  packageClasses:
    - application
  parameters:
    - name: chart
      required: true
      secret: false
    - name: values
      required: false
      secret: false
  provenance:
    required:
      - sourceDigest
      - buildClass
      - chartDigest
```

### `patch-overlay/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: patch-overlay
spec:
  version: v1
  driver: patch
  output: ociImage
  packageClasses:
    - patch
  source:
    include:
      - package.yaml
      - patches/**
      - files/**
      - hooks/**
  provenance:
    required:
      - sourceDigest
      - buildClass
      - baseArtifactDigest
```

## Kubernetes 包示例

Kubernetes 的 `ComponentPackage` 通常会选择 rootfs 类型的 build class。Package-specific
binary inputs 和 staging rules 属于 `ComponentPackage.spec.build`；BOM 只为一个 release pin
class 和 artifact：

```yaml
packages:
  - category: core
    name: kubernetes
    version: v1.31.1
    source:
      path: packages/core/kubernetes/v1.31.1
      digest: sha256:...
    build:
      class: rootfs/v1
      profile: release
      platform: linux/amd64
    artifact:
      name: kubernetes-rootfs
      image: registry.example.com/dist/kubernetes:v1.31.1
      digest: sha256:...
```

## 边界

- `BuildClass` 不选择包版本。
- `BuildClass` 不定义集群特定 inputs。
- `BuildClass` 不列出 package-specific external assets。
- `BuildClass` 不把 package-specific staging rules 隐藏在仓库级 scripts 中。
- `BuildClass` 不要求每个 distribution 仓库 vendor 标准 class 定义。
- `BuildClass` 不批准本地 patches。
- `BuildClass` 不表示已应用的运行时状态。

## 相关 Kind

- `ComponentPackage` 提供 build class 消费的 source shape。
- `BOM` 为每个 package entry 选择 class。
- `PackageAcceptanceReport` 记录构建产物是否通过校验。
