# Design Proposal: Sealos Component Package Format

## Status

Draft

## Summary

This document defines how a Sealos component should be packaged as an OCI artifact so that it can participate in BOM resolution, local hydration, reconciliation, and promotion.

The package format is intentionally based on the repository's current image model rather than replacing it. Today the code already distinguishes `rootfs`, `patch`, and `application` images, merges image metadata, and applies them with different execution semantics. The package format makes that contract explicit and machine-readable.

## Why This Design Is Needed

The multi-cluster design now has BOM and applied-revision schemas, but it still lacks a clear answer to one operational question: what exactly is a component artifact?

Without a formal package contract:

- BOM entries only point at digests, not at a well-defined payload structure.
- Hydration logic has no reliable way to discover manifests, charts, files, or hooks.
- Operators cannot reason about compatibility, inputs, or dependencies before apply time.
- Promotion is difficult because the contents and intent of a packaged component are implicit.

## Existing Repository Model

The current repository already contains the seeds of a package model:

- `rootfs`, `patch`, and `application` image classes in [pkg/types/v1beta1/cluster.go](/home/allosaurus/Workspace/sealos-lifecycle/pkg/types/v1beta1/cluster.go:52)
- image metadata merging in [pkg/image/merge.go](/home/allosaurus/Workspace/sealos-lifecycle/pkg/image/merge.go:50)
- class-specific execution behavior in [pkg/guest/guest.go](/home/allosaurus/Workspace/sealos-lifecycle/pkg/guest/guest.go:43)
- config file injection through Clusterfile `Config` objects in [pkg/config/config.go](/home/allosaurus/Workspace/sealos-lifecycle/pkg/config/config.go:31)

The new package format should preserve compatibility with this behavior while making the package layout explicit.

## Goals

- Define one package unit that can be referenced from a BOM.
- Preserve compatibility with existing Sealos image classes.
- Make package contents discoverable without bespoke image inspection logic.
- Declare dependencies, compatibility, inputs, and hooks explicitly.
- Support deterministic local hydration inside the cluster.

## Non-Goals

- Replacing the existing image transport. OCI remains the transport.
- Encoding every runtime policy or promotion rule into the package manifest.
- Solving local patch packaging in the same format on day one.

## Packaging Unit

The packaging unit is one component revision stored as one immutable OCI artifact.

Examples:

- `kubernetes rootfs`
- `calico cni`
- `ingress-nginx`
- `registry mirror patch`

The BOM should reference the artifact digest for that component revision. The package manifest inside the artifact describes what the component contains and how it should be consumed.

## Package Classes

The package format should preserve the existing class model:

- `rootfs`: node-level system baseline content
- `patch`: overlay content that modifies or extends an existing baseline
- `application`: cluster workload content

These classes are not only labels. They imply default execution semantics:

- `rootfs` packages are primarily host- or node-oriented and usually target all nodes.
- `patch` packages are overlays and may target all nodes or cluster-scoped content depending on the declared contents.
- `application` packages are cluster workload packages and usually target the cluster API or first master path.

## Recommended Packaging Model

Package boundaries should follow operational lifecycle, not individual binaries.

That means the initial Kubernetes-oriented model should avoid splitting `kubelet`, `kube-apiserver`, `kube-controller-manager`, `kube-scheduler`, and similar pieces into separate artifacts. Those parts are tightly version-coupled, usually rolled out together, and are more naturally customized through config and overlays than through independent package lifecycles.

Recommended initial package set:

- `containerd-runtime` as a `rootfs` package
- `kubernetes-rootfs` as a `rootfs` package
- `cilium-cni` as an `application` package
- later `kubernetes-control-plane-patch` as a `patch` package

If Sealos manages the container runtime, it should be modeled as a separate package such as `containerd-runtime`, not folded into the Kubernetes package split.

### Package Boundary Matrix

