# Sealos Distribution 文档

本目录保存 package-based Sealos distribution 工作的设计、指南、参考、示例和项目 proposal。

## 结构

| 目录 | 用途 |
| --- | --- |
| [architecture/](./architecture/) | 架构和聚焦的子设计。 |
| [guides/](./guides/) | 面向 operator 和 author 的 walkthrough。 |
| [reference/](./reference/) | 当前能力快照和命令/参考材料。 |
| [examples/](./examples/) | guide 和测试使用的可运行或可检查 fixture。 |
| [plans/](./plans/) | 实施计划、PoC 记录和 milestone notes。 |
| [projects/](./projects/) | 尚未合入主文档集的前瞻项目 proposal。 |

## Architecture

| 文档 | 作用 |
| --- | --- |
| [Package format](./architecture/package-format.md) | 定义 BOM resolution 和 hydration 使用的 OCI-backed package contract。 |
| [Distribution and config sync](./architecture/distribution-and-config-sync.md) | 定义 multi-cluster distribution 的顶层架构和核心边界。 |
| [Reconcile and ownership](./architecture/reconcile-and-ownership.md) | 定义 desired-state assembly、ownership rules、drift states 和 failure semantics。 |
| [Materialization and drift](./architecture/materialization-and-drift.md) | 定义 rendered content、local repo resources 和 generated outputs 如何被追踪并与 live state 比较。中文：[zh-CN](./architecture/materialization-and-drift.zh-CN.md)。 |
| [Release and promotion](./architecture/release-and-promotion.md) | 定义 release channels、health proof 和 shared baseline promotion guardrails。 |
| [Local patch policy](./architecture/local-patch-policy.md) | 定义 `LocalPatchPolicy` 的 source、scope 和 provenance。中文：[zh-CN](./architecture/local-patch-policy.zh-CN.md)。 |

## Guides

| 文档 | 作用 |
| --- | --- |
| [Day 0 install](./guides/day-0-install.md) | 走读从 target selection 到 render、apply、validation 的单集群安装流程。中文：[zh-CN](./guides/day-0-install.zh-CN.md)。 |
| [Controller install](./guides/controller-install.md) | 安装或升级 `DistributionTarget` CRD、RBAC 和 `sealos-agent --controller` deployment。中文：[zh-CN](./guides/controller-install.zh-CN.md)。 |
| [BOM and channel](./guides/bom-and-channel.md) | 说明 BOM revisions、distribution lines 和 `ReleaseChannel` objects。中文：[zh-CN](./guides/bom-and-channel.zh-CN.md)。 |
| [Local repo and secret](./guides/local-repo-and-secret.md) | 描述 cluster-local repo layout 和 secret initialization workflow。中文：[zh-CN](./guides/local-repo-and-secret.zh-CN.md)。 |
| [Local patch policy authoring](./guides/local-patch-policy-authoring.md) | 描述 local patch policy 的 authorship、review rules 和 validation。中文：[zh-CN](./guides/local-patch-policy-authoring.zh-CN.md)。 |
| [Sync drift](./guides/sync-drift.md) | 走读当前 `sync diff`、`sync status`、`sync commit` 和 `sync revert` loop。中文：[zh-CN](./guides/sync-drift.zh-CN.md)。 |
| [Sync operations runbook](./guides/sync-operations-runbook.md) | 把 `sync diff`、`sync status` 和 `operatorAction` 转成告警、dashboard、工单和修复入口字段。中文：[zh-CN](./guides/sync-operations-runbook.zh-CN.md)。 |
| [Cilium packaging](./guides/cilium-packaging.md) | 展示当前 Cilium package 从 package directory 到 OCI image、BOM 和 render output 的流程。中文：[zh-CN](./guides/cilium-packaging.zh-CN.md)。 |
| [Grafana with KubeBlocks](./guides/grafana-kubeblocks-example.md) | 展示包含 database 和 Secret boundary 的 stateful application packaging 示例。中文：[zh-CN](./guides/grafana-kubeblocks-example.zh-CN.md)。 |
| [Derived distribution](./guides/derived-distribution.md) | 说明 cluster 如何通过 derived BOM 和 forked package revisions 偏离 shared baseline。中文：[zh-CN](./guides/derived-distribution.zh-CN.md)。 |

