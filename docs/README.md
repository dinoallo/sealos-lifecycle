# Sealos Distribution Docs

## Purpose

This directory contains a small documentation set for the package-based
distribution work in this repository. The files do not all serve the same
purpose: some define architecture intent, some translate that intent into
repo-scoped execution work, and some record PoC or milestone validation.

## Document Map

| Document | Role | Status |
| --- | --- | --- |
| [sealos-component-package-format-design.md](./sealos-component-package-format-design.md) | Defines the OCI-backed component package contract consumed by BOM resolution and hydration. | Design draft with implementation-aligned examples. |
| [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md) | Defines the top-level multi-cluster distribution architecture and its core boundaries. | Design draft. |
| [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md) | Defines desired-state assembly, ownership rules, drift states, and reconcile failure semantics. | Sub-design draft. |
| [sealos-materialization-tracking-and-drift-detection-model.md](./sealos-materialization-tracking-and-drift-detection-model.md) | Defines how rendered content, local repo resources, and generated outputs should be tracked and compared against live state. | Sub-design draft with [Chinese translation](./sealos-materialization-tracking-and-drift-detection-model.zh-CN.md). |
| [sealos-multi-cluster-release-and-promotion-design.md](./sealos-multi-cluster-release-and-promotion-design.md) | Defines release channels, health proof, and promotion guardrails for shared baselines. | Sub-design draft. |
| [sealos-bom-and-distribution-channel-guide.md](./sealos-bom-and-distribution-channel-guide.md) | Explains how BOM revisions, distribution lines, and `DistributionChannel` objects fit together, including Day 0 target selection and the current implementation gap. | Guide with [Chinese translation](./sealos-bom-and-distribution-channel-guide.zh-CN.md). |
| [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md) | Proposes a cluster-local repo layout and the correct way to initialize secret-bearing inputs without leaking them into shared artifacts. | Guide with [Chinese translation](./sealos-local-repo-and-secret-guide.zh-CN.md). |
| [sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md](./sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md) | Breaks the multi-cluster design into repo-scoped epics, milestones, package boundaries, and testing order. | Execution draft. |
| [sealos-cilium-packaging-walkthrough.md](./sealos-cilium-packaging-walkthrough.md) | Walks through the current Cilium component package flow from package directory to OCI image, BOM, and render output. | How-to draft with [Chinese translation](./sealos-cilium-packaging-walkthrough.zh-CN.md). |
| [sealos-grafana-kubeblocks-example.md](./sealos-grafana-kubeblocks-example.md) | Shows a design example for packaging Grafana with a KubeBlocks-managed PostgreSQL backend while keeping Secret bytes local. | Design example with [Chinese translation](./sealos-grafana-kubeblocks-example.zh-CN.md). |
| [sealos-derived-distribution-walkthrough.md](./sealos-derived-distribution-walkthrough.md) | Explains how a cluster should diverge from the shared baseline by creating a derived BOM and selectively forked package revisions. | How-to draft with [Chinese translation](./sealos-derived-distribution-walkthrough.zh-CN.md). |
| [sealos-oci-component-packaging-milestone-plan.md](./sealos-oci-component-packaging-milestone-plan.md) | Captures the milestone boundary for OCI-backed component packaging and records the verified repo outcome. | Locally validated milestone record. |
| [sealos-minimal-k8s-package-poc-plan.md](./sealos-minimal-k8s-package-poc-plan.md) | Defines and records the minimal single-node Kubernetes PoC for the package and BOM flow. | Locally validated PoC record. |

## Recommended Reading Order

1. Start with [sealos-component-package-format-design.md](./sealos-component-package-format-design.md) to understand the component artifact contract.
2. Read [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md) for the architecture-level distribution model.
3. Read [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md) for control-loop behavior, ownership, and drift semantics.
4. Read [sealos-materialization-tracking-and-drift-detection-model.md](./sealos-materialization-tracking-and-drift-detection-model.md) when you want the concrete tracking model for rendered files, Kubernetes objects whose live state is stored in etcd, and generated host-side outputs.
5. Read [sealos-multi-cluster-release-and-promotion-design.md](./sealos-multi-cluster-release-and-promotion-design.md) for release-channel and promotion policy.
6. Read [sealos-bom-and-distribution-channel-guide.md](./sealos-bom-and-distribution-channel-guide.md) when you want the object model for BOM revisions, distribution lines, and `DistributionChannel`.
7. Read [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md) when you want a concrete local-repo layout and secret-initialization workflow.
8. Use [sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md](./sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md) for repo implementation sequencing.
9. Read [sealos-cilium-packaging-walkthrough.md](./sealos-cilium-packaging-walkthrough.md) when you want a concrete example of the current packaging flow.
10. Read [sealos-grafana-kubeblocks-example.md](./sealos-grafana-kubeblocks-example.md) when you want a stateful application example that includes a database boundary and Secret handling.
11. Read [sealos-derived-distribution-walkthrough.md](./sealos-derived-distribution-walkthrough.md) when you want the supported conceptual path for a cluster to fork into a derived distribution line.
12. Use [sealos-oci-component-packaging-milestone-plan.md](./sealos-oci-component-packaging-milestone-plan.md) to understand the OCI packaging milestone that has already been proven in this repo.
13. Use [sealos-minimal-k8s-package-poc-plan.md](./sealos-minimal-k8s-package-poc-plan.md) when you need the prepared-host PoC flow and validation shape.

## Notes

- The two plan documents with local validation notes are useful as historical evidence, but they should not be read as generic guarantees for every environment.
- The multi-cluster docs now follow a three-layer structure: architecture overview, focused sub-designs, and one dedicated implementation plan.
- The implementation plan is the source for epics, milestones, command layout, and package-by-package execution breakdown.
