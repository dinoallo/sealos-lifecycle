# Sealos Distribution Docs

This directory contains the design, guides, references, examples, and project proposals for the package-based Sealos distribution work.

## Structure

| Directory | Purpose |
| --- | --- |
| [architecture/](./architecture/) | Architecture and focused sub-designs. |
| [guides/](./guides/) | Operator-facing and author-facing walkthroughs. |
| [reference/](./reference/) | Current capability snapshots and command/reference material. |
| [examples/](./examples/) | Runnable or inspectable fixtures used by guides and tests. |
| [plans/](./plans/) | Implementation plans, PoC records, and milestone notes. |
| [projects/](./projects/) | Forward-looking project proposals that are not yet folded into the main docs set. |

## Architecture

| Document | Role |
| --- | --- |
| [Package format](./architecture/package-format.md) | Defines the OCI-backed package contract consumed by BOM resolution and hydration. |
| [Distribution and config sync](./architecture/distribution-and-config-sync.md) | Defines the top-level multi-cluster distribution architecture and core boundaries. |
| [Reconcile and ownership](./architecture/reconcile-and-ownership.md) | Defines desired-state assembly, ownership rules, drift states, and failure semantics. |
| [Materialization and drift](./architecture/materialization-and-drift.md) | Defines how rendered content, local repo resources, and generated outputs are tracked and compared against live state. Chinese: [zh-CN](./architecture/materialization-and-drift.zh-CN.md). |
| [Release and promotion](./architecture/release-and-promotion.md) | Defines release channels, health proof, and promotion guardrails for shared baselines. |
| [Local patch policy](./architecture/local-patch-policy.md) | Defines `LocalPatchPolicy` source, scope, and provenance. Chinese: [zh-CN](./architecture/local-patch-policy.zh-CN.md). |

## Guides

| Document | Role |
| --- | --- |
| [Day 0 install](./guides/day-0-install.md) | Walks through installing one Sealos cluster from target selection through render, apply, and validation. Chinese: [zh-CN](./guides/day-0-install.zh-CN.md). |
| [Controller install](./guides/controller-install.md) | Installs or upgrades the `DistributionTarget` CRD, RBAC, and `sealos-agent --controller` deployment. Chinese: [zh-CN](./guides/controller-install.zh-CN.md). |
| [BOM and channel](./guides/bom-and-channel.md) | Explains BOM revisions, distribution lines, and `ReleaseChannel` objects. Chinese: [zh-CN](./guides/bom-and-channel.zh-CN.md). |
| [Local repo and secret](./guides/local-repo-and-secret.md) | Describes a cluster-local repo layout and secret initialization workflow. Chinese: [zh-CN](./guides/local-repo-and-secret.zh-CN.md). |
| [Local patch policy authoring](./guides/local-patch-policy-authoring.md) | Describes authorship, review rules, and validation for local patch policy. Chinese: [zh-CN](./guides/local-patch-policy-authoring.zh-CN.md). |
| [Sync drift](./guides/sync-drift.md) | Walks through the current `sync diff`, `sync status`, `sync commit`, and `sync revert` loop. Chinese: [zh-CN](./guides/sync-drift.zh-CN.md). |
| [Sync operations runbook](./guides/sync-operations-runbook.md) | Turns `sync diff`, `sync status`, and `operatorAction` into alert, dashboard, ticket, and repair-entry fields. Chinese: [zh-CN](./guides/sync-operations-runbook.zh-CN.md). |
| [Cilium packaging](./guides/cilium-packaging.md) | Shows the current Cilium package flow from package directory to OCI image, BOM, and render output. Chinese: [zh-CN](./guides/cilium-packaging.zh-CN.md). |
| [Grafana with KubeBlocks](./guides/grafana-kubeblocks-example.md) | Shows a stateful application packaging example with a database and Secret boundary. Chinese: [zh-CN](./guides/grafana-kubeblocks-example.zh-CN.md). |
| [Derived distribution](./guides/derived-distribution.md) | Explains how a cluster can diverge from a shared baseline with a derived BOM and forked package revisions. Chinese: [zh-CN](./guides/derived-distribution.zh-CN.md). |

## Reference

| Document | Role |
| --- | --- |
| [Capability matrix](./reference/capability-matrix.md) | Summarizes implemented package build, resolution, and deployment capabilities. Chinese: [zh-CN](./reference/capability-matrix.zh-CN.md). |
| [Sync operator actions](./reference/sync-operator-actions.md) | Lists current `operatorAction` values and direct command guardrails. Chinese: [zh-CN](./reference/sync-operator-actions.zh-CN.md). |

## Examples

| Document | Role |
| --- | --- |
| [Sync drift minimal example](./examples/sync-drift-minimal/README.md) | Provides the rendered bundle fixture, local repo fixture, applied-state fixture, and example `sync diff` / `sync status` snapshots used by current drift walkthroughs. Chinese: [zh-CN](./examples/sync-drift-minimal/README.zh-CN.md). |

## Plans

| Document | Role |
| --- | --- |
| [Distribution implementation plan](./plans/distribution-implementation-plan.md) | Breaks the multi-cluster design into repo-scoped epics, milestones, package boundaries, and testing order. |
| [OCI packaging milestone](./plans/oci-packaging-milestone.md) | Records the OCI packaging milestone boundary and verified repo outcome. |
| [Minimal Kubernetes PoC](./plans/minimal-k8s-poc.md) | Records the minimal prepared-host Kubernetes PoC for the package and BOM flow. |

## Projects

| Document | Role |
| --- | --- |
| [Git-synced distribution config](./projects/git-synced-distribution-config/proposal.md) | Proposes a future Git repository model for distribution configuration. Chinese: [zh-CN](./projects/git-synced-distribution-config/proposal.zh-CN.md). |
| [Git-synced cluster config](./projects/git-synced-distribution-config/cluster-config.md) | Proposes the companion cluster-local repository model. Chinese: [zh-CN](./projects/git-synced-distribution-config/cluster-config.zh-CN.md). |
| [Distribution document kinds](./projects/git-synced-distribution-config/kinds.md) | Catalogs CRD, source, generated, evidence, and proposed document kinds. Chinese: [zh-CN](./projects/git-synced-distribution-config/kinds.zh-CN.md). |

## Recommended Reading Order

1. Start with [Package format](./architecture/package-format.md).
2. Check [Capability matrix](./reference/capability-matrix.md) for current implementation status.
3. Read [Distribution and config sync](./architecture/distribution-and-config-sync.md) for the top-level model.
4. Read [Reconcile and ownership](./architecture/reconcile-and-ownership.md), [Materialization and drift](./architecture/materialization-and-drift.md), and [Release and promotion](./architecture/release-and-promotion.md) for the focused sub-designs.
5. Use [Day 0 install](./guides/day-0-install.md), [Controller install](./guides/controller-install.md), [BOM and channel](./guides/bom-and-channel.md), [Local repo and secret](./guides/local-repo-and-secret.md), and [Sync drift](./guides/sync-drift.md) for operational workflows.
6. Use [Sync operations runbook](./guides/sync-operations-runbook.md) when converting drift output into alerts, dashboards, tickets, and repair paths.
7. Use [Sync drift minimal example](./examples/sync-drift-minimal/README.md) when following the drift walkthroughs.
8. Use [Distribution implementation plan](./plans/distribution-implementation-plan.md) for repo-scoped execution sequencing.

## Notes

- The `examples/sync-drift-minimal/` directory is referenced by docs, tests, and workflows; keep its path stable unless those references are updated together.
- Plans and PoC records are historical evidence for this repository, not generic guarantees for every environment.