| Package | Class | Owns | Why It Is A Package |
| --- | --- | --- | --- |
| `containerd-runtime` | `rootfs` | `containerd`, `ctr`, `containerd-shim-runc-v2`, `runc`, runtime config, systemd units or drop-ins | The host runtime has a distinct lifecycle boundary and should remain separable from Kubernetes bootstrap payloads. |
| `kubernetes-rootfs` | `rootfs` | `kubeadm`, `kubelet`, `kubectl`, host baseline files, systemd units or drop-ins, kubeadm defaults, bootstrap hooks | These assets bootstrap the node baseline and usually move together as one Kubernetes revision. |
| `cilium-cni` | `application` | Cilium manifests, optional values files, networking health checks | Networking has its own lifecycle, release cadence, and operational owner. |
| `kubernetes-control-plane-patch` | `patch` | audit policy, admission config, extra API server flags, static-pod patches, kubelet config overlays, policy manifests | This is the right layer for SRE customization that should be reusable across clusters without forking the rootfs package. |

### Inputs Vs Patch Packages

The packaging model should distinguish three customization levels:

- `inputs` for cluster-specific values that are expected to vary per installation
- `patch` packages for reusable opinionated overlays managed by platform or SRE teams
- separate packages only for components with a genuinely independent lifecycle

Good candidates for `inputs`:

- cluster name
- advertise address
- pod CIDR and service CIDR
- registry and image mirror overrides
- kubeadm config fragments
- kubelet extra environment values
- Cilium basic values overrides that differ per cluster

Good candidates for a later `kubernetes-control-plane-patch` package:

- audit policy
- admission controller configuration
- API server extra args and extra volumes
- static-pod manifest patches
- kubelet config overlays
- cluster policy defaults that should travel as one reusable platform opinion

Good candidates for separate packages later:

- CNI
- CSI
- ingress controller
- observability stack
- service mesh

The container runtime is the one host-level exception that is also reasonable as an initial separate package, because operators may want Sealos either to manage it explicitly or to leave it completely external.

Anti-recommendation:

- do not package `kubelet`, `kube-apiserver`, `kube-controller-manager`, `kube-scheduler`, or `kube-proxy` as separate first-class packages in the initial design

That split would create unnecessary dependency and upgrade complexity without matching how most operators actually manage Kubernetes.

### Evolution Path

Recommended adoption sequence:

1. PoC: `containerd-runtime`, `kubernetes-rootfs`, and `cilium-cni`
2. Early production model: add `kubernetes-control-plane-patch`
3. Broader platform model: add independently owned addon packages such as CSI, ingress, and observability

This gives SREs a place to express opinionated control-plane customizations without turning every Kubernetes daemon into its own package.

## Transport And Discovery

OCI remains the artifact transport. Each artifact should contain a package manifest at a stable path:

```text
/package.yaml
```

The manifest should also be discoverable via OCI metadata:

- existing labels: `sealos.io.type`, `sealos.io.version`, `sealos.io.distribution`
- new preferred manifest kind: `distribution.sealos.io/v1alpha1`, `ComponentPackage`

Legacy images may continue to rely on current labels, but all new packaged components should include `package.yaml`.

## Proposed Package Layout

The package payload should use a simple, explicit layout:

```text
/
  package.yaml
  rootfs/
  manifests/
  charts/
  patches/
  files/
  hooks/
```

Layout rules:

- `package.yaml` is required.
- All referenced paths must be relative to the package root.
- Directories are optional unless referenced by the manifest.
- Secret values should not be baked into the package; they must be provided through local hydration inputs.

## Worked Example

A fuller production-style Kubernetes rootfs example now lives at [pkg/distribution/packageformat/testdata/kubernetes-production-rootfs/package.yaml](/home/allosaurus/Workspace/sealos-lifecycle/pkg/distribution/packageformat/testdata/kubernetes-production-rootfs/package.yaml:1).

A matching BOM example for that package now lives at [pkg/distribution/bom/testdata/default-platform-production-bom.yaml](/home/allosaurus/Workspace/sealos-lifecycle/pkg/distribution/bom/testdata/default-platform-production-bom.yaml:1).

That fixture is intentionally more complete than the minimal bootstrap example. It shows:

- multiple declared hydration inputs
- host-level rootfs payload plus cluster manifests
- explicit bootstrap and healthcheck hooks
- compatibility and dependency declarations

## Package Manifest

The manifest describes the package contents and runtime contract.

Required top-level fields:

- `apiVersion`
- `kind`
- `metadata.name`
- `spec.component`
- `spec.version`
- `spec.class`
- `spec.contents`

Key optional fields:

- `spec.dependencies`
- `spec.compatibility`
- `spec.inputs`
- `spec.hooks`

## Content Model

