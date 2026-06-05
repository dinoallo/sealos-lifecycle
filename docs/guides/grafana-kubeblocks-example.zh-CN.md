# 设计示例：用 KubeBlocks PostgreSQL 为 Grafana 打包

## 状态

设计示例

## 概述

这份文档补一个更贴近真实应用的例子：Grafana 作为可观测面板，后面带一个由
KubeBlocks `Cluster` CRD 管理的 PostgreSQL 数据库。

它不是在描述“当前仓库已经落地的实现”，而是在说明现有 package /
ownership / local binding 模型下，这类场景应该怎么拆：

- Grafana 自己的应用包怎么建模
- 数据库为什么不该被塞进同一个 opaque package
- Secret 值应该留在哪里
- KubeBlocks 运行时生成的数据库凭证应该怎么归类

## 相关文档

- 包格式契约：
  [Package format](../architecture/package-format.md)
- ownership 与 drift：
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- 顶层分发模型：
  [Distribution and config sync](../architecture/distribution-and-config-sync.md)
- release channel 与派生发行版：
  [Release and promotion](../architecture/release-and-promotion.md)
- 数据面保护 gate：
  [Data plane protection](./data-plane-protection-runbook.md)

## 外部参考

- KubeBlocks PostgreSQL quickstart：
  <https://kubeblocks.io/docs/preview/kubeblocks-for-postgresql/02-quickstart>
- KubeBlocks PostgreSQL custom secret：
  <https://kubeblocks.io/docs/preview/kubeblocks-for-postgresql/06-custom-secret/01-custom-secret>

这两个官方文档给了本例依赖的最小外部前提：

- PostgreSQL 的主 CRD 是 `apiVersion: apps.kubeblocks.io/v1`，
  `kind: Cluster`
- PostgreSQL system account 可以通过
  `componentSpecs[].systemAccounts[].secretRef` 绑定自定义 Secret
- KubeBlocks 还会为该账号自动生成自己的运行时连接 Secret

## 设计意图

这个例子回答的是一个很具体的问题：

当一个应用既有自己的 workload，又有数据库，又有 Secret 时，Sealos 应该怎
么划 ownership 边界？

推荐答案是：

- 不要把 Grafana、PostgreSQL、Secret 一起塞进一个大而 opaque 的 package
- 数据库应该作为独立 package / component，因为它有自己的存储、备份、升级
  和 blast radius
- Secret 值应该留在 local repo 或别的 cluster-local secret 系统里
- Grafana 可以消费 Secret 引用和数据库 endpoint，但不应该把凭证字节烤进
  shared artifact

## 推荐组件拆分

这个例子的第一版最干净的拆法是：

1. `kubeblocks-postgresql`
   作为平台能力包，或者作为已存在的平台前置条件，提供 KubeBlocks operator
   和 PostgreSQL addon 能力。
2. `grafana-db`
   一个 `application` package，里面带 Grafana 所需 PostgreSQL 的
   KubeBlocks `Cluster` manifest。
3. `grafana`
   一个 `application` package，里面带 Grafana 的 Deployment、Service、
   Ingress、healthcheck 和默认配置。

这个拆法符合前面已经定下来的 package-boundary 规则：

- Grafana 和 PostgreSQL 不共享同一个生命周期
- PostgreSQL 的存储、备份、容量、维护策略不是普通 app-local 参数
- 所以数据库不应该只是 Grafana values 里的一个大块配置

## 为什么不做成一个大 Grafana 包

如果把所有东西都塞进一个 package，会把下面三种本来不同的边界混在一起：

- Grafana 的 shared baseline
- 数据库的 shared baseline
- cluster-local Secret ownership

这通常会带来两个长期问题：

- 数据库策略慢慢滑进 ad-hoc local override，而不是被当作可评审的 package
  contract
- secret-bearing 数据混进 package payload 或被误提升进 shared values

这正是 package 模型应该避免的。

## Ownership 对照表

