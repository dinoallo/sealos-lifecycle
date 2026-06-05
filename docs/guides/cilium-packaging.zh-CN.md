# 操作指引：把 Cilium 打包成 Sealos 组件包

## 状态

基于当前仓库实现

## 概述

这份指引展示当前仓库里 Cilium 是如何接入 package-based distribution
流程的。

它描述的是今天已经存在的实现，而不是未来设想：

- Cilium 目前被打成一个 `application` 类型的组件包
- 包内容以 manifest 为主，不是 rootfs 包
- 通过 `sealos sync package build` 构建成 OCI 镜像
- 再通过 BOM 引用这个 OCI 包

## 相关文件

- 包目录：
  [scripts/poc/minimal-single-node/packages/cilium](../../scripts/poc/minimal-single-node/packages/cilium)
- 包清单：
  [scripts/poc/minimal-single-node/packages/cilium/package.yaml](../../scripts/poc/minimal-single-node/packages/cilium/package.yaml)
- 默认 values：
  [scripts/poc/minimal-single-node/packages/cilium/files/values/basic.yaml](../../scripts/poc/minimal-single-node/packages/cilium/files/values/basic.yaml)
- Cilium manifest：
  [scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml](../../scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml)
- 健康检查 hook：
  [scripts/poc/minimal-single-node/packages/cilium/hooks/healthcheck.sh](../../scripts/poc/minimal-single-node/packages/cilium/hooks/healthcheck.sh)
- OCI packaging CLI：
  [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go)
- 开发与 release-build helper：
  [scripts/poc/minimal-single-node/fetch-assets.sh](../../scripts/poc/minimal-single-node/fetch-assets.sh)
  [scripts/poc/minimal-single-node/stage-assets.sh](../../scripts/poc/minimal-single-node/stage-assets.sh)
  [scripts/poc/minimal-single-node/publish-oci.sh](../../scripts/poc/minimal-single-node/publish-oci.sh)

这些 helper 只用于 package authoring、fixture 生成和 release-build 自动化。
它们不是 Day 0 operator 安装入口；scriptless 安装路径见
[Day 0 install](./day-0-install.zh-CN.md)。

## 当前 Cilium 包里有什么

当前包目录很小，偏 manifest-oriented：

```text
scripts/poc/minimal-single-node/packages/cilium/
  package.yaml
  files/values/basic.yaml
  manifests/cilium.yaml
  hooks/healthcheck.sh
```

关键点：

- `spec.class: application`
- 依赖 `kubernetes`
- 暴露了一个 input surface：`cilium-values`
- 主 payload 是 `manifests/cilium.yaml`
- 还带了一份默认 values 文件
- 只有一个 `healthcheck` hook

当前这个包没有：

- Helm chart
- rootfs payload
- bootstrap hook

## Step 1：从包目录开始

当前仓库里的包目录就是：

```bash
scripts/poc/minimal-single-node/packages/cilium
```

`package.yaml` 的高层结构可以理解成：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: cilium-cni
spec:
  component: cilium
  version: v1.15.0
  class: application
  dependencies:
    - name: kubernetes
      version: v1.30.3
  inputs:
    - name: cilium-values
      type: valuesFile
      path: files/values/basic.yaml
  contents:
    - name: cilium-manifests
      type: manifest
      path: manifests/cilium.yaml
    - name: cilium-values
      type: values
      path: files/values/basic.yaml
  hooks:
    - name: healthcheck
      phase: healthcheck
      target: cluster
      path: hooks/healthcheck.sh
