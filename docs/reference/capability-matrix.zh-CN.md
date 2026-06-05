# 当前包能力矩阵

## 状态

与当前仓库实现对齐的 BOM 驱动 MVP 能力快照

## 摘要

这份文档只回答一个很窄的问题：

- 当前仓库里，哪些包相关能力已经具备
- 哪些能力已经有了，但边界仍然故意收得很窄
- 哪些能力不应该被误解成“已经实现”

这里讨论的范围是当前 `sync` 驱动、BOM 驱动的 MVP。

## 阅读规则

把这份矩阵理解成**实现快照**，不是长期承诺。

- `Ready now`：仓库里已经有明确代码路径和测试。
- `Ready with boundary`：能力存在，但只在当前单节点或窄表面模型里成立。
- `Not implemented`：不要假设仓库已经支持这条工作流。

## 已经具备

| 能力 | 当前状态 | 主要证据 | 重要边界 |
| --- | --- | --- | --- |
| 编写并校验一个 component package 目录 | Ready now | [pkg/distribution/packageformat/load.go](../../pkg/distribution/packageformat/load.go), [cmd/sealos/cmd/sync_package.go](../../cmd/sealos/cmd/sync_package.go) | 更大范围的 `sync` 流程仍然属于实验态。 |
| 从本地 package 目录检查包元数据 | Ready now | [sync_package.go](../../cmd/sealos/cmd/sync_package.go) 里的 `sealos sync package inspect`，以及 [sync_package_test.go](../../cmd/sealos/cmd/sync_package_test.go) | 这是目录级 inspect，不是 release 选择。 |
| 从一个 component package 目录构建 OCI 镜像 | Ready now | [sync_package.go](../../cmd/sealos/cmd/sync_package.go) 里的 `sealos sync package build`，以及 [ocipackage.go](../../pkg/distribution/ocipackage/ocipackage.go) | 仍然依赖本地镜像/构建环境准备好。 |
| 推送已构建的 OCI component package image 并拿到 digest | Ready now | [sync_package.go](../../cmd/sealos/cmd/sync_package.go) 里的 `sealos sync package push`，以及 [sync_package_test.go](../../cmd/sealos/cmd/sync_package_test.go) | push 会校验返回的 OCI digest，也可以写 provenance；但不会自动回写 BOM，也不会生成签名。 |
| 把 OCI component package image 拉取成一个本地 package 目录 | Ready now | [sync_package.go](../../cmd/sealos/cmd/sync_package.go) 里的 `sealos sync package pull`，[pull.go](../../pkg/distribution/ocipackage/pull.go) 里的复用 pull/cache 逻辑 | pull 会解出 package 文件系统并校验 `package.yaml`；registry auth 仍然交给本地 image/buildah 环境。 |
| 从 BOM 解析 component package | Ready now | [pkg/distribution/bom/resolve.go](../../pkg/distribution/bom/resolve.go), [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `sync render` | 当前解析仍然是显式 BOM 驱动。 |
| 从本地目录 override 或 cached OCI artifact 两种来源解析包 | Ready now | [sync.go](../../cmd/sealos/cmd/sync.go), [pull.go](../../pkg/distribution/ocipackage/pull.go), [packageformat/load.go](../../pkg/distribution/packageformat/load.go) | BOM 驱动的 OCI 引用会先按 digest-derived key 拉到 cluster runtime cache，再由 render/validate 读取。 |
| 把 BOM render 成 hydrated desired-state bundle | Ready now | [materialize.go](../../pkg/distribution/reconcile/materialize.go), [render.go](../../pkg/distribution/hydrate/render.go) | render 虽然已经是 cluster-targeted，但仍然以当前单节点路径为中心。 |
| 把 rendered bundle apply 到当前 cluster 解析出来的目标面 | Ready now | [apply.go](../../pkg/distribution/reconcile/apply.go) | 当前部署单元仍然是 rendered bundle，多节点行为是带可选 host batching 的 executor 层能力，不是 controller。 |
| 在部署流程里消费 package contents | Ready now | `rootfs`、`file`、`manifest`、hooks 都由 [apply.go](../../pkg/distribution/reconcile/apply.go) 消费 | 这些语义在 MVP 里故意收得很窄。 |
| 根据 package input contract 初始化 cluster-local repo skeleton | Ready now | [sync_localrepo.go](../../cmd/sealos/cmd/sync_localrepo.go) 里的 `sealos sync local-repo init`，以及 [sync_test.go](../../cmd/sealos/cmd/sync_test.go) | 它只生成模板和 policy 元数据；真实 Secret 值仍然必须由运维提供。 |
| 在 validate/render 前检查 cluster-local repo 是否就绪 | Ready now | [sync_localrepo.go](../../cmd/sealos/cmd/sync_localrepo.go) 里的 `sealos sync local-repo doctor`，以及 [sync_test.go](../../cmd/sealos/cmd/sync_test.go) | 它检查未替换的 init 模板、stale component 目录、Secret-like 文件权限/kind 错误和缺失的 local patch policy；不会打印 Secret payload。 |
| 在 render/apply 里接入 local repo 的 `inputs/`、`resources/`、`patches/` | Ready now | [localrepo.go](../../pkg/distribution/localrepo/localrepo.go), [materialize.go](../../pkg/distribution/reconcile/materialize.go) | local repo 模型当前仍然是 cluster-local、single-node 范围。 |
| 在 render/apply 前校验 BOM、本地 package source、local repo 和 cluster topology | Ready now | [sync_validate.go](../../cmd/sealos/cmd/sync_validate.go) 里的 `sealos sync validate` | 这是只读校验，检查 package/local repo/topology conformance；它不是 live cluster health check。 |
| 在 render 前运行、强制执行并记录 source preflight | Ready now | [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `sealos sync preflight --file ...` 和 `sealos sync render` 默认 source gate，[bundle.go](../../pkg/distribution/hydrate/bundle.go) 里的 bundle metadata，以及 [sync_test.go](../../cmd/sealos/cmd/sync_test.go) | source preflight 聚合 local-repo doctor 和 validate，然后 render 把脱敏摘要写进 `spec.sourcePreflight`。 |
| 在 apply 前检查 rendered bundle freshness 和 runtime readiness | Ready now | [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `sealos sync preflight --bundle-dir ...`，以及 [sync_runtime_preflight.go](../../cmd/sealos/cmd/sync_runtime_preflight.go) | rendered-bundle preflight 会检查 topology/render-input freshness，以及本机 host/runtime readiness，例如权限、systemd、swap、Kubernetes 现状、端口、已存在二进制、kubeconfig/client 可用性和受管 service 状态。runtime warning 只进入结构化输出；blocking runtime check 会 gate `sync apply`。 |
| 在真正修改集群前预览 rendered bundle 的 apply 意图 | Ready now | [sync_plan.go](../../cmd/sealos/cmd/sync_plan.go) 里的 `sealos sync plan` | plan 是静态只读视图：它会解析 target、汇总资源、报告 package/phase safety profiles，并标记 generated host projections，但不会执行 SSH、kubectl 或动态 apply 探测。 |
| 通过 release metadata API 解析 release channel | Ready now | [sync_release_metadata.go](../../cmd/sealos/cmd/sync_release_metadata.go) 里的 `sealos sync release-metadata serve`、`sealos sync render --release-source --release-line --channel`，以及 [pkg/distribution/bom](../../pkg/distribution/bom) 里的 lookup client/service | 这个 service 通过 `GET /v1/distributions/{line}/channels/{channel}` 和 `GET /v1/distributions/{line}/revisions/{revision}/bom` 暴露 digest-pinned `ReleaseChannel` 和 BOM 文档。 |
| 通过 health-gated promotion API 推进 release channel | Ready now | [release_metadata_service.go](../../pkg/distribution/bom/release_metadata_service.go) 里的 `POST /v1/distributions/{line}/channels/{channel}/promotions`、[channel.go](../../pkg/distribution/bom/channel.go) 里的共享 promotion policy，以及 [channel_test.go](../../pkg/distribution/bom/channel_test.go) | service 接收包含 `targetRevision`、approval metadata 和通过的 `DistributionHealthProof` 的结构化 promotion 请求，写入 proof evidence，同时执行 source/target channel policy 和 health evidence thresholds，然后推进本地 channel 文件。它是本地 metadata service，不是多租户 hosted release platform。 |
| 用策略要求的 health proof 把本地 `ReleaseChannel` 推进到目标 BOM revision | Ready now | [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `sealos sync promote`，[channel.go](../../pkg/distribution/bom/channel.go) 里的 `DistributionHealthProof` 和 promotion helper，以及 [pkg/distribution/promotion](../../pkg/distribution/promotion) 里的 policy rules | 它会更新一份本地 channel 文件，校验目标 BOM 属于同一条线，执行 source/target channel policy，对 beta/stable 目标 channel 要求一份通过的本地 `DistributionHealthProof`，并拒绝缺失或失败的 required signals 以及不满足 `minPassedSignals` 的 proof；返回 `policyDecision`，并在存在 proof 时把 proof 元数据追加进 `spec.promotionHistory[]`。 |
| 从 package acceptance evidence 生成本地 promotion health proof | Ready now | [sync_health_proof.go](../../cmd/sealos/cmd/sync_health_proof.go) 里的 `sealos sync health-proof`，以及 [smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) 产出的 `acceptance-report.yaml` | 它会把 `PackageAcceptanceReport` 转成指向指定目标 BOM line/revision 的 `DistributionHealthProof`。生成出的 proof 会规范化 required signals，并写入 `source`、`evidenceRef`、`thresholds.requiredSignals`、`thresholds.minPassedSignals` 和 `signalSummary`。只跑 preflight 的 safe smoke、target identity/digest 不匹配、缺少 rendered desired-state/local-repo digest、缺少预期 acceptance stage contract 或 mutating marker、缺少 mutating apply、或缺少 clean post-apply evidence 时都会得到 `spec.passed: false`。 |
| 围绕 rendered desired state 做 diff/status/commit/revert | Ready now | [sync.go](../../cmd/sealos/cmd/sync.go), [pkg/distribution/compare](../../pkg/distribution/compare), [pkg/distribution/commit](../../pkg/distribution/commit) | 这是 CLI 驱动的当前 operator loop，不是 controller。 |
| 运行安全的端到端 package lifecycle smoke 流程 | Ready now | `make verify-sync-package-smoke`，底层是 [smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) | 默认 smoke 会构建当前 CLI，使用临时状态，串起 package inspect、local-repo init/doctor、source preflight、render、runtime preflight、plan 和 sourcePreflight 校验。host mutation 和 OCI image build 通过 `SYNC_PACKAGE_SMOKE_ARGS` 显式打开。 |
| 运行会修改 host 的单节点 apply acceptance 流程 | Ready now | `make verify-sync-package-apply I_UNDERSTAND_THIS_MUTATES_HOST=1`，底层是 [smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) | 这个 target 会有意修改 host。它复用 smoke 路径，在 rendered-bundle runtime preflight 通过后继续跑 `sync apply`、`sync status`、`sync diff` 和 `validate.sh`。额外 smoke 参数仍然通过 `SYNC_PACKAGE_SMOKE_ARGS` 透传。 |
| 运行会修改 host 的单节点 drift/revert acceptance 流程 | Ready now | `make verify-sync-package-revert I_UNDERSTAND_THIS_MUTATES_HOST=1`，底层是 [smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) | 这个 target 会先运行 apply acceptance 流程，然后临时制造 Cilium ConfigMap drift，确认 `sync diff` 能观察到它，再跑 object-scoped `sync revert` 并确认 rendered desired value 被恢复。这验证的是 drift recovery，不是卸载；它不会删除 Secret、PVC 或数据库这类数据面资源。 |
| 生成并校验 package lifecycle acceptance report | Ready now | [smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) 产出的 `acceptance-report.yaml`，并由 [check-report.sh](../../scripts/poc/minimal-single-node/check-report.sh) 校验 | 每次 smoke/apply/revert 都会在 workdir 下写报告；也可以通过 `--report-file` 指定路径。随后会按 `safe`、`apply` 或 `revert` 模式校验报告契约。报告记录 stage 状态、输出路径、BOM/package/local-repo identity、desired-state digest 以及 post-apply/post-revert 状态，但不会复制 Secret 内容。 |
| 为 `sync` 工作流显式覆盖 cluster runtime root | Ready now | [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `--runtime-root` | 主要用于测试、smoke 和脚本化流程，用来明确 rendered state、current bundle 和 Clusterfile inventory 从哪里读。 |
| 用 policy 驱动 cluster-local patch surface 校验 | Ready now | [policy.go](../../pkg/distribution/ownership/policy.go), [pkg/distribution/policyreport](../../pkg/distribution/policyreport) | 当前支持的 policy scope 仍然故意很窄。 |

## 已经有，但边界很窄

| 能力 | 当前状态 | 主要证据 | 边界 |
| --- | --- | --- | --- |
| 在一条统一流程里把 package 内容同时部署到 Kubernetes 和 host | Ready with boundary | [apply.go](../../pkg/distribution/reconcile/apply.go) | 当前部署单元是 rendered bundle，不是“直接安装一个 package”。 |
| 跨多个 cluster host 编排一个 rendered bundle | Ready with boundary | [topology.go](../../pkg/distribution/reconcile/topology.go), [apply.go](../../pkg/distribution/reconcile/apply.go), [kubeadm_bootstrap.go](../../pkg/distribution/reconcile/kubeadm_bootstrap.go) | CLI 驱动的 `sync apply` 路径会解析 `allNodes`、`firstMaster`、`cluster`，按 remote host staging bundle payload，生成 kubeadm join config，并在 cluster-scoped step 需要时从 remote first master 拉取 kubeconfig。package 自己的 hook/script 仍然需要具备 multi-node-safe 行为。 |
| 解析本地文件形式的 `ReleaseChannel` target | Ready with boundary | [channel.go](../../pkg/distribution/bom/channel.go), [sync.go](../../cmd/sealos/cmd/sync.go), [runner.go](../../pkg/distribution/agent/runner.go) | `--release-channel` 会加载一份本地 channel 文档，校验它的 `distribution` 和 `targetRevision` 是否匹配 `spec.bomPath` 指向的 BOM，然后 render 解析出来的 BOM。本地文件现在可以通过 `sealos sync promote` 推进；service-backed lookup 则通过 `--release-source --release-line --channel` 使用。 |
| 运行进程级 distribution reconcile agent | Ready with boundary | [main.go](../../cmd/sealos-agent/main.go), [root.go](../../cmd/sealos-agent/cmd/root.go), [runner.go](../../pkg/distribution/agent/runner.go) | `sealos-agent` 可以围绕 BOM 或本地 `ReleaseChannel` 跑一次或按 interval 循环；这仍然适合直接在 host 上执行和调试。 |
| 运行 Kubernetes controller reconcile state machine | Ready with boundary | [pkg/distribution/controller](../../pkg/distribution/controller), [root.go](../../cmd/sealos-agent/cmd/root.go) 里的 `--controller`, [deploy/distribution-controller/base](../../deploy/distribution-controller/base), [Controller install zh-CN](../guides/controller-install.zh-CN.md) | `sealos-agent --controller` 会 watch `DistributionTarget` 和 `DistributionRolloutPolicy` 对象，并把每个 target reconcile 委托给现有 agent runner，同时写入明确的 phase/status conditions、retry/backoff state、hold diagnostics 和 Kubernetes events；它也支持可选 leader election，并提供可安装 CRD/RBAC/deployment manifests。它不会自动驱动 release promotion。 |
| 对 host-targeted rendered-bundle rollout 做分批、canary、pause、health gate 和失败策略 | Ready with boundary | [sync.go](../../cmd/sealos/cmd/sync.go) 和 [root.go](../../cmd/sealos-agent/cmd/root.go) 里的 `--rollout-batch-size`、`--rollout-canary-size`、`--rollout-pause-after-canary`、`--rollout-health-gate`、`--rollout-failure-action`，[types.go](../../pkg/distribution/controller/types.go) 里的 `DistributionRolloutPolicy`，[apply.go](../../pkg/distribution/reconcile/apply.go) 里的 rollout handling，以及 [pkg/distribution/state](../../pkg/distribution/state) 中的 successful revision history | 这些控制只作用于 rendered-bundle executor 里符合条件的 host-targeted all-node runtime-rootfs steps。开启 `healthGate` 时，每批 host 完成后会先运行 component 的 `healthcheck` hooks；post-canary pause 会把 target 标成非 degraded 的暂停状态，并等待显式更新 target 或 policy；`Rollback` 会重新 apply cluster bundle store 中保留的上一次成功 rendered revision。rollback snapshot 包含 BOM/target/local revision metadata，可以跨 BOM line 回滚；durable per-package rollout cursor 仍然不在这个能力面里。 |
| 报告 package/phase rollout safety profiles | Ready with boundary | [cmd/sealos/cmd/sync_plan.go](../../cmd/sealos/cmd/sync_plan.go), [cmd/sealos/cmd/sync_test.go](../../cmd/sealos/cmd/sync_test.go) 里的 `TestSyncPlanPackageSafetyModelCoversPackageClasses` | `sync plan` 现在会为 rootfs、host-file、manifest、chart、patch、values 和各类 hook phase 报告 package/step safety profile；local patch、upgrade 和 remove 流程会标出 approval requirement，generated host projection 也会报告 gate。这仍然是静态 review 模型，不是持久 per-package rollout cursor。 |
| 跨 multi-node projection commit/revert 已建模 drift | Ready with boundary | [sync.go](../../cmd/sealos/cmd/sync.go), [sync_multinode.go](../../cmd/sealos/cmd/sync_multinode.go), [commit.go](../../pkg/distribution/commit/commit.go), [sync drift 指南](../guides/sync-drift.zh-CN.md) | Kubernetes object commit/revert 仍是 cluster-scoped。host-path revert 可以选择 local 或 remote host，用 `--component` 消除多 component 同 path 歧义，并能恢复 host-scoped local-input payload。host-path commit 只覆盖 local-input regular file；如果选中 host 有 host-scoped input 就回写 scoped input，没有 scoped provenance 且多节点内容已分叉时会拒绝提交。generated projection 会在 diff/status 中路由；只有 `repairable=true` 的已建模 control-plane host path 支持直接 repair。 |
| 跟踪 host file、Kubernetes object 和已建模 generated projection | Ready with boundary | [types.go](../../pkg/distribution/packageformat/types.go), [inventory.go](../../pkg/distribution/hydrate/inventory.go), [compare.go](../../pkg/distribution/compare/compare.go) | package 可以在 `ComponentPackage.spec.generatedOutputs.hostPaths[]` 下声明 generated host-path output；render 会把它们存进 bundle 的 `spec.trackedHostPaths[]`，并和已知 kubeadm static Pod 自动发现结果一起进入 inventory。compare engine 会语义检查 generated Kubernetes-object host file，但任意 generated artifact 类型仍需要显式建模。 |
| 管理已建模 generated host-path projection 的 drift | Ready with boundary | [compare.go](../../pkg/distribution/compare/compare.go), [compare_test.go](../../pkg/distribution/compare/compare_test.go) 里的 generated remediation 测试，以及 [sync_test.go](../../cmd/sealos/cmd/sync_test.go) 里的 CLI 输出测试 | `sync diff/status` 会把已建模 generated drift 路由到 local input、选中的 package/BOM baseline 或 manual review，并输出 `projectionClass`、`generator`、`generatedKind`、`generatedName` 和明确的 `repairable` metadata。当前自动 repair 仍然只覆盖有保留 kubeadm input 的已知 kubeadm control-plane static Pod projection。 |
| 支持 generated control-plane static Pod tracking | Ready with boundary | [inventory.go](../../pkg/distribution/hydrate/inventory.go) | kubeadm control-plane static Pod 会从保留的 kubeadm input 自动发现；package 也可以显式声明非 kubeadm generated host-path projection。 |
| 带 approval 的 local patch policy gate | Ready with boundary | [gate.go](../../pkg/distribution/policyreport/gate.go), [local_patch_policy_gate.yml](../../.github/workflows/local_patch_policy_gate.yml) | 它当前治理的是 local patch policy，不是所有未来 ownership policy。 |
| 按时间巡检 approval 卫生状态 | Ready with boundary | [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `sealos sync policy-approval-scan`，以及 [local_patch_policy_approval_scan.yml](../../.github/workflows/local_patch_policy_approval_scan.yml) | 这是一条 repo 级 approval 巡检，不是通用 cluster health controller。 |
| package/BOM 侧提供 local patch policy source | Ready with boundary | [pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go), [pkg/distribution/packageformat/types.go](../../pkg/distribution/packageformat/types.go), [pkg/distribution/hydrate/policy.go](../../pkg/distribution/hydrate/policy.go) | `LocalPatchPolicy` 的 scope 仍然只支持 `clusterLocal`；优先级是 `localRepo > bom > package > builtInDefault`，只有 package source 生效时才要求正好选中一份 package policy，也没有多层 policy merge。 |

## 还没有

下面这些能力是当前**不该被误解成已经有**的。

| 能力 | 当前状态 | 为什么重要 |
| --- | --- | --- |
| 不经过 BOM/bundle，直接“安装这个 package” | Not implemented | 当前部署路径是 `package -> BOM -> render -> bundle -> apply`，不是 package-direct install。 |
| 从 package `chart` content 直接 Helm render/apply | Not implemented | `chart` 是合法 schema content type，也会在 render 中被复制，但当前 executor 不会运行 Helm。Helm 路径实现前应使用 raw `manifest` content，或显式使用已 review 的 hook。 |
| 在 `package.yaml` 中写 inline hook command | Not implemented | Hook 目前只能引用包内文件，这样 review、digest、可执行位检查、timeout policy 和审计工具才能检查稳定 payload。 |
| 在 `ComponentPackage` 中写 dependency version range | Not implemented | package dependency 只支持命名依赖和 BOM 已选择的精确版本。BOM 和 release-channel selection 仍然是版本解析层。 |
| 从 legacy image label 推断运行时行为 | Not implemented | legacy label 可以用于显式 metadata migration，但 hook、input、dependency、local patch policy、generated output 和 chart 语义都必须来自 `package.yaml`。 |
| hosted fleet release platform | Not implemented | 本地 release metadata service 已支持 lookup 和 health-gated channel advancement，但它不是包含认证、远端存储、evidence collection 或 fleet policy management 的多租户 hosted service。 |
| 完全泛化的 generated-output drift 管理 | Not implemented | 已建模的 generated Kubernetes-object host-path drift 已经有结构化路由 metadata，但当前 MVP 仍不覆盖所有 generated artifact 类型，也不会为任意外部 generator 自动 repair。 |
| package、BOM、cluster-local 多层 policy merge | Not implemented | 当前模型刻意拒绝这层复杂度。 |

## 最实用的解释

如果把当前仓库状态压成最短一句话，就是：

- 打包：好了
- 解析：好了
- 部署：好了，它是 BOM 驱动的当前 MVP
- CLI 驱动的多节点 bundle 编排：好了，但边界很窄
- 本地文件形式的 `ReleaseChannel` 选择：好了，但边界很窄
- `sealos-agent` 进程级 reconcile：好了，但边界很窄
- `sealos-agent --controller` 最小 watched reconcile 和安装 manifests：好了，但边界很窄
- host rollout batching 和持久 batch policy：好了，但边界很窄
- health-gated host rollout 和本地 health-gated release metadata promotion 已经有了，
  但边界仍然很窄

所以当前仓库已经足够支撑：

- package 编写
- OCI package build / push / pull
- BOM 驱动的 render/apply
- 本地 `ReleaseChannel` target 选择
- 带显式 promotion policy 和 history 的本地 `ReleaseChannel` advancement
- 进程级 agent reconcile
- 带可安装 manifests 和 RBAC 的最小 `DistributionTarget` controller reconcile
- rendered bundle 的分批 host apply waves
- controller targets 的持久 batch-size rollout policy
- 当前 CLI 驱动路径上的 drift / ownership 实验

但它还不是下面这些最终形态：

- 多集群 release management
- Kubernetes controller 驱动的多节点 topology-aware deployment
- 带 fleet evidence collection 和 policy management 的多租户 hosted release platform

## 相关文档

- 包格式契约：
  [Package format](../architecture/package-format.md)
- OCI packaging milestone：
  [OCI packaging milestone](../plans/oci-packaging-milestone.md)
- 最小 prepared-host PoC：
  [Minimal Kubernetes PoC](../plans/minimal-k8s-poc.md)
- local repo 与部署闭环：
  [Local repo and secret](../guides/local-repo-and-secret.md),
  [Sync drift](../guides/sync-drift.md)
- BOM 与 `ReleaseChannel` 模型，包括当前本地文件边界：
  [BOM and channel](../guides/bom-and-channel.md)
- Controller 安装指南：
  [Controller install zh-CN](../guides/controller-install.zh-CN.md)
