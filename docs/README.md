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
| [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md) | Describes the top-level multi-cluster distribution model, ownership rules, and reconcile architecture. | Design draft. |
| [sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md](./sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md) | Breaks the multi-cluster design into repo-scoped epics, milestones, package boundaries, and testing order. | Execution draft. |
| [sealos-oci-component-packaging-milestone-plan.md](./sealos-oci-component-packaging-milestone-plan.md) | Captures the milestone boundary for OCI-backed component packaging and records the verified repo outcome. | Locally validated milestone record. |
| [sealos-minimal-k8s-package-poc-plan.md](./sealos-minimal-k8s-package-poc-plan.md) | Defines and records the minimal single-node Kubernetes PoC for the package and BOM flow. | Locally validated PoC record. |

## Recommended Reading Order

1. Start with [sealos-component-package-format-design.md](./sealos-component-package-format-design.md) to understand the component artifact contract.
2. Read [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md) for the broader pull-based distribution model.
3. Use [sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md](./sealos-multi-cluster-distribution-and-config-sync-implementation-plan.md) for repo implementation sequencing.
4. Use [sealos-oci-component-packaging-milestone-plan.md](./sealos-oci-component-packaging-milestone-plan.md) to understand the OCI packaging milestone that has already been proven in this repo.
5. Use [sealos-minimal-k8s-package-poc-plan.md](./sealos-minimal-k8s-package-poc-plan.md) when you need the prepared-host PoC flow and validation shape.

## Notes

- The two plan documents with local validation notes are useful as historical evidence, but they should not be read as generic guarantees for every environment.
- The multi-cluster design document captures architecture intent. The dedicated implementation plan is the source for epics, milestones, and package-by-package execution breakdown.