```

这意味着：

- Cilium 作为集群工作负载内容被下发
- Kubernetes 必须先存在
- 这个包里有一份 manifest payload 和一份默认 values payload
- 同时暴露了一个 values 输入面，供 cluster-local 数据绑定

## Step 2：准备或刷新包资产

当前仓库支持两种方式准备 Cilium 包内容。

### 方式 A：直接使用仓库里已跟踪的 manifest

PoC 目录下已经跟踪了：

- `manifests/cilium.yaml`
- `files/values/basic.yaml`

这已经足够支撑今天的 inspect、build、push 和 render 流程。

### 方式 B：重新生成 manifest

package author 可以用开发 helper 重新生成 Cilium manifest：

```bash
scripts/poc/minimal-single-node/fetch-assets.sh
```

它当前做的事情是：

1. 下载 `cilium` CLI
2. 读取 `packages/cilium/files/values/basic.yaml`
3. 执行 `cilium install --dry-run --version v1.15.0 -f <values-file>`
4. 输出生成后的 `cilium.yaml`

如果要把新生成的 manifest 回填到包目录里，可以用：

```bash
install -D -m 0644 /path/to/cilium.yaml \
  scripts/poc/minimal-single-node/packages/cilium/manifests/cilium.yaml
```

如果要用仓库自带 helper，需要注意
`scripts/poc/minimal-single-node/stage-assets.sh` 不是一个只处理 Cilium 的脚本，
而是面向整个三组件 PoC 的 asset stager。它会先校验 containerd 和
Kubernetes 的二进制输入，再一起处理 Cilium 资产。普通 Day 0 安装不应该运行
`fetch-assets.sh` 或 `stage-assets.sh`，而应该消费已经发布并带 digest pin 的 BOM
或 `ReleaseChannel`。

## Step 3：先 inspect 包元数据

在 build 之前，先 inspect：

```bash
sealos sync package inspect \
  --package-dir scripts/poc/minimal-single-node/packages/cilium
```

这个命令会读取 `package.yaml`，确认当前包的关键元数据，例如：

- package name: `cilium-cni`
- component: `cilium`
- version: `v1.15.0`
- class: `application`

这是进入 OCI 流程前最快的结构性检查。

## Step 4：构建 OCI 包镜像

用一等公民 CLI 直接构建：

```bash
sealos sync package build \
  --package-dir scripts/poc/minimal-single-node/packages/cilium \
  --image localhost:5000/poc-minimal/cilium-cni:v1.15.0 \
  --platform linux/amd64 \
  --timestamp 0 \
  --distribution poc-minimal
```

当前 build 流程会：

1. 从 `package.yaml` 读取元数据
2. 把包目录整理成一个确定性的 build context
3. 生成一个最小 `Containerfile`
4. 基于 `scratch` 构建 OCI 镜像
5. 附加组件包相关的 OCI labels

关键 labels 包括：

- `sealos.io.type=application`
- `sealos.io.version=v1.15.0`
- `distribution.sealos.io/kind=ComponentPackage`
- `distribution.sealos.io/package-name=cilium-cni`
- `distribution.sealos.io/component=cilium`

## Step 5：推送 OCI 包镜像

构建完成后再推送：

```bash
sealos sync package push \
  --image localhost:5000/poc-minimal/cilium-cni:v1.15.0 \
  --destination localhost:5000/poc-minimal/cilium-cni:v1.15.0 \
  --provenance-file /tmp/cilium-cni.provenance.yaml
```

推送命令会先把返回值校验为 OCI digest，再报告推送成功。可选的
provenance 文件会记录目标 transport、digest algorithm、encoded digest 值，
以及 registry auth 输入是否被配置；`--creds` 的具体值只会传给底层 push，
不会写进命令输出、provenance 或失败诊断。

推送结果里最重要的是 digest。签名仍然交给外部 registry/image policy 处理；
这个 PoC 流程通过 image 加 digest 固定 BOM 里的包选择。BOM 最终要记录的是：

- image
- digest

如果目标是 mirror 或离线 registry，就把 `--destination` 指向 mirror registry，
并在 BOM 里记录这个 mirror image 加 digest。render 阶段的 OCI 解析仍然使用
digest-derived 的 pull-if-missing cache；cache GC、预热和 registry outage
runbook 属于后续运维任务，不属于发布命令本身。

PoC 中用于三组件整体发布的 release-build fixture 是：

```bash
scripts/poc/minimal-single-node/publish-oci.sh \
  --registry-prefix localhost:5000/poc-minimal
