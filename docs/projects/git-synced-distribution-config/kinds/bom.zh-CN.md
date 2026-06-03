# Kind: BOM

## 状态

已实现的文件 schema，并计划扩展源优先本地构建能力。

## 类别

发布源文档。

## 维护方

distribution release owner 维护 `BOM` 文档。

## 常见位置

- `boms/<distribution>/<revision>/bom.yaml`
- `releases/<distribution>/<revision>/bom.yaml`

## 用途

`BOM` 是 release 的物料清单。它为一个 distribution revision 选择精确的 package set，并绑定包身份、source provenance、build metadata、artifact metadata、依赖和本地 patch 策略。

它是让源优先本地构建模式和非本地构建模式共存的核心文档：

- 源优先模式使用 `source` 和 `build` 在本地生成 artifact。
- 非本地模式使用 `artifact` 消费已发布的输出。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: sealos-v5.0.0
spec: {}
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `revision` | 是 | 该 BOM 表示的不可变 distribution revision。 |
| `localPatchPolicy` | 否 | 该 release 默认本地 patch policy 的相对路径。 |
| `baseArtifacts` | 否 | package build 或 runtime assembly 共享的 base artifacts。 |
| `packages` | 是 | 该 revision 选择的 package entries。 |

## Package 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `category` | 推荐 | 用于避免重名并区分包类型的 package category。 |
| `name` | 是 | category 内的包名。 |
| `version` | 是 | 包版本。 |
| `source.path` | 源优先模式需要 | 仓库相对 source path。 |
| `source.digest` | 可复现源优先模式需要 | 用于 build 或 render 的 source facts digest。 |
| `build.class` | 源优先模式需要 | 构建该 source 使用的 build class。 |
| `build.profile` | 否 | 构建 profile，例如 `debug`、`release` 或 `fips`。 |
| `artifact.name` | 当前实现需要 | 逻辑 artifact 名称。 |
| `artifact.image` | 当前实现需要 | 已发布的 OCI image 或 artifact reference。 |
| `artifact.digest` | 当前实现需要 | 已发布 artifact 的 digest。 |
| `artifact.platform` | 否 | 平台相关 artifact 的目标平台。 |
| `dependencies` | 否 | 当前 package 依赖的其他 package 名称。 |
| `required` | 否 | 该 package 是否为 release 必需。 |

当前实现会校验每个 package 都有 artifact metadata。源优先提案保留 `artifact` 作为非本地路径，同时为本地构建增加更强的 `source` 和 `build` 要求。实现源优先本地构建时，需要明确什么时候 `artifact` 可以省略或由构建流程生成。

## Package 解析关系

BOM package 会通过两条等价的 materialization 路径解析到一个 `ComponentPackage`：

- 源优先本地构建模式加载 `source.path/package.yaml`，校验它是 `ComponentPackage`，加载 `ComponentPackage.spec.build` 中的 package-local build contract，执行 `build.class`，并生成 materialized package root 或 artifact。
- 非本地构建模式从 `artifact.image@artifact.digest` 加载已经 materialized 的 package artifact。

两条路径都必须产出一个可加载的 package root，并且其中必须包含 `package.yaml`。这个 `package.yaml` 必须能通过 `ComponentPackage` 校验。

解析时必须强制：

```text
BOM package.name == ComponentPackage.spec.component
BOM package.version == ComponentPackage.spec.version
```

`artifact.name` 是逻辑 artifact 名称，用于可读性、报告、artifact index 和 base artifact 去重。它不是 package identity，也不能作为 `BOM` 和 `ComponentPackage` 之间的绑定键。

当 `source` 和 `artifact` 同时存在时，artifact 是 pinned source 和 build contract 的 materialized 结果，不是独立事实源。

BOM 不应重复 package-specific build recipes。它 pin 的是 release selection：package identity、
source path 和 digest、被选择的 build class、profile 或 platform options，以及可选 artifact
digest。Package-specific asset declarations、staging rules 和 adapter scripts 仍然保留在
`source.path` 下的 `ComponentPackage.spec.build` 中。

## 包身份

包身份应按以下维度解析：

```text
category + name + version + source digest
```

仅靠 `name` 不够。不同 category 可以合法复用短名称，同一个 version 也可能由不同 source facts 重建。

## 校验规则

- 必须设置 `apiVersion`、`kind` 和 `metadata.name`。
- 必须设置 `spec.revision`。
- 至少需要一个 package entry。
- 在实现仍按 name 解析依赖时，package name 必须唯一。
- 解析 category、name、version 和 source provenance 后，package identity 不能冲突。
- Source path 必须是相对路径，且不能逃逸仓库根目录。
- Digest 如果存在，必须使用支持的 digest 格式。
- Dependencies 必须引用同一个 BOM 中的 package。

## 生命周期

1. 包 owner 发布或更新 `ComponentPackage`。
2. Release owner 将包版本选择进 `BOM`。
3. 源优先模式用 `source` 加 `build` 构建缺失 artifact。
4. 非本地模式校验并消费 `artifact`。
5. `ReleaseChannel` 在健康证据通过后晋级 BOM revision。
6. 运行时 reconcile 记录已应用 revision。

## 边界

- `BOM` 不选择集群。
- `BOM` 不包含集群特定 input values。
- `BOM` 不包含 secrets。
- `BOM` 本身不表示运行时成功。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: sealos-v5.0.0
spec:
  revision: v5.0.0
  localPatchPolicy: ownership/local-patch-policy.yaml
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
      required: true
```

## 相关 Kind

- `ComponentPackage` 定义 package source metadata。
- `BuildClass` 定义 source build 行为。
- `ReleaseChannel` 指向目标 BOM revision。
- `HydratedBundle` 记录 BOM 的渲染输出。
- `AppliedRevision` 记录已应用到集群的 revision。
