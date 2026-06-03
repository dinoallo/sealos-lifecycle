# Proposal: Git 同步的集群配置

## 状态

草案

## 摘要

本文定义独立的 `cluster-config` Git 仓库布局，用于保存 Sealos lifecycle 的集群本地配置。

[distribution configuration proposal](proposal.zh-CN.md) 定义平台发行版事实：package source、build class、profile、BOM、channel 和共享 validation policy。`cluster-config` 仓库负责集群本地意图：`ClusterTarget`、delivery policy 选择、非 secret inputs、local patches 和 secret references。

将两个仓库分离可以让所有权、访问控制和 promotion 更安全。平台团队可以在看不到集群私有值的情况下推进全局 release 内容；集群 owner 也可以在不编辑共享发行版定义的情况下修改本地输入。

共享的 document kind 目录见 [document kind 规范](kinds.zh-CN.md)。

## 目标

- 为每个集群提供一个稳定入口。
- 让 cluster-local inputs 和 patches 不进入 `distribution-config`。
- 允许集群 owner 在平台策略允许范围内选择 distribution、channel、profile 和 delivery mode。
- 支持私有集群 pull-based 同步。
- 默认不把 secret 值放进 Git。
- 让影响某个集群的文件容易查找和校验。

## 非目标

- 定义 `distribution-config` 仓库布局。
- 替换 release BOM 或 channel 模型。
- 将 rendered bundle 或构建后的 package artifact 作为事实源保存。
- 保存明文 private key、token、certificate 或 secret 值。
- 定义 Git 仓库托管、认证或分支保护要求。

## 推荐仓库模型

环境 owner 使用独立的 cluster configuration 仓库：

```text
cluster-config/
  clusters/
    prod/
      prod-a/
        target.yaml
        inputs/
          kubernetes.yaml
          cilium.yaml
        patches/
          kube-apiserver-audit.yaml
        secrets/
          refs.yaml
    staging/
      staging-a/
        target.yaml
        inputs/
        patches/
  policy/
    validation/
  README.md
```

该仓库应保存集群本地源配置。不应保存生成的 render 结果、local cache、下载后的 package artifact 或构建后的 package payload。

## 目录职责

| 路径 | 职责 |
| --- | --- |
| `clusters/<scope>/<cluster>/target.yaml` | 集群选择 distribution、channel、profile、delivery mode、inputs 和 patches 的稳定入口。 |
| `clusters/<scope>/<cluster>/inputs/` | 非 secret 的集群特定 package inputs。 |
| `clusters/<scope>/<cluster>/patches/` | 集群本地 overlay 或结构化 patch。 |
| `clusters/<scope>/<cluster>/secrets/` | secret reference，或在运维模型允许时保存 encrypted/sealed secret material。 |
| `policy/` | CI 或 agent-side preflight checks 使用的校验规则。 |

## Cluster Target

每个集群应有且仅有一个 `target.yaml`。

示例：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ClusterTarget
metadata:
  name: prod-a
spec:
  distribution: default-platform
  channel: stable
  profile: prod-amd64
  delivery:
    mode: preferArtifact
  distributionRef:
    name: platform
    ref: main
  localPatchRevision: prod-a-20240425
  inputs:
    - component: kubernetes
      path: inputs/kubernetes.yaml
    - component: cilium
      path: inputs/cilium.yaml
  patches:
    - path: patches/kube-apiserver-audit.yaml
  secrets:
    - path: secrets/refs.yaml
```

target 文件不应复制 BOM 内容。它选择 distribution、channel 和 profile，然后指向在 distribution defaults 之后应用的本地 inputs 和 patches。

仓库 URL 和凭据应由 agent 配置或部署 bootstrap 提供。`distributionRef` 可以命名一个已配置的 distribution 仓库和 Git ref，但不应嵌入凭据。

## Delivery Policy

在策略允许时，cluster target 可以选择 package 的 materialization 方式：

| 模式 | 行为 |
| --- | --- |
| `artifact` | 从 OCI 或其他配置的 artifact store 按 digest 拉取预构建 package artifact。 |
| `localBuild` | 从选中 BOM 锁定的 source facts 和 build contract 本地构建 package。 |
| `preferArtifact` | 当预构建 artifact 可用且策略允许时优先拉取；否则回退到 local build。 |

改变 delivery mode 不应改变选中的 distribution revision、package graph、feature resolution、input merge order 或 patch order。它只改变 render/apply 前 package payload 如何 materialize。

## Inputs 和 Patches

Inputs 应该是小型、非 secret、按组件划分的 YAML 文档。

输入示例：

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentInput
metadata:
  name: prod-a-kubernetes
spec:
  component: kubernetes
  values:
    clusterCIDR: 10.244.0.0/16
    serviceCIDR: 10.96.0.0/12
    controlPlaneEndpoint: api.prod-a.example.com
```

Patches 应是显式 overlay 或结构化 patch 文档。不应依赖 cluster 目录之外的隐式文件发现。

`target.yaml` 中的路径相对于 cluster root，例如 `clusters/prod/prod-a/`。应拒绝绝对路径和 `..` 路径穿越。

## Secret 处理

Secret 值不应以明文形式保存在 Git 文件中。

可接受模式：

- Git 中只保存 secret reference
- 如果运维模型支持，可以在私有 cluster 仓库中保存 sealed 或 encrypted secrets
- hydration 阶段从集群内 secret store 注入敏感值
- certificate 和 private key 不进入 package artifact，而是在运行时作为 local input 提供

distribution package manifest 可以声明必需的 secret-shaped inputs。`cluster-config` 负责将这些需求绑定到本地 secret references 或 runtime injection points。

## 解析契约

agent 或 operator 应按确定性顺序解析一个集群：

1. 读取该集群的 `target.yaml`。
2. 解析已配置的 `distribution-config` 仓库和 Git ref。
3. 将选中的 release channel 解析到一个 BOM。
4. 解析选中的 distribution profile。
5. 按选中的 delivery mode materialize packages。
6. 按文档化顺序合并 package defaults、profile defaults、cluster inputs 和 cluster patches。
7. 从已批准的本地来源注入必需 secrets。
8. 执行 render/apply，或将生成结果写入本地 workspace 或 CI artifact。

如果引用文件缺失、本地路径逃逸 cluster root、选中的 channel/profile 不存在、delivery mode 不被允许、必需 secret binding 缺失，或 patch 无法干净应用，解析应拒绝继续。

## 校验

cluster configuration 仓库的 CI 或 agent preflight checks 应校验：

- 每个 `target.yaml` 都能按 `ClusterTarget` 解析
- 每个 cluster target 都选择了允许的 distribution、channel、profile 和 delivery mode
- 每个引用的 input、patch 和 secret reference path 都存在
- 本地路径是相对路径，且不能逃逸 cluster root
- 非 secret input 文件不包含明显 secret material
- patch 能基于选中的 distribution profile 和 package defaults 干净应用
- local build mode 只允许用于具备所需构建能力的集群
- generated output path 和 local cache 已被 Git ignore

## 建议

所有 cluster-local state 都应放在 `cluster-config`：

```text
clusters/<scope>/<cluster>/target.yaml
clusters/<scope>/<cluster>/inputs/
clusters/<scope>/<cluster>/patches/
clusters/<scope>/<cluster>/secrets/
```

平台拥有的 distribution state 保持在 `distribution-config`。agent 在解析时将两个仓库组合起来。