```

它内部会顺序执行：

1. stage package assets
2. `sealos sync package inspect`
3. `sealos sync package build`
4. `sealos sync package push`
5. 写出 OCI-backed BOM

这个脚本适合 CI 或 package author 临时制造本地 release source 做测试；它不是
集群安装 wrapper。

## Step 6：在 BOM 里引用 Cilium

推送完成后，BOM 里的 Cilium 条目应类似：

```yaml
- name: cilium
  kind: infra
  version: v1.15.0
  dependencies:
    - kubernetes
  artifact:
    name: cilium-cni
    image: localhost:5000/poc-minimal/cilium-cni:v1.15.0
    digest: sha256:<pushed-digest>
```

关键在于：必须同时写 `image` 和 `digest`，这样包选择才是确定性的。

## Step 7：渲染 bundle

当前 PoC 有两种常见的开发渲染方式。

### 从本地包目录渲染

```bash
scripts/poc/minimal-single-node/render.sh --package-mode local
```

这个模式会给 `containerd`、`kubernetes`、`cilium` 都传
`--package-source`，适合开发时直接改包目录。

### 从 OCI 包渲染

先发布 OCI 包并生成 OCI-backed BOM，然后：

```bash
scripts/poc/minimal-single-node/render.sh --package-mode oci
```

这个模式下，render 会直接从 BOM 里的 artifact reference 解析 Cilium。

面向 operator 的 0-to-1 PoC 使用 Day 0 guide 里的流程：选择 release source，
执行 `sealos sync local-repo init`，校验 source，执行 `sealos sync render`，
然后 apply 渲染出的 bundle。

当前渲染结果里，Cilium 相关文件会落到类似路径：

```text
components/cilium/
  package.yaml
  files/manifests/cilium.yaml
  files/files/values/basic.yaml
  files/hooks/healthcheck.sh
```

这就是后续 apply 流程消费的 filesystem-backed desired-state artifact。

## Step 8：理解这个包里的 global 和 local

对当前 Cilium 包来说：

- `global`
  - `package.yaml`
  - `manifests/cilium.yaml`
  - `hooks/healthcheck.sh`
  - 包内的默认 `files/values/basic.yaml`
  - package identity、compatibility、dependency metadata
- `local`
  - 在 hydration 时真正绑定到 `cilium-values` 输入面的具体值
  - cluster-specific IPAM 或 routing 配置
  - 环境相关 mirror 设置
  - 其他被允许的 per-cluster overrides

这里有个容易误解的点：

`files/values/basic.yaml` 同时是：

- packaged content
- input surface

这不代表这个文件本身是 local。它仍然是包内默认值或 merge base。真正属于
`local` 的，是 hydration 时绑定进去的具体值。

## Step 9：当前实现的边界

这份指引描述的是当前仓库实现，因此有几个边界要记住：

- 当前包是 manifest-based，不是 chart-based
- values 文件会随 bundle 一起带出，但更准确的理解是“包内默认值 +
  一个声明出来的输入面”，而不是一个通用 Helm 渲染引擎
- 端到端 PoC 仍然是三组件一起工作的，所以真实 render/apply 通常还是在完整
  BOM 上进行

## 结论

当前仓库里，Cilium packaging 的实际流程就是：

1. 维护 `scripts/poc/minimal-single-node/packages/cilium` 这个
   `ComponentPackage` 目录
2. 准备或刷新 Cilium manifest、默认 values 和 healthcheck hook
3. 用 `sealos sync package inspect` 检查包
4. 用 `sealos sync package build` 构建 OCI 镜像
5. 用 `sealos sync package push` 推送
6. 在 BOM 里记录 image 和 digest
7. 从本地包目录或 OCI artifact 渲染最终 bundle

这些属于 package 与 release-build 工作。Day 0 安装从这些 assets 已经存在开始，
通过 BOM 或 `ReleaseChannel` 选择、render、apply 和 validation 完成。
