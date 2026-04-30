# Design Proposal: Sealos Component Package Format

## Status

Draft

## Summary

This document defines how a Sealos component should be packaged as an OCI artifact so that it can participate in BOM resolution, local hydration, reconciliation, and promotion.

The package format is intentionally based on the repository's current image model rather than replacing it. Today the code already distinguishes `rootfs`, `patch`, and `application` images, merges image metadata, and applies them with different execution semantics. The package format makes that contract explicit and machine-readable.

## Related Documents

- For the repo-verified OCI packaging milestone built on top of this contract, see [sealos-oci-component-packaging-milestone-plan.md](./sealos-oci-component-packaging-milestone-plan.md).
- For the minimal prepared-host Kubernetes PoC that consumes these packages, see [sealos-minimal-k8s-package-poc-plan.md](./sealos-minimal-k8s-package-poc-plan.md).

## Why This Design Is Needed

The multi-cluster design now has BOM and applied-revision schemas, but it still lacks a clear answer to one operational question: what exactly is a component artifact?

Without a formal package contract:

- BOM entries only point at digests, not at a well-defined payload structure.
- Hydration logic has no reliable way to discover manifests, charts, files, or hooks.
- Operators cannot reason about compatibility, inputs, or dependencies before apply time.
- Promotion is difficult because the contents and intent of a packaged component are implicit.

## Existing Repository Model

The current repository already contains the seeds of a package model:

- `rootfs`, `patch`, and `application` image classes in [pkg/types/v1beta1/cluster.go](../pkg/types/v1beta1/cluster.go)
- image metadata merging in [pkg/image/merge.go](../pkg/image/merge.go)
- class-specific execution behavior in [pkg/guest/guest.go](../pkg/guest/guest.go)
- config file injection through Clusterfile `Config` objects in [pkg/config/config.go](../pkg/config/config.go)

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

### Packager Checklist: One Package Or Many

The default packaging choice should be one package.

Do not split a component into multiple packages unless there is a clear
operational lifecycle boundary. Package count is not an optimization target by
itself. Every extra package adds more BOM edges, upgrade ordering, ownership
rules, and test combinations.

Use this decision order when packaging a component:

1. Start with one package for the whole component.
2. If a value is expected to vary per cluster, model it as a declared `input`,
   not as a new package.
3. If a change is a reusable platform opinion shared by many clusters, model it
   as shared package content or a `patch` package, not as ad-hoc local data.
4. Split into a separate package only if the candidate slice has a genuinely
   independent lifecycle.

### Quick Decision Table

| Question | If Yes | If No |
| --- | --- | --- |
| Does this vary mainly by cluster installation facts such as CIDR, endpoint, mirror, secret, or node inventory? | Keep one package and expose a declared `input`. | Go to the next question. |
| Is this a reusable SRE or platform opinion that several clusters should share? | Keep it `global` as package content or move it into a `patch` package. | Go to the next question. |
| Does it have its own release cadence, owner, upgrade window, or rollback need? | Consider a separate package. | Keep it in the existing package. |
| Would operators reasonably want to manage or replace it independently? | Consider a separate package. | Keep it in the existing package. |
| Would splitting reduce blast radius without creating tightly coupled version choreography? | A separate package may be justified. | Keep it in the existing package. |

### Independent Lifecycle Signals

The strongest reasons to split into a separate package are:

- independent version stream
- independent operational owner or team
- independent upgrade and rollback workflow
- independent dependency graph
- independent failure domain or blast radius
- realistic operator need to swap or omit it

If most of those signals are absent, do not split.

### Strong Reasons Not To Split

Keep content in one package when:

- the parts are tightly version-coupled
- they are almost always upgraded together
- one part is only a small implementation detail of another
- splitting would introduce ordering complexity without real operator value
- the only difference is that some fields vary per cluster

### Output Choices

After walking the checklist, there are only four normal outcomes:

| Situation | Packaging Outcome |
| --- | --- |
| Per-cluster installation fact | Declared `input` |
| Shared reusable policy or hardening layer | `patch` package or shared package content |
| Genuine independent subsystem | Separate package |
| Everything else | Keep it in the current package |

### Examples In This Design

| Candidate | Recommended Outcome | Why |
| --- | --- | --- |
| cluster name, CIDR, advertise address | `input` | These are installation facts that vary per cluster. |
| image mirror overrides | `input` | These are environment bindings, not a new lifecycle boundary. |
| audit policy | `patch` package or shared package content | This is a reusable platform opinion, not per-cluster installation data. |
| admission controller defaults | `patch` package | These should travel as one shared policy layer. |
| container runtime | Separate package | It has a distinct host lifecycle boundary and may be managed independently from Kubernetes bootstrap. |
| CNI | Separate package | It has its own release cadence, owner, and operational blast radius. |
| `kubelet` versus `kube-apiserver` as separate first-class packages | Do not split | They are tightly coupled and usually move together. |