| 元素 | 推荐 owner | 原因 |
| --- | --- | --- |
| Grafana manifests、service 布局、healthcheck、默认配置结构 | `global` | 这是应该被 digest pin 并跨集群复用的应用基线。 |
| KubeBlocks PostgreSQL `Cluster` 的 manifest 结构、默认 topology、默认 service version | `global` | 这是数据库基线的一部分，由 package revision 拥有。 |
| Grafana ingress host、TLS Secret 名、storage class、数据库 Secret 名、cluster-specific service override | 通过 declared inputs 进入 `local` | 这些值会按环境变化，但应该通过受控的 binding surface 进入。 |
| Grafana admin 登录密码、PostgreSQL 账号密码的 Secret 字节 | `local` | Secret 值必须留在 cluster-local 边界里。 |
| KubeBlocks 运行时生成的账号 Secret | `local runtime state` | 它是在集群里生成的，不应该被提升回 shared artifact。 |
| 计划给所有集群复用的 dashboard、告警配置、plugin 默认值 | `global` 或单独 patch package | 这是共享平台意见，不是 per-cluster Secret 状态。 |

## 包 1：`grafana-db`

数据库包负责 KubeBlocks `Cluster` 资源和它自己的健康语义，但不应该携带真正
的密码字节。

### 建议的 package manifest 形态

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: grafana-postgresql
spec:
  component: grafana-db
  version: 16.4.0-kb.1
  class: application
  dependencies:
    - name: kubeblocks-postgresql
  inputs:
    - name: grafana-db-values
      type: valuesFile
      path: files/values/basic.yaml
      required: true
  contents:
    - name: grafana-db-manifest
      type: manifest
      path: manifests/postgresql-cluster.yaml
    - name: grafana-db-values
      type: values
      path: files/values/basic.yaml
  hooks:
    - name: healthcheck
      phase: healthcheck
      target: cluster
      path: hooks/healthcheck.sh
```

关键点是：包内那份 `basic.yaml` 应该承载默认值和结构，而不是 Secret 值。

### 建议的基线 values 结构

```yaml
clusterName: grafana-db
serviceVersion: "16.4.0"
storage:
  size: 20Gi
  className: ""
systemAccounts:
  postgres:
    secretName: grafana-db-root
```

这里的 values 只保存“要引用哪个 Secret 名”，不保存 Secret 本身。

### KubeBlocks Cluster payload 示例

最终 render 出来的 baseline manifest 可以长成这样：

```yaml
apiVersion: apps.kubeblocks.io/v1
kind: Cluster
metadata:
  name: grafana-db
spec:
  clusterDef: postgresql
  topology: replication
  terminationPolicy: Delete
  componentSpecs:
    - name: postgresql
      serviceVersion: 16.4.0
      replicas: 2
      systemAccounts:
        - name: postgres
          secretRef:
            name: grafana-db-root
```

这个 KubeBlocks CR 本身是 `global` package content。被它引用的 Secret 不是。

### 数据库账号的本地 Secret

真正的 Secret 字节应该放在 local repo：

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

这份 Secret 明确不属于 shared package artifact。

### 运行时 Secret 边界

KubeBlocks 还会为账号生成自己的运行时 Secret。按官方 PostgreSQL 文档，生成
的 Secret 名一般遵循：

- `{cluster}-{component}-account-{name}`

在这个例子里，运行时 Secret 名通常会像这样：

- `grafana-db-postgresql-account-postgres`

这份生成后的 Secret 仍然是 `local`，不是 baseline package content，也不应
该被 promotion 回上游。

## 包 2：`grafana`

Grafana 包应该携带应用 manifest、配置基线和 healthcheck，但不应该携带
admin password 或数据库 password 的字节。

### 建议的 package manifest 形态

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: grafana-observability
spec:
  component: grafana
  version: 11.0.0
  class: application
  dependencies:
    - name: grafana-db
  inputs:
    - name: grafana-values
      type: valuesFile
      path: files/values/basic.yaml
      required: true
  contents:
    - name: grafana-manifests
      type: manifest
      path: manifests/grafana.yaml
    - name: grafana-values
      type: values
      path: files/values/basic.yaml
  hooks:
    - name: healthcheck
      phase: healthcheck
      target: cluster
      path: hooks/healthcheck.sh
```

