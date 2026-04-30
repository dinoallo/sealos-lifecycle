# Sub-Design: Materialization Tracking And Drift Detection

## Status

Draft

## Summary

This document defines how Sealos should track the outputs materialized from
shared package content and cluster-local repo content, how it should compare
those outputs with live state, including Kubernetes object state persisted in
etcd and generated host-side files produced after bootstrap.

The core rule is that drift detection should not track only source artifacts.
It must also track the concrete projections those artifacts become after
hydration, apply, and bootstrap-time generation.

## Related Documents

- Top-level architecture:
  [sealos-multi-cluster-distribution-and-config-sync-design.md](./sealos-multi-cluster-distribution-and-config-sync-design.md)
- Reconcile, ownership, and drift semantics:
  [sealos-multi-cluster-reconcile-and-ownership-model.md](./sealos-multi-cluster-reconcile-and-ownership-model.md)
- Package contract and content classes:
  [sealos-component-package-format-design.md](./sealos-component-package-format-design.md)
- Local repo and secret-handling model:
  [sealos-local-repo-and-secret-guide.md](./sealos-local-repo-and-secret-guide.md)
- Current applied revision schema:
  [pkg/distribution/state/types.go](../pkg/distribution/state/types.go)
- Current materialization path:
  [pkg/distribution/reconcile/materialize.go](../pkg/distribution/reconcile/materialize.go)
- Current apply behavior:
  [pkg/distribution/reconcile/apply.go](../pkg/distribution/reconcile/apply.go)

## Why This Sub-Design Exists

The ownership document defines who is allowed to define desired state. The
package-format document defines what a package may contain. That still leaves a
practical gap:

- What exact live object or file should be tracked for each content type?
- Which comparison rule should apply to that object or file?
- How should Sealos track generated outputs that are not stored directly in the
  package artifact?
- Which live objects should be observed, and which should be treated as
  baseline-owned desired state?

This document answers those questions without mixing them back into the package
contract or release-policy documents.

## Scope

### In Scope

- Tracking global package content after it becomes live filesystem or
  Kubernetes state
- Tracking local repo content after it becomes live filesystem or Kubernetes
  state
- Compare strategies for different materialization classes
- Handling Kubernetes API object state exposed through kube-apiserver, even
  when that state is internally persisted in etcd
- Handling generated files such as kubeadm-produced static Pod manifests
- The minimum additional inventory needed beyond the current `AppliedRevision`

### Out Of Scope

- Final CRD or CLI shapes
- Full field-manager strategy for server-side apply
- Component-specific parsers for every future application package
- Promotion policy and release-channel behavior

## Two Tracking Layers

Sealos should track state at two different layers.

### 1. Source Tracking

This answers: what inputs defined the intended state?

Examples:

- selected `BOM` revision
- selected `ComponentPackage` digests
- local repo revision hash
- input payload digests
- local patch digests

The current repository already records a coarse version of this through
`AppliedRevision`, which stores the `BOM`, one `localPatchRevision`, and the
rendered `desiredStateDigest`.

### 2. Projection Tracking

This answers: what did those inputs become in the live system?

Examples:

- `/usr/bin/kubelet` on a node
- `/etc/kubernetes/kubeadm.yaml`
- the `DaemonSet/cilium`
- the `Deployment/grafana`
- a local `Secret/grafana-admin-credentials`

Drift detection needs both layers:

- source tracking alone cannot explain which exact file or object drifted
- projection tracking alone cannot explain which revision or local input
  produced the expected state

## Materialization Classes

The current apply path already distinguishes packaged `rootfs`, `file`, and
`manifest` content. Drift detection should build on that, but use a slightly
more explicit projection model.

| Projection Class | Produced From | Live Identity | Recommended Compare Strategy | Typical Ownership |
| --- | --- | --- | --- | --- |
| `hostPath` | packaged `rootfs/` or `files/` content | absolute node path | bytewise content digest plus existence and mode | `global` or `local`, depending on source |
| `k8sObject` | packaged manifests or local repo resources | `group`, `kind`, `namespace`, `name` | normalized Kubernetes object compare | `global` or `local` |
| `generatedHostPath` | hooks or external generators driven by tracked inputs | absolute node path plus generator identity | semantic compare against generated intent | often mixed |
| `runtimeObject` | controllers, operators, or runtime side effects | `group`, `kind`, `namespace`, `name` | observe only, or policy-specific health checks | runtime-local |

Kubernetes resource state that happens to be stored in etcd is still a
`k8sObject` projection. It does not need a separate `etcdYaml` class. The
stable compare target is the normalized API object returned by
kube-apiserver, not the raw serialization inside etcd.

