# Kind: ComponentPackage

## 状态

已实现的文件 schema，并包含用于源优先 materialization 的 package-local build 扩展规划。

## 类别

源侧包文档。

## 维护方

组件包 owner 在 distribution 源仓库中维护该文档。

## 常见位置

- `packages/<category>/<name>/package.yaml`
- `packages/<category>/<name>/<version>/package.yaml`

## 用途

`ComponentPackage` 描述一个组件版本对应的可构建、可安装单元。它是源仓库里的包级契约，用来定义包内容、hooks、inputs、依赖和本地 patch 归属。

该文档既要支持源优先的本地构建，也要支持非本地构建模式，也就是同一个包可以已经被发布成外部 artifact。

## 必需信封

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: kubernetes
spec: {}
```

`metadata.name` 是仓库内的包名。包重名问题不只靠这个字段解决，而是在 `BOM` 里结合 `category`、`name`、`version` 和 source provenance 一起形成包身份。

## Source 与 Materialized 形态

`ComponentPackage` 会以两种形态出现：

- Source form：存储在 distribution 源仓库下的 `package.yaml`。
- Materialized form：存储在构建后 package payload 或 artifact root 下的 `package.yaml`。

两种形态使用同一个 kind，并且必须通过同一个 schema 校验。Source form 声明 package source contract。Materialized form 声明 downstream loader、hydration 和 apply workflow 消费的 payload。

Build workflow 不能输出另一个最终 document kind。它必须生成一个 package root，其中包含有效的 `ComponentPackage` manifest，以及该 manifest 引用的文件。

当 package 被 `BOM` 选择时，必须满足：

```text
BOM package.name == ComponentPackage.spec.component
BOM package.version == ComponentPackage.spec.version
```

## Spec 契约

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `component` | 是 | 逻辑组件名，通常与 `metadata.name` 一致。 |
| `version` | 是 | 该包表示的组件版本。 |
| `class` | 是 | 包类别，当前值为 `rootfs`、`patch`、`application`。 |
| `dependencies` | 否 | 在安装或渲染前必须可用的包名。 |
| `compatibility` | 否 | Kubernetes、OS、架构或发行线兼容性规则。 |
| `build` | 否 | 用于源优先 materialization 的 package-local build contract。 |
| `inputs` | 否 | 包声明接受的非 secret 输入。 |
| `contents` | 是 | 包内容，例如 rootfs、manifest、chart、values、patch 或 hook。 |
| `hooks` | 否 | 包生命周期 hook。 |
| `localPatchPolicy` | 否 | 指向该包本地 patch 归属策略的相对路径。 |

## Build

`spec.build` 描述应放在 package source 附近的 package-specific build
requirements。对于可以直接通过所选 `BuildClass` 默认行为 materialize 的 package，该字段可省略。

支持字段：

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| `class` | 否 | 该 package source 期望的默认 build class。 |
| `inputs` | 否 | materialization 前需要准备好的非 secret build assets。 |
| `staging` | 否 | 从 build inputs 到 materialized package root 路径的映射。 |
| `script.path` | 否 | package-local adapter script，通常位于 `build/` 下。 |

Build inputs 是 package build assets，不是集群特定 inputs。它们可以引用 local mirror
entry、artifact cache entry、upstream asset reference，或已经存放在 package source
里的小文件。任何会影响 package bytes 的外部 asset 都必须按 digest pin。

`staging.path` 必须相对于 materialized package root，且不能逃逸该 root。`script.path`
必须相对于 package source 目录。仓库级 scripts 可以调用这个契约，但 package-specific
staging 规则不应只存在于仓库级 scripts 中。

## Contents

支持的 content 类型：

- `rootfs`
- `manifest`
- `chart`
- `patch`
- `file`
- `values`
- `hook`

每个 content entry 必须有稳定的名称和仓库相对路径。路径不能逃逸包目录或仓库根目录。

当前可执行语义比 schema 更窄：

- `manifest` 是当前 apply 路径消费的 raw Kubernetes YAML content type。
- `chart` 可以被 schema 接受并在 render 中复制，但当前 apply executor 不会运行
  Helm，也不会渲染 chart template。
- `values` 是 render 支撑数据，不会被直接 apply。
- `hook` content 只有被 `spec.hooks[]` 引用时才会执行。

## Inputs

支持的 input 类型：

- `configFile`
- `valuesFile`
- `env`

Inputs 只声明包能接受哪些值，本身不提供集群特定取值。集群特定取值应放在 `ComponentInput` 或 cluster 配置仓库中。

## Hooks

支持的 hook 阶段：

- `bootstrap`
- `configure`
- `install`
- `upgrade`
- `remove`
- `healthcheck`

支持的 hook 目标：

- `allNodes`
- `firstMaster`
- `cluster`

Hook 必须只依赖已声明输入和包内文件，不能读取未声明的 host 文件或 secret。

当前不支持 inline hook command。Hook 行为必须放在包内引用文件中，这样 review、
digest、可执行位检查、timeout 和审计工具才能检查一个稳定 payload。

## 校验规则

- 必须设置 `apiVersion`、`kind` 和 `metadata.name`。
- 必须设置 `spec.component` 和 `spec.version`。
- `spec.class` 必须属于支持的包类别。
- `spec.build.class` 如果存在，必须与 BOM 选择的 build class 兼容。
- `spec.build.inputs` 必须是非 secret；如果解析到 package source 之外，必须按 digest pin。
- `spec.build.staging` entry 必须引用已声明的 build input，并使用相对路径。
- `spec.build.script.path` 如果存在，必须指向 package source 目录内的文件。
- 至少需要一个 content entry。
- 同一个包内 content 名称必须唯一。
- 同一个包内 input 名称必须唯一。
- 同一个包内 hook 名称必须唯一。
- `localPatchPolicy` 如果存在，必须是相对路径。
- `dependencies[].version` 如果存在，是 BOM 已选择的精确版本。版本范围属于 BOM
  或 release-channel selection policy，不属于 package manifest。

## 生命周期

1. 包 owner 声明包文件、inputs 和 hooks。
2. `BOM` 通过 source path、digest、build class 和 artifact 输出引用该包。
3. 源优先本地构建模式从仓库事实构建包。
4. 非本地构建模式消费 `BOM` 引用的 artifact。
5. Hydration 在 `HydratedBundle` provenance 中记录包输出。

## 边界

- `ComponentPackage` 不选择集群。
- `ComponentPackage` 不携带 secrets。
- `ComponentPackage` 不表示运行时状态。
- `ComponentPackage` 不批准 ownership 变更。

## 示例

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: kubernetes
spec:
  component: kubernetes
  version: v1.31.1
  class: rootfs
  build:
    class: rootfs/v1
    inputs:
      - name: kubeadm
        type: file
        sourceRef: kubernetes-release:v1.31.1/bin/linux/amd64/kubeadm
        digest: sha256:...
        required: true
    staging:
      - input: kubeadm
        path: rootfs/usr/bin/kubeadm
        mode: "0755"
    script:
      path: build/package-build.sh
  inputs:
    - name: cluster-network
      type: valuesFile
      path: inputs/network.values.yaml
  contents:
    - name: kube-binaries
      type: rootfs
      path: rootfs/
    - name: kubeadm-config
      type: manifest
      path: manifests/kubeadm.yaml
  hooks:
    - name: kubeadm-init
      phase: install
      target: firstMaster
      path: hooks/kubeadm-init.sh
  localPatchPolicy: ownership/local-patch-policy.yaml
```

## 相关 Kind

- `BuildClass` 定义 package source 如何构建。
- `BOM` 选择包版本和 artifact。
- `ComponentInput` 提供集群特定的非 secret 取值。
- `LocalPatchPolicy` 定义本地 ownership 边界。
- `HydratedBundle` 记录渲染后的包输出。