### 建议的基线 values 结构

```yaml
admin:
  existingSecret: grafana-admin-credentials
database:
  # 这里只是示意 Service 名；具体生成规则以目标版本的
  # KubeBlocks naming convention 为准。
  host: grafana-db-postgresql-postgresql
  port: 5432
  name: grafana
  # 这里只是示意生成后的 Secret 名；具体名字跟随 operator 的
  # runtime naming convention。
  credentialsSecretName: grafana-db-postgresql-account-postgres
service:
  type: ClusterIP
ingress:
  enabled: false
  host: ""
  tlsSecretName: ""
```

这样 package baseline 是明确的，但所有真正带 Secret 字节的东西都还在包外。

### Grafana admin 的本地 Secret

Grafana 管理员登录 Secret 也应该留在本地：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
type: Opaque
stringData:
  admin-user: admin
  admin-password: <cluster-local-password>
```

也就是说，package 可以引用这个 Secret 的名字，但不能携带它的凭证字节。

## BOM 里应该怎么放

一个包含这套例子的 BOM 大致会是：

```yaml
spec:
  components:
    - name: kubeblocks-postgresql
      kind: infra
      version: v1
      artifact:
        name: kubeblocks-postgresql
        image: registry.example/platform/kubeblocks-postgresql:v1
        digest: sha256:<digest>
    - name: grafana-db
      kind: infra
      version: 16.4.0-kb.1
      dependencies:
        - kubeblocks-postgresql
      artifact:
        name: grafana-postgresql
        image: registry.example/observability/grafana-postgresql:16.4.0-kb.1
        digest: sha256:<digest>
    - name: grafana
      kind: app
      version: 11.0.0
      dependencies:
        - grafana-db
      artifact:
        name: grafana-observability
        image: registry.example/observability/grafana-observability:11.0.0
        digest: sha256:<digest>
```

如果平台已经保证集群里有 KubeBlocks PostgreSQL 能力，第一个组件也可以不放
在每个 app-focused BOM 里，而是当作平台前置条件。

## 这个例子里哪些是 global，哪些是 local

### Global

- 选择 Grafana 作为面板
- 选择 KubeBlocks PostgreSQL 作为数据库
- package layout 和 manifests
- 基线 service version
- healthcheck 逻辑
- 计划给所有集群共享的 dashboard / sidecar 默认值

### Local

- `grafana-db-root` 的名字和字节
- `grafana-admin-credentials` 的名字和字节
- ingress host 和 TLS Secret 名
- storage class / size（如果它按集群变化）
- 绑定进 package inputs 的具体 Secret 引用

### Local Runtime

- KubeBlocks 运行时生成的账号 Secret 内容
- live database state
- 安装后在 Grafana UI 里交互式创建出来的 dashboard 或设置

## Promotion 规则

这个例子也很适合说明，哪些东西可以 promotion，哪些不可以。

可以 promotion 进 shared baseline 的：

- 更合理的 Grafana baseline manifest
- 经过评审的 PostgreSQL topology 调整
- 可复用的 observability dashboards
- healthcheck 修复

不应该 promotion 成 shared baseline 的：

- 单个集群的 Grafana 管理员密码
- 单个集群的 PostgreSQL 密码
- 单个集群运行时生成出来的 Secret

如果很多集群都想要同一套 dashboard 包或同一套 retention / sizing policy，
那它应该变成 package content 或 shared patch package，而不是永远留作很多份
secret-bearing local copy。

## 最后的判断口诀

对于 Grafana 这种“有数据库、有 Secret”的应用：

- 应用基线单独打包
- 数据库基线单独打包
- Secret 名和 endpoint 通过 declared inputs 绑定
- Secret 字节保留在 local repo 或别的 cluster-local secret 系统里
- 运行时生成的凭证归入 local runtime state，不要当成可上游化的 package
  material
- revert、rollback、upgrade 或 derived-line switch 触碰数据库前，先执行
  [Data plane protection](./data-plane-protection-runbook.md) gate

这是同时保住可复现性和 Secret 边界的最干净做法。