### Global Vs Local At Package Scope

A component package is global by default.

That means everything physically stored in the OCI artifact is part of the
shared, immutable baseline unless the package manifest explicitly models it as a
local binding surface.

| Package Element | Default Scope | Why |
| --- | --- | --- |
| `metadata`, `spec.component`, `spec.version`, `spec.class` | `global` | Package identity and lifecycle boundary must be the same for every cluster that consumes the artifact digest. |
| `spec.compatibility`, `spec.dependencies` | `global` | Selection rules and dependency intent are part of the reusable package contract, not per-cluster data. |
| `spec.contents` entries and the bytes stored under `rootfs/`, `manifests/`, `charts/`, `files/`, `hooks/` | `global` | These are the payload being distributed and must remain immutable for a digest-pinned artifact. |
| `spec.hooks` and the referenced hook scripts | `global` | Execution logic is package behavior, not cluster-local state. |
| `spec.inputs` declarations | `global` | The package declares which surfaces are allowed to vary, but the declaration itself is part of the shared contract. |
| Actual values bound to `spec.inputs` during hydration | `local` | These values come from the target cluster's local repo or secret path and may legitimately vary per cluster. |
| Local overlays against package-defined extension points | `local` | These are cluster-specific adaptations applied after artifact selection and must be validated by ownership rules. |
| Secrets, node inventory, runtime-discovered state | `local` | These are environment-bound and must not be baked into the shared artifact. |

The practical rule is:

- if it must be digest-pinned and reused across clusters, it is `global`
- if it is supplied after artifact selection and is expected to vary by cluster,
  it is `local`
- if it represents a reusable platform opinion that should travel across many
  clusters, it should become package content or a separate patch package, not an
  ad-hoc local input

For example, in a `kubernetes-rootfs` package:

- `global`: kubelet and kubeadm binaries, systemd units, bootstrap hooks,
  healthcheck manifests, default kubeadm config structure, baseline audit
  policy, and dependency declarations
- `local`: cluster name, advertise address, pod CIDR, service CIDR, image
  mirror overrides, kubelet extra environment values, private certificates, and
  environment-specific registry settings

This is why audit policy belongs more naturally in a shared package or patch
package than in a cluster-local input path. It is usually a reusable platform
opinion, not an installation-specific fact like CIDR or endpoint address.

### Initial Per-Package Global/Local Matrix

The following matrix applies the ownership rule to the initial package set used
throughout this design.

`Local binding surface` means the cluster-specific data that may be bound at
hydrate time. It does not mean the package can be overridden arbitrarily.

| Package | Global Baseline In The Package | Local Binding Surface | Shared But Not Local |
| --- | --- | --- | --- |
| `containerd-runtime` | `containerd`, `ctr`, `runc`, packaged runtime config defaults, service units or drop-ins, preflight/bootstrap/healthcheck hooks | registry mirror endpoints, sandbox image location, private CA or auth references, proxy settings, environment-specific runtime path tweaks if unavoidable | common runtime hardening, default snapshotter choice, standard plugin enablement, reusable registry policy |
| `kubernetes-rootfs` | `kubeadm`, `kubelet`, `kubectl`, systemd units, sysctl profile, bootstrap manifests, bootstrap and healthcheck hooks, baseline kubeadm config structure | cluster name, control-plane endpoint, advertise address, node IP inventory, pod/service CIDR, image repository overrides, private certificates or CA refs, kubelet extra env | audit policy, admission controller config, reusable API server flags, static-pod patches, common kubelet config overlays |
| `cilium-cni` | Cilium manifest or chart revision, baseline RBAC and DaemonSet resources, default feature profile, healthcheck logic | cluster-specific IPAM values, native-routing or pod-CIDR integration, MTU overrides, environment-specific mirror refs, approved nodeSelector or toleration overrides | whether Cilium is the chosen CNI, shared `kubeProxyReplacement` policy, shared Hubble profile, standard operator sizing policy |
| `kubernetes-control-plane-patch` (later) | reusable control-plane hardening overlays, audit policy, admission config, shared policy manifests, reusable static-pod patches | cluster-specific endpoint refs, certificate refs, narrowly scoped environment-specific exemptions | the hardening profile itself, standard platform security defaults |

The decision pattern should be consistent across packages:

- versioned binaries, manifests, hook logic, and default policy belong to
  `global`
- installation facts, private endpoints, secrets, and topology-specific values
  belong to `local`
- reusable SRE or platform opinions that many clusters should share belong to
  `global`, often as package content or a separate patch package

### About Packaged Default Files Used By Inputs

Some current package examples use the same path both as packaged content and as
an input declaration, such as:

- `files/etc/containerd/config.toml`
- `files/etc/kubernetes/kubeadm.yaml`
- `files/values/basic.yaml`

Those files should be interpreted as package-owned defaults, schemas, or merge
bases.

They do not become `local` just because the manifest declares an input at that
path. What is `local` is the concrete value bound during hydration, not the
baseline file carried by the artifact.

### Promoting Repeated Local Binding Into Official Inputs

Repeated use of local binding is strong product feedback, but it should not be
treated as an automatic rule that every high-frequency override becomes a new
`input`.

The right interpretation is:

- repeated local binding means the package boundary probably needs refinement
- the refinement may be a new `input`
- but it may also be a better baseline default, a `patch` package, or a
  separate package

Use this decision order:

1. If many clusters adjust the same dimension, first ask whether the dimension
   is truly expected to differ per cluster.
2. If the answer is yes, promote it into an explicit `input` so the package
   contract names it, validates it, and documents it.
3. If the answer is no because clusters are converging on the same value, move
   that value into the shared baseline or a `patch` package instead.
4. If the overrides are starting to express a reusable capability layer with its
   own lifecycle, stop expanding the input surface and consider a separate
   package boundary.

### Promotion Checklist

Promote repeated local binding into an official `input` only when most of these
statements are true:

- the value is expected to vary by cluster, not just by release history
- the variation does not change the component's core ownership boundary
- the input can be named clearly in the package contract
- the input can be validated structurally or semantically
- the allowed value range is small enough to keep package behavior predictable
- exposing the input does not turn the package into a generic pass-through for
  arbitrary internal settings

If several of those statements are false, do not automatically expand the input
surface.

### Anti-Pattern Checks

Repeated local binding should usually **not** become a new `input` when:

- clusters are converging on the same final value
- the override is really a shared SRE or platform policy
- the override changes too much of the package's internal behavior
- the value cannot be validated cleanly
- the new input would become a catch-all escape hatch for implementation detail

In those cases, the better answer is usually one of:

- improve the baseline default
- add shared package content
- introduce a `patch` package
- split a genuinely independent subsystem into its own package

### Outcome Matrix

| Observed Pattern Across Clusters | Recommended Outcome |
| --- | --- |
| Same dimension changes often, but each cluster needs its own value | Promote to an explicit `input` |
| Many clusters independently converge on the same setting | Move it into the shared baseline or a `patch` package |
| A reusable policy bundle appears across clusters | Model it as shared content or a `patch` package |
| Overrides start to describe a subsystem with its own lifecycle | Consider a separate package |

### Cilium Examples

For `cilium-cni`, these are good candidates to promote into or keep as official
inputs:

- cluster-specific IPAM values
- native routing or pod-CIDR integration values
- MTU
- environment-specific mirror settings
- narrowly scoped placement or toleration overrides

These are better candidates for shared baseline or patch-level treatment if
many clusters converge on them:

- whether `hubble` is enabled by default
- whether `kubeProxyReplacement` is part of the platform standard
- a standard operator sizing profile
- a standard observability or security profile around Cilium

The goal is not to maximize the number of inputs. The goal is to make the input
surface explicit, stable, and intentionally small.

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

A fuller production-style Kubernetes rootfs example now lives at [pkg/distribution/packageformat/testdata/kubernetes-production-rootfs/package.yaml](../pkg/distribution/packageformat/testdata/kubernetes-production-rootfs/package.yaml).

A matching BOM example for that package now lives at [pkg/distribution/bom/testdata/default-platform-production-bom.yaml](../pkg/distribution/bom/testdata/default-platform-production-bom.yaml).

A workload-oriented example with a database boundary and local Secret handling
now lives at [sealos-grafana-kubeblocks-example.md](./sealos-grafana-kubeblocks-example.md).

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

Important boundary rule:

- `spec.inputs` declares a `global` contract surface
- the concrete value bound to an input is `local`

If an input path points at a file inside the package, that packaged file should
be interpreted as a default, schema anchor, or merge base. The package still
owns the baseline file. The cluster only owns the value bound at hydration time.

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

The initial schema lives in [pkg/distribution/packageformat/types.go](../pkg/distribution/packageformat/types.go).

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