The most important distinction is between `hostPath` and
`generatedHostPath`:

- `hostPath` means Sealos directly wrote the file bytes
- `generatedHostPath` means Sealos tracked the inputs and the generator, but
  another tool produced the final file bytes

## Compare Strategies

The projection class determines the right comparison rule.

| Compare Strategy | Intended Use | Notes |
| --- | --- | --- |
| `bytewiseFile` | binaries, plain config files, directly written host files | Compare content hash, existence, and file mode. |
| `normalizedK8sObject` | package manifests, local repo resources, approved local Secret objects | Ignore status, `managedFields`, `resourceVersion`, `uid`, and other server-assigned metadata. Compare only ownership-relevant fields. This is the main strategy for Kubernetes resource state stored in etcd. |
| `semanticGeneratedFile` | generated host-side files such as kubeadm-produced static Pod manifests | Parse the generated object and compare only fields that belong to tracked intent. Ignore formatting, key order, and non-semantic rewrites. |
| `observeOnly` | runtime-generated objects such as operator-created connection Secrets | Do not treat the object itself as baseline-owned desired state. Observe for health or reference resolution only. |

One practical rule follows from this table:

- if Sealos wrote the bytes directly, drift detection may compare bytes
- if another system generated the bytes, drift detection should compare meaning
- if another system owns the runtime object, Sealos should not pretend it owns
  the object's full desired shape

For Kubernetes resources, Sealos should never compare “the YAML in etcd”.
Etcd is only the storage backend. The compare target is the normalized object
returned by kube-apiserver.

## How Global Content And Local Repo Content Map Into Tracking

Tracking should follow ownership boundaries rather than filesystem origin alone.

### Global Package Content

Global package content usually becomes one of these:

- a `hostPath` projection
- a `k8sObject` projection
- or an input to a later generated projection

Examples:

- `rootfs/usr/bin/kubelet` -> `hostPath`
- `files/etc/kubernetes/kubeadm.yaml` -> `hostPath`
- `manifests/cilium.yaml` -> `k8sObject`
- `hooks/bootstrap.sh` plus `kubeadm.yaml` -> generator input for
  `generatedHostPath`

### Local Repo Inputs

Local repo `inputs/` should usually be tracked as source records first, not as
live objects directly.

Examples:

- a cluster-specific `cilium-values.yaml`
- a cluster-specific `kubeadm.yaml` payload
- a local registry mirror override

These inputs influence later projections, but they are not themselves live API
objects unless another step materializes them into one.

### Local Repo Resources

Local repo `resources/` should usually become `k8sObject` projections.

Examples:

- `Secret/grafana-admin-credentials`
- `ExternalSecret/grafana-db-root`
- a local `ConfigMap`

These are live objects and should be tracked by Kubernetes identity just like
package-provided manifests, but with `local` ownership scope.

### Local Repo Patches

Local repo `patches/` should be tracked in two ways:

- as source records, by patch digest and target identity
- through the final projection they modify

The patch file itself is not the live object. The target object or file is.

## Example: Tracking Kubernetes Object State Stored In Etcd

When an operator asks about “the YAML in etcd”, the correct design object is a
Kubernetes API object such as:

- `DaemonSet/cilium`
- `Deployment/grafana`
- `Secret/grafana-admin-credentials`

These resources may be physically persisted in etcd, but Sealos should not
track raw etcd bytes. It should track the object identity and compare the
normalized API object exposed through kube-apiserver.

### What Should Be Tracked

For an etcd-backed Kubernetes object such as `DaemonSet/cilium`, Sealos should
track at least:

- the component and package revision that introduced the object
- the object identity:
  `group`, `kind`, `namespace`, `name`
- the ownership scope for the tracked fields
- the desired normalized-object digest or field inventory
- the local repo revision if local bindings or local-owned overlays contribute
  to that object
- the last successful normalized digest of the applied object shape

### What Should Not Be Assumed

Sealos should not assume:

- that etcd stores canonical YAML for the object
- that raw etcd bytes are a stable compare surface
- that the object returned by kube-apiserver will match the original manifest
  byte-for-byte
- that every field in one Kubernetes object has the same ownership scope

### Ownership Inside One API Object

An etcd-backed Kubernetes object is a good example of why object-level
ownership is not always enough.

Within one API object, different fields may have different ownership origins.

Typical examples:

- `global-owned`
  - package-owned labels and annotations
  - the main container image
  - shared command, volume, and probe structure
- `local-owned`
  - approved local overlays such as `nodeSelector`, tolerations, or secret-name
    bindings
  - cluster-specific references carried through declared inputs
