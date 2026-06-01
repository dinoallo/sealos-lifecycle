# Kind: BuildClass

## 状态

提议中的文件 schema。当前实现已经在 `BOM` entry 中记录 `build.class`，但独立的 `BuildClass` 文档还未实现。

## 类别

源仓库文档。

## 维护方

distribution 平台 owner 维护 build class。包 owner 可以选择 build class，但不应在包内重新定义 class 的语义。

## 常见位置

- `build-classes/<name>.yaml`
- `build/classes/<name>.yaml`

## 用途

`BuildClass` 定义 package source 的可复现构建契约。它让源优先本地构建模式和非本地构建模式共享同一个包模型：

- 源优先模式使用 build class 从仓库事实构建 artifact。
- 非本地模式消费已经构建好的 artifact，但仍通过同一个 class 名称保留 provenance。

Class 是包元数据和构建执行之间的边界。包声明自己包含什么，build class 声明这种 source shape 如何构建。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs-image
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `driver` | 是 | 逻辑构建 driver，例如 `containerfile`、`script`、`helm` 或 `copy-rootfs`。 |
| `output` | 是 | 构建产物类型，例如 `ociImage`、`filesystem`、`chart` 或 `manifestBundle`。 |
| `packageClasses` | 是 | 该 build class 接受的 `ComponentPackage.spec.class` 值。 |
| `platforms` | 否 | 支持的目标平台。为空表示平台无关。 |
| `source` | 否 | source 文件 include/exclude 规则。 |
| `parameters` | 否 | 已声明的非 secret 构建参数和默认值。 |
| `provenance` | 否 | artifact metadata 中必须写入的 provenance 字段。 |

## 校验规则

- `metadata.name` 在 distribution 源仓库内必须全局唯一。
- 必须设置 `driver` 和 `output`。
- `packageClasses` 中的每个值都必须是支持的 `ComponentPackage` class。
- `parameters` 必须是非 secret。Secret 只能由运行时或 CI 环境提供，并且只能通过名称引用。
- Build class 被采用后应保持不可变。需要改变 class 行为时，应创建新的 class 名称或版本化 class 名称。

## 构建输入

包构建流程在调用 build class 前会解析这些值：

- package identity：`category`、`name`、`version`
- source path 和 digest
- `build.class`
- 目标平台
- build profile
- 已声明的 build options

Build class 不能读取未声明的 host path、cluster 配置、运行时状态或 secret 内容。

## 本地与非本地模式共存

`BuildClass` 是让两种构建模式更容易维护的共享抽象：

- 在源优先本地构建模式中，controller 或本地构建工具针对 source 文件执行 class，并记录生成的 artifact。
- 在非本地构建模式中，artifact 已经可用，但 class 仍保留在 `BOM` provenance 中，用于说明 artifact 是如何生成的。

这样可以避免为本地和非本地 delivery 维护两套 package schema。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs-image
spec:
  driver: containerfile
  output: ociImage
  packageClasses:
    - rootfs
  platforms:
    - linux/amd64
    - linux/arm64
  source:
    include:
      - rootfs/**
      - Containerfile
      - hooks/**
    exclude:
      - "**/.git/**"
      - "**/*.tmp"
  parameters:
    - name: baseImage
      required: true
      secret: false
    - name: compression
      default: zstd
      secret: false
  provenance:
    required:
      - sourceDigest
      - buildClass
      - buildProfile
      - platform
```

## Kubernetes 包示例

Kubernetes 的 `ComponentPackage` 通常会选择 rootfs 类型的 build class：

```yaml
packages:
  - category: core
    name: kubernetes
    version: v1.31.1
    source:
      path: packages/core/kubernetes/v1.31.1
      digest: sha256:...
    build:
      class: rootfs-image
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
- `BuildClass` 不批准本地 patches。
- `BuildClass` 不表示已应用的运行时状态。

## 相关 Kind

- `ComponentPackage` 提供 build class 消费的 source shape。
- `BOM` 为每个 package entry 选择 class。
- `PackageAcceptanceReport` 记录构建产物是否通过校验。