## Reference

| 文档 | 作用 |
| --- | --- |
| [Capability matrix](./reference/capability-matrix.md) | 汇总已实现的 package build、resolution 和 deployment capabilities。中文：[zh-CN](./reference/capability-matrix.zh-CN.md)。 |
| [Sync operator actions](./reference/sync-operator-actions.md) | 列出当前 `operatorAction` values 和 direct command guardrails。中文：[zh-CN](./reference/sync-operator-actions.zh-CN.md)。 |

## Examples

| 文档 | 作用 |
| --- | --- |
| [Sync drift minimal example](./examples/sync-drift-minimal/README.md) | 提供当前 drift walkthrough 使用的 rendered bundle fixture、local repo fixture、applied-state fixture 和 `sync diff` / `sync status` snapshots。中文：[zh-CN](./examples/sync-drift-minimal/README.zh-CN.md)。 |

## Plans

| 文档 | 作用 |
| --- | --- |
| [Distribution implementation plan](./plans/distribution-implementation-plan.md) | 将 multi-cluster design 拆成 repo-scoped epics、milestones、package boundaries 和 testing order。 |
| [OCI packaging milestone](./plans/oci-packaging-milestone.md) | 记录 OCI packaging milestone boundary 和已验证的 repo outcome。 |
| [Minimal Kubernetes PoC](./plans/minimal-k8s-poc.md) | 记录 package 和 BOM flow 的 minimal prepared-host Kubernetes PoC。 |

## Projects

| 文档 | 作用 |
| --- | --- |
| [Git-synced distribution config](./projects/git-synced-distribution-config/proposal.md) | 提出未来 distribution configuration 的 Git 仓库模型。中文：[zh-CN](./projects/git-synced-distribution-config/proposal.zh-CN.md)。 |
| [Git-synced cluster config](./projects/git-synced-distribution-config/cluster-config.md) | 提出配套的 cluster-local repository model。中文：[zh-CN](./projects/git-synced-distribution-config/cluster-config.zh-CN.md)。 |
| [Distribution document kinds](./projects/git-synced-distribution-config/kinds.md) | 汇总 CRD、source、generated、evidence 和 proposed document kinds。中文：[zh-CN](./projects/git-synced-distribution-config/kinds.zh-CN.md)。 |

## 推荐阅读顺序

1. 从 [Package format](./architecture/package-format.md) 开始。
2. 查看 [Capability matrix](./reference/capability-matrix.md) 了解当前实现状态。
3. 阅读 [Distribution and config sync](./architecture/distribution-and-config-sync.md) 理解顶层模型。
4. 阅读 [Reconcile and ownership](./architecture/reconcile-and-ownership.md)、[Materialization and drift](./architecture/materialization-and-drift.md) 和 [Release and promotion](./architecture/release-and-promotion.md) 理解聚焦子设计。
5. 使用 [Day 0 install](./guides/day-0-install.md)、[Controller install](./guides/controller-install.md)、[BOM and channel](./guides/bom-and-channel.md)、[Local repo and secret](./guides/local-repo-and-secret.md) 和 [Sync drift](./guides/sync-drift.md) 完成操作流程。
6. 使用 [Sync operations runbook](./guides/sync-operations-runbook.md) 把 drift 输出转成告警、dashboard、工单和修复路径。
7. 跟随 drift walkthrough 时使用 [Sync drift minimal example](./examples/sync-drift-minimal/README.md)。
8. 使用 [Distribution implementation plan](./plans/distribution-implementation-plan.md) 了解 repo-scoped execution sequencing。

## Notes

- `examples/sync-drift-minimal/` 会被 docs、tests 和 workflows 引用；除非同步更新这些引用，否则应保持路径稳定。
- Plans 和 PoC records 是本仓库的历史验证材料，不是对所有环境的通用保证。
