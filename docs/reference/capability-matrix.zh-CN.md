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
| 推送已构建的 OCI component package image 并拿到 digest | Ready now | [sync_package.go](../../cmd/sealos/cmd/sync_package.go) 里的 `sealos sync package push` | push 只是传输动作，不会自动回写 BOM。 |
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
| 在真正修改集群前预览 rendered bundle 的 apply 意图 | Ready now | [sync_plan.go](../../cmd/sealos/cmd/sync_plan.go) 里的 `sealos sync plan` | plan 是静态只读视图：它会解析 target 并汇总资源，但不会执行 SSH、kubectl 或动态 apply 探测。 |
| 用策略要求的 health proof 把本地 `ReleaseChannel` 推进到目标 BOM revision | Ready now | [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `sealos sync promote`，[channel.go](../../pkg/distribution/bom/channel.go) 里的 `DistributionHealthProof` 和 promotion helper，以及 [pkg/distribution/promotion](../../pkg/distribution/promotion) 里的 policy rules | 它会更新一份本地 channel 文件，校验目标 BOM 属于同一条线，执行 source/target channel policy，对 beta/stable 目标 channel 要求一份针对同一 line/revision、通过且至少包含一个 signal 的本地 `DistributionHealthProof`，返回 `policyDecision`，并在存在 proof 时把 proof 元数据追加进 `spec.promotionHistory[]`。这不是 registry/API-backed release lookup，也不是 health-proof ingestion service。 |
| 从 package acceptance evidence 生成本地 promotion health proof | Ready now | [sync_health_proof.go](../../cmd/sealos/cmd/sync_health_proof.go) 里的 `sealos sync health-proof`，以及 [smoke.sh](../../scripts/poc/minimal-single-node/smoke.sh) 产出的 `acceptance-report.yaml` | 它会把 `PackageAcceptanceReport` 转成指向指定目标 BOM line/revision 的 `DistributionHealthProof`。生成逻辑是保守的：只跑 preflight 的 safe smoke report、report BOM file、已 render 的 BOM line/revision 或 BOM digest 和目标 BOM 不一致、缺少 rendered desired-state/local-repo revision digest、缺少预期 acceptance stage contract 或 mutating-stage marker、缺少 mutating apply evidence、或缺少 clean post-apply evidence 时都会得到 `spec.passed: false`，因此不会满足 beta/stable promotion gate。 |
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
| 解析本地文件形式的 `ReleaseChannel` target | Ready with boundary | [channel.go](../../pkg/distribution/bom/channel.go), [sync.go](../../cmd/sealos/cmd/sync.go), [runner.go](../../pkg/distribution/agent/runner.go) | `--release-channel` 会加载一份本地 channel 文档，校验它的 `distribution` 和 `targetRevision` 是否匹配 `spec.bomPath` 指向的 BOM，然后 render 解析出来的 BOM。本地文件现在可以通过 `sealos sync promote` 推进；但还没有 registry/API 驱动的 distribution line + channel lookup。 |
| 运行进程级 distribution reconcile agent | Ready with boundary | [main.go](../../cmd/sealos-agent/main.go), [root.go](../../cmd/sealos-agent/cmd/root.go), [runner.go](../../pkg/distribution/agent/runner.go) | `sealos-agent` 可以围绕 BOM 或本地 `ReleaseChannel` 跑一次或按 interval 循环；这仍然适合直接在 host 上执行和调试。 |
| 运行最小 Kubernetes controller reconcile loop | Ready with boundary | [pkg/distribution/controller](../../pkg/distribution/controller), [root.go](../../cmd/sealos-agent/cmd/root.go) 里的 `--controller`, [deploy/distribution-controller/base](../../deploy/distribution-controller/base), [Controller install zh-CN](../guides/controller-install.zh-CN.md) | `sealos-agent --controller` 会 watch `DistributionTarget` 和 `DistributionRolloutPolicy` 对象，并把每个 target reconcile 委托给现有 agent runner，同时写 status condition，也支持可选 leader election，并提供可安装 CRD/RBAC/deployment manifests。registry-backed channel lookup 和 health-gated promotion automation 仍然没有实现。 |
| 对 host-targeted rendered-bundle rollout 做分批、canary、pause、health gate 和失败策略 | Ready with boundary | [sync.go](../../cmd/sealos/cmd/sync.go) 和 [root.go](../../cmd/sealos-agent/cmd/root.go) 里的 `--rollout-batch-size`、`--rollout-canary-size`、`--rollout-pause-after-canary`、`--rollout-health-gate`、`--rollout-failure-action`，[types.go](../../pkg/distribution/controller/types.go) 里的 `DistributionRolloutPolicy`，以及 [apply.go](../../pkg/distribution/reconcile/apply.go) 里的 rollout handling | 这些控制只作用于 rendered-bundle executor 里符合条件的 host-targeted all-node runtime-rootfs steps。开启 `healthGate` 时，每批 host 完成后会先运行 component 的 `healthcheck` hooks；post-canary pause 会把 target 标成非 degraded 的暂停状态，并等待显式更新 target 或 policy；`Rollback` 会重新 apply cluster bundle store 中保留的上一次成功 rendered revision，然后等待显式更新 target 或 policy。这仍然不是覆盖所有 multi-node workflow 的 package 级安全模型。 |
| 从多节点中的指定 host commit 一个 local input 绑定出来的 host file | Ready with boundary | [sync.go](../../cmd/sealos/cmd/sync.go), [commit.go](../../pkg/distribution/commit/commit.go) | 当前 multi-node commit 支持面故意很窄：只覆盖 local-input regular file；如果选中 host 有 host-scoped input，就回写 scoped input；如果没有 scoped provenance 且多节点内容已经分叉，就拒绝把单个节点的值覆盖到默认 input。 |
| 跟踪 host file、Kubernetes object 和部分 generated projection | Ready with boundary | [inventory.go](../../pkg/distribution/hydrate/inventory.go), [compare.go](../../pkg/distribution/compare/compare.go) | generated projection 覆盖面刻意很窄。 |
| 支持 generated control-plane static Pod tracking | Ready with boundary | [inventory.go](../../pkg/distribution/hydrate/inventory.go) | 只覆盖明确建模的 kubeadm-generated static Pod 集合。 |
| 带 approval 的 local patch policy gate | Ready with boundary | [gate.go](../../pkg/distribution/policyreport/gate.go), [local_patch_policy_gate.yml](../../.github/workflows/local_patch_policy_gate.yml) | 它当前治理的是 local patch policy，不是所有未来 ownership policy。 |
| 按时间巡检 approval 卫生状态 | Ready with boundary | [sync.go](../../cmd/sealos/cmd/sync.go) 里的 `sealos sync policy-approval-scan`，以及 [local_patch_policy_approval_scan.yml](../../.github/workflows/local_patch_policy_approval_scan.yml) | 这是一条 repo 级 approval 巡检，不是通用 cluster health controller。 |
| package/BOM 侧提供 local patch policy source | Ready with boundary | [pkg/distribution/bom/types.go](../../pkg/distribution/bom/types.go), [pkg/distribution/packageformat/types.go](../../pkg/distribution/packageformat/types.go), [pkg/distribution/hydrate/policy.go](../../pkg/distribution/hydrate/policy.go) | `LocalPatchPolicy` 的 scope 仍然只支持 `clusterLocal`；优先级是 `localRepo > bom > package > builtInDefault`，只有 package source 生效时才要求正好选中一份 package policy，也没有多层 policy merge。 |

## 还没有

下面这些能力是当前**不该被误解成已经有**的。

| 能力 | 当前状态 | 为什么重要 |
| --- | --- | --- |
| 不经过 BOM/bundle，直接“安装这个 package” | Not implemented | 当前部署路径是 `package -> BOM -> render -> bundle -> apply`，不是 package-direct install。 |
| 覆盖所有 multi-node workflow 的 package 级安全模型 | Not implemented | 持久 rollout policy 现在已经覆盖符合条件的 rendered-bundle host batches，支持 canary、pause、health gate、stop 和 rollback；但 bootstrap rootfs、manifest-only 以及 package-specific safety model 仍需要单独设计。 |
| live `ReleaseChannel` release lookup 和 health-gated promotion service | Not implemented | 本地文件形式的 `ReleaseChannel` resolution 和 `sealos sync promote` advancement 已经有了，但还没有 registry/API 驱动的 `distribution line + channel` lookup，也没有 health-gated promotion service。 |
| 完全泛化的 generated-output drift 管理 | Not implemented | 当前 MVP 只跟踪一组已知 generated target。 |
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
- 带 health gate 的 rollout 只覆盖符合条件的 host 分批；发布系统化还没好

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
- registry/API-backed 且 health-gated 的 promotion automation

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