- `runtime-owned`
  - `status`
  - `managedFields`
  - server-assigned metadata
  - controller-produced observations

So drift classification should work like this:

- defaulting or status-only change -> stay `Clean`
- local-owned field changed outside the local repo -> `Dirty`
- global-owned field changed directly -> `Orphan`

## Separate Example: Tracking A Generated Host File

Generated host files are a separate class from etcd-backed Kubernetes objects.
They still matter, but they should not be confused with API object state.

The Kubernetes PoC package in this repository carries:

- `files/etc/kubernetes/kubeadm.yaml`
- a bootstrap hook that runs `kubeadm init`

See:

- [scripts/poc/minimal-single-node/packages/kubernetes/package.yaml](../scripts/poc/minimal-single-node/packages/kubernetes/package.yaml)
- [scripts/poc/minimal-single-node/packages/kubernetes/hooks/bootstrap.sh](../scripts/poc/minimal-single-node/packages/kubernetes/hooks/bootstrap.sh)

That means a file such as
`/etc/kubernetes/manifests/kube-apiserver.yaml` should be treated as a
`generatedHostPath` projection.

For such a generated host file, Sealos should track at least:

- the `kubernetes-rootfs` package revision
- the bootstrap hook identity
- the rendered digest of the hydrated `kubeadm.yaml`
- the local repo revision that supplied cluster-specific values
- the generated target path
- the last successful normalized digest of the generated static Pod manifest

## Inventory Beyond `AppliedRevision`

The current `AppliedRevision` is still useful, but it is too coarse to explain
which projected objects or files are expected and how they should be compared.

Sealos should eventually keep a finer-grained materialization inventory next to
the revision snapshot.

An illustrative shape is:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: AppliedInventory
metadata:
  name: default
spec:
  bom:
    name: default-platform
    revision: rev-007
  localRepoRevision: sha256:...
  entries:
    - component: kubernetes
      ownership: global
      projectionClass: hostPath
      compareStrategy: bytewiseFile
      target:
        path: /etc/kubernetes/kubeadm.yaml
      source:
        kind: packageContent
        packageDigest: sha256:...
        bundlePath: files/etc/kubernetes/kubeadm.yaml
      desiredDigest: sha256:...
    - component: cilium
      ownership: global
      projectionClass: k8sObject
      compareStrategy: normalizedK8sObject
      target:
        group: apps
        kind: DaemonSet
        namespace: kube-system
        name: cilium
      source:
        kind: packageManifest
        packageDigest: sha256:...
        bundlePath: manifests/cilium.yaml
      desiredDigest: sha256:...
    - component: kubernetes
      ownership: mixed
      projectionClass: generatedHostPath
      compareStrategy: semanticGeneratedFile
      target:
        path: /etc/kubernetes/manifests/kube-apiserver.yaml
      source:
        kind: generated
        generator:
          component: kubernetes
          hook: bootstrap
          tool: kubeadm
        inputs:
          - sha256:...
          - sha256:...
      lastAppliedNormalizedDigest: sha256:...
```

This schema is illustrative, not a final API commitment. The important design
point is the data shape, not the exact field names.

## Proposed Drift-Detection Flow

One reasonable first-pass control loop is:

1. Materialize the bundle from `BOM + local repo`.
2. Build a projection inventory from:
   - rendered `rootfs` steps
   - rendered `file` steps
   - rendered `manifest` steps
   - local repo resources
   - declared or known generated outputs
3. Apply the desired state.
4. After successful apply, record:
   - the revision snapshot
   - the projection inventory
   - normalized digests for generated projections
5. On later reconcile runs, compare live state against the inventory using the
   entry-specific compare strategy.
6. Classify drift using both:
   - ownership scope
   - projection compare result

This keeps revision tracking and live-object tracking separate, but connected.

## MVP Boundary

The first MVP does not need a universal tracking engine for every possible
operator or application package.

It does need a clear first-pass model for:

- direct `rootfs` and `file` projections
- direct manifest-backed Kubernetes objects whose live state is stored in etcd
- a small set of known generated outputs, especially kubeadm-produced static
  Pod manifests
- local repo Secret and `ExternalSecret` resources

That is enough to keep the Kubernetes bootstrap path, Cilium package flow, and
initial stateful examples conceptually coherent.

## Open Questions

- Where should generated outputs be declared: in package metadata, in ownership
  policy, or in a separate tracking manifest?
- What is the smallest ownership selector language that can express field-level
  rules for generated files and Kubernetes objects?
- Should the fine-grained inventory live inside one new state object, or in a
  bundle-local file next to the current applied revision record?
- Which generated outputs should the first MVP support explicitly beyond
  kubeadm static Pod manifests?