The package manifest should describe content by type and path rather than by hard-coded conventions alone.

Initial content types:

- `rootfs`
- `manifest`
- `chart`
- `patch`
- `file`
- `values`
- `hook`

This keeps the format flexible enough for current Sealos behavior without allowing arbitrary hidden payloads.

## Inputs

Packages should declare the external inputs they expect during hydration.

Initial input types:

- `configFile`
- `valuesFile`
- `env`

This is important because current application packaging often relies on implicit config file paths such as `etc/mysql-config.yaml`. The package format should make those expectations explicit so hydration can validate them before apply.

## Hooks

Hooks are allowed, but they should be explicit and phase-bound.

Initial hook phases:

- `bootstrap`
- `configure`
- `install`
- `upgrade`
- `remove`
- `healthcheck`

Initial execution targets:

- `allNodes`
- `firstMaster`
- `cluster`

Rules:

- Hooks must reference a relative path inside the package.
- Hooks should be used sparingly and only when declarative content is not sufficient.
- Hydration and reconcile should be able to see hook intent from the manifest before execution.

## Compatibility Contract

Each package may declare compatibility constraints:

- supported Kubernetes versions
- supported Sealos versions
- supported platforms such as `linux/amd64` and `linux/arm64`

These constraints should be checked before a package is selected for application, not discovered after apply fails.

## Dependency Contract

Dependencies are declared at the package level and resolved by the BOM and reconcile layers.

Initial rules:

- dependencies are named references, not implicit path ordering
- dependencies must be unique
- self-dependency is invalid
- reconcile should topologically sort components before apply

## Class-Specific Constraints

The initial format should enforce a few important rules:

- `rootfs` packages must contain at least one `rootfs` content entry
- `patch` packages must not contain `rootfs` content
- `application` packages must not contain `rootfs` content

These constraints align with the repo's current behavior and prevent obviously invalid packages.

## Hydration Contract

Hydration consumes:

- the immutable package artifact
- the declared package inputs
- the local repo data for the target cluster

Hydration produces:

- a deterministic rendered desired-state bundle
- a stable content digest used by applied-revision tracking

The package format should make hydration deterministic by declaring payload paths and expected input surfaces explicitly.

The first renderer now materializes that bundle as:

- `bundle.yaml`
- `components/<component>/package.yaml`
- `components/<component>/files/...`

The top-level `bundle.yaml` records the BOM revision, rendered components, and per-step bundle paths so later apply logic can consume a stable, filesystem-backed desired-state artifact.

## Migration Strategy

The package format should not force an immediate rewrite of all existing images.

Recommended migration path:

1. Accept legacy images that only expose the current label-based contract.
2. Add `package.yaml` to new component artifacts first.
3. Add a compatibility layer that can infer a basic package manifest from legacy images when needed.
4. Move BOM-managed components to the explicit package format over time.

## Initial Go Schema

The initial schema lives in [pkg/distribution/packageformat/types.go](/home/allosaurus/Workspace/sealos-lifecycle/pkg/distribution/packageformat/types.go:15).

It covers:

- package class
- content descriptors
- declared inputs
- compatibility
- dependencies
- hooks
- validation for relative paths and class-specific constraints

This schema is intentionally narrow. It is enough to unblock BOM integration, hydration planning, and early package validation.

The first loader implementation now lives alongside the schema and supports:

- loading `package.yaml` from a directory
- loading `package.yaml` from a mounted OCI artifact through a small image-mounter interface
- validating that declared content and hook paths exist in the package payload

The initial BOM integration now also supports resolving component package manifests from artifact references after a BOM has been validated.

## Recommendation

Yes, component packaging should be designed now. It sits directly on the critical path between BOM metadata and real reconcile behavior.

The next implementation step should be:

1. keep this package manifest schema small
2. define how `package.yaml` is loaded from an OCI artifact
3. add one example packaged component fixture
4. decide whether the hydration MVP supports raw manifests only, or raw manifests plus charts

## Open Questions

- Should the MVP hydration path support both Helm charts and raw manifests, or only raw manifests?
- Should hook scripts be modeled only as referenced files, or should inline command forms also be allowed later?
- Should package dependencies support version ranges in the manifest, or should BOM selection remain the only version resolver?
- How much of the legacy image metadata can be inferred safely when `package.yaml` is absent?
