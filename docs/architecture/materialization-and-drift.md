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
  [Distribution and config sync](../architecture/distribution-and-config-sync.md)
- Reconcile, ownership, and drift semantics:
  [Reconcile and ownership](../architecture/reconcile-and-ownership.md)
- Package contract and content classes:
  [Package format](../architecture/package-format.md)
- Local repo and secret-handling model:
  [Local repo and secret](../guides/local-repo-and-secret.md)
- Local patch policy source and scope:
  [Local patch policy](../architecture/local-patch-policy.md)
- Operator action quick reference:
  [Sync operator actions](../reference/sync-operator-actions.md)
- Current applied revision schema:
  [pkg/distribution/state/types.go](../../pkg/distribution/state/types.go)
- Current materialization path:
  [pkg/distribution/reconcile/materialize.go](../../pkg/distribution/reconcile/materialize.go)
- Current apply behavior:
  [pkg/distribution/reconcile/apply.go](../../pkg/distribution/reconcile/apply.go)

## Why This Sub-Design Exists

The ownership document defines who is allowed to define desired state. The
package-format document defines what a package may contain. That still leaves a
practical gap:

- What exact live object or file should be tracked for each content type?
- Which comparison rule should apply to that object or file?
- How should Sealos track generated outputs that are not stored directly in the
  package artifact?
- Which live objects should be observed, and which should be treated as
  `globalBaseline`-owned desired state?

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
| `observeOnly` | runtime-generated objects such as operator-created connection Secrets | Do not treat the object itself as `globalBaseline`-owned desired state. Observe for health or reference resolution only. |

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

In the current single-node MVP, this patch shape is intentionally narrow:

- `patches/<component>/**/*.yaml`
- each YAML document is a partial Kubernetes object overlay
- the target object is identified by `apiVersion`, `kind`, `metadata.name`, and
  usually `metadata.namespace`
- render merges the patch into the matching package manifest object and also
  keeps the patch document in the bundle as a `localPatch` fragment for
  ownership-aware compare
- patch files are not applied directly as standalone resources
- the current validator only permits a narrow set of local patch paths, notably
  `ConfigMap.data` / `binaryData`, workload placement fields, selected
  secret-name references, and ingress or service exposure fields

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

- [scripts/poc/minimal-single-node/packages/kubernetes/package.yaml](../../scripts/poc/minimal-single-node/packages/kubernetes/package.yaml)
- [scripts/poc/minimal-single-node/packages/kubernetes/hooks/bootstrap.sh](../../scripts/poc/minimal-single-node/packages/kubernetes/hooks/bootstrap.sh)

That means files such as:

- `/etc/kubernetes/manifests/kube-apiserver.yaml`
- `/etc/kubernetes/manifests/kube-controller-manager.yaml`
- `/etc/kubernetes/manifests/kube-scheduler.yaml`

should be treated as `generatedHostPath` projections.

For such a generated host file, Sealos should track at least:

- the `kubernetes-rootfs` package revision
- the bootstrap hook identity
- the rendered digest of the hydrated `kubeadm.yaml`
- the local repo revision that supplied cluster-specific values
- the generated target path
- the last successful normalized digest of the generated static Pod manifest

Current single-node MVP note:

- packages can now declare generated host files in
  `ComponentPackage.spec.generatedOutputs.hostPaths[]`. During render, those
  declarations are copied into the hydrated bundle and become
  `spec.trackedHostPaths[]` entries with
  `projectionClass=generatedHostPath` and
  `compareStrategy=semanticGeneratedFile`.
- this is the current bundle-local inventory location for generated outputs;
  it records the target host path, component, generator tool/hook identity,
  generated object identity, and known semantic expectations such as container
  image, command, flags, and mounts.
- declaration is not limited to kubeadm. Package authors can model other
  generated Kubernetes-object host files, for example a Cilium health/status
  projection rendered by a package hook.
- the repository now tracks three known generated host files from this flow:
  `kube-apiserver.yaml`, `kube-controller-manager.yaml`, and
  `kube-scheduler.yaml` under `/etc/kubernetes/manifests/`
- it records each path as a `generatedHostPath` produced by the Kubernetes
  bootstrap hook through `kubeadm`
- current compare behavior is intentionally narrow:
  it validates semantic identity as a Kubernetes `Pod` in namespace
  `kube-system`, and also checks that the expected control-plane container
  exists
- it derives a small field-level expectation from the rendered `kubeadm.yaml`:
  the expected control-plane container image for each tracked static Pod
- it now also derives a small known-field set from the same rendered input:
  the expected command name, selected flags such as
  `--service-cluster-ip-range` and `--cluster-cidr`, and a small set of
  expected volume mounts such as `/etc/kubernetes/pki`
- the current parser also tolerates both common kubeadm shapes for
  `extraArgs`: mapping form and list-of-`name`/`value` form; it also derives
  expected mounts from `extraVolumes`
- it still does not compare the full generated manifest against a complete
  field-level desired intent model
- `sync diff` and `sync status` report this projection today; `sync revert`
  can repair a narrow subset of known kubeadm control-plane host paths when the
  rendered kubeadm input is retained, while `sync commit` still does not manage
  generated projections directly
- current CLI output also carries a generated-projection remediation hint:
  semantic field drift points operators back to the rendered `kubeadm` input,
  while parse-level failures are classified as manual-review cases
- the current hint also distinguishes who must change the source of truth:
  `changeOwner=localInput` for field drift that can be reconciled through
  cluster-local bootstrap inputs, `changeOwner=globalBaseline` for drift that
  points back to the selected BOM/package global baseline, and
  `changeOwner=manualReview` for cases where Sealos cannot safely classify the
  live projection automatically
- the current CLI payload also includes a small operator playbook:
  `nextSteps[]` gives ordered follow-up actions, and `allowedCommands[]`
  enumerates the Sealos commands that are safe to use from that state
- for generated projections, `commandGuidance[]` now adds command-level
  preconditions and an evaluated `availability`, so `sync diff/status` can say
  not only which command is relevant, but also whether it is currently blocked
  by a missing prerequisite such as a bundle digest mismatch
- generated remediation now also carries structured projection metadata:
  `projectionClass`, `generator`, `generatedKind`, `generatedName`, and an
  explicit `repairable` flag. This lets operators and automation distinguish
  modeled-but-not-repairable generated drift from the narrow generated
  projections with a known repair path.

## Current Remediation Model For Ordinary Drift

Generated projections are not the only tracked projections that now carry
operator guidance. In the current single-node MVP, `sync diff` and
`sync status` also attach a remediation block to ordinary `k8sObject` drift and
direct `hostPath` drift.

The intent is to keep `changeOwner` aligned with the ownership boundary that
must absorb the fix:

| Projection | Typical Drift Owner | Current `changeOwner` | Typical Action |
| --- | --- | --- | --- |
| package-owned `k8sObject` | selected package or BOM global baseline | `globalBaseline` | `reviewDistributionBaselineForAppliedObject` |
| local-owned `k8sObject` | local repo patch or local resource | `localOverlay` | `reviewLocalObjectOverlayAndCommitOrReapply` |
| package-owned direct `hostPath` | selected package or BOM global baseline | `globalBaseline` | `reviewDistributionBaselineForHostPath` |
| local-owned direct `hostPath` | local repo input binding | `localInput` | `reviewLocalHostInputAndCommitOrReapply` |
| generated `generatedHostPath` | local bootstrap input, global baseline, or manual review | `localInput`, `globalBaseline`, or `manualReview` | already covered above |

### Ordinary Kubernetes Objects

For ordinary `k8sObject` projections, the current CLI behavior is:

- a `global` object that is `Drifted` or `Missing` is treated as an
  `Orphan`-class problem and points back to the selected package/BOM global
  baseline
- a `local` object that is `Drifted` or `Missing` is treated as a `Dirty`
  cluster-local overlay problem and points back to the local repo
- the remediation payload includes:
  - `action`
  - `changeOwner`
  - `source`
  - optional `policyName` and `policyEligiblePaths[]` when the drifted object
    fields fall within the current default `LocalPatchPolicy`
  - `nextSteps[]`
  - `allowedCommands[]`
  - `commandGuidance[]`

Current single-node MVP guidance for ordinary objects is intentionally narrow:

- `globalBaseline` object drift allows operator-facing commands such as
  `sync diff`, `sync status`, `sync revert`, `sync render`, `sync apply`,
  `sync package build`, and `sync package push`
- when a package-owned object drifts only on fields that are already covered by
  the default `LocalPatchPolicy`, the remediation block still classifies the
  live state as `globalBaseline`, but it now surfaces `policyName` and
  `policyEligiblePaths[]` to show that the durable fix can be expressed as a
  local repo patch instead of a package/BOM fork
- `localOverlay` object drift allows `sync diff`, `sync status`,
  `sync commit`, `sync revert`, `sync render`, and `sync apply` when the live
  object still exists
- if the local-owned object is missing, the current guidance removes
  `sync commit` and points operators toward `sync revert` or local repo edits

### Direct Host Paths

For direct `hostPath` projections, the current CLI behavior follows the same
ownership split:

- a `global` direct host path points back to the selected package/BOM global
  baseline
- a `local` direct host path points back to the cluster-local input that bound
  that file
- the current single-node MVP only offers `sync commit` for local-owned direct
  host files that are backed by a declared input binding and are still present
  on disk
- missing local-owned host files are guided toward `sync revert`, not
  `sync commit`

### Command Preconditions

The remediation payload is not just a static playbook. `commandGuidance[]`
also carries evaluated command availability.

In the current single-node MVP, the main precondition is:

- `bundleMatchesRecordedDesiredStateDigest`

This means:

- `sync diff` and `sync status` can always explain the drift
- commands that would change live state or promote the current desired state,
  especially `sync revert`, `sync commit`, and `sync apply`, are marked
  `available` or `blocked` depending on whether the inspected bundle still
  matches the recorded desired-state digest for that cluster

That distinction matters because operator guidance should not suggest a live
repair command when the inspected bundle is already detached from the cluster's
recorded desired state.

## Inventory Beyond `AppliedRevision`

The current `AppliedRevision` is still useful, but it is too coarse to explain
which projected objects or files are expected and how they should be compared.

The current MVP keeps that finer-grained materialization inventory inside the
rendered bundle under `spec.trackedK8sObjects[]` and `spec.trackedHostPaths[]`.
A future controller-facing API could lift the same information into a separate
state object next to the revision snapshot.

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
- generated host-path outputs declared by package metadata, plus the known
  kubeadm-produced static Pod manifests derived from retained kubeadm input
- local repo Secret and `ExternalSecret` resources

That is enough to keep the Kubernetes bootstrap path, Cilium package flow, and
initial stateful examples conceptually coherent.

## Observation Layers In CLI Output

The CLI should not collapse every drift-related concept into one field.

At least three layers need to stay distinct:

1. Current compare result
   - What `sync diff` sees right now by comparing the rendered bundle with live
     tracked objects.
   - This is the raw compare payload, including object-by-object mismatch paths.
   - In the current CLI shape this is exposed under `sync diff.currentCompare`.

2. Persisted observed snapshot
   - A summarized snapshot that can be safely written back into
     `AppliedRevision.status.observedSummary` when the rendered bundle digest
     matches the cluster's recorded desired-state digest.
   - This is the right place for stable counters such as `dirty`, `orphan`, and
     `mixedOwnershipObject`.
   - In the current CLI shape this is exposed as
     `sync diff.persistedObservedSummary` and
     `sync status.recordedObservedSummary`.

3. Recorded revision state
   - The cluster-level state stored in `AppliedRevision.status.state`.
   - This answers the coarse question: is the recorded revision currently
     `Clean`, `Dirty`, `Orphan`, or `Degraded`?
   - In the current CLI shape this is exposed as `recordedState` in
     `sync status`, and as `recordedRevision.state` in `sync diff`.

Keeping these layers separate avoids two common operator mistakes:

- mistaking the current raw compare result for a persisted observation snapshot
- mistaking a persisted observed snapshot for the full recorded revision state

The same distinction also makes temporary bundle inspection safer. If `sync diff`
or `sync status` is pointed at an ad hoc `--bundle-dir` and that bundle does not
match the cluster's recorded desired-state digest, Sealos should still return the
current compare result, but it should not silently overwrite the recorded
observed snapshot or revision state.

## Local Patch Policy Artifact

The current single-node MVP no longer treats local-patch policy as an implicit
code-only constant.

Instead, each rendered bundle now carries an explicit policy artifact:

- `bundle.spec.localPatchPolicySource`
- `bundle.spec.localPatchPolicyScope`
- `bundle.spec.localPatchPolicyName`
- `bundle.spec.localPatchPolicyPath`
- `bundle.spec.localPatchPolicyDigest`

and the policy document itself is rendered at that path, currently
`policy/local-patch-policy.yaml`.

If the local repo provides its own `policy/local-patch-policy.yaml`, that
document is copied into the bundle and becomes the source of truth for:

- local patch validation during render
- `policyEligible` mismatch annotation during compare
- local patch overlay extraction during `sync commit`

If the local repo does not provide one, render still writes an explicit default
policy artifact into the bundle so the rendered revision remains self-describing.

In other words, the current single-node MVP now makes policy ownership
explicit:

- `localPatchPolicySource: localRepo` means the cluster-local repo defined it
- `localPatchPolicySource: bom` means the selected BOM chose the policy
  artifact through `spec.localPatchPolicy`
- `localPatchPolicySource: package` means exactly one selected component
  package chose the policy artifact through `spec.localPatchPolicy`
- `localPatchPolicySource: builtInDefault` means Sealos rendered the built-in
  default policy into the bundle
- `localPatchPolicyScope: clusterLocal` means the rendered artifact governs
  cluster-local override surfaces only; package/BOM-scoped policy is still
  unsupported
- package/BOM policy sources select one effective policy artifact; package,
  BOM, and cluster-local policy layers are not merged

## Current `sync diff` And `sync status` Output Shape

The current single-node MVP already exposes these layers directly in CLI YAML.
The examples below are intentionally shortened; they show the fields that carry
the main state model, not every counter or mismatch.

### Example: `sync diff`

```yaml
clusterName: demo
bomName: minimal-single-node
revision: rev-poc-001
channel: alpha
bundlePath: /var/lib/sealos/demo/distribution/current
appliedRevisionPath: /var/lib/sealos/demo/distribution/applied-revision.yaml
localPatchPolicy:
  source: builtInDefault
  scope: clusterLocal
  name: defaultLocalPatchPolicy
currentState: Orphan
headline: state=Orphan; dirtyObjects=0; orphanObjects=1; dirtyHostPaths=0; orphanHostPaths=0; directCommitEligible=0; directRevertEligible=0; bundleMatchRequired=0; policyEligibleOrphanObjects=1
observationPersisted: true
persistedObservedSummary:
  total: 2
  matched: 1
  drifted: 1
  clean: 1
  orphan: 1
  directCommitEligible: 0
  directRevertEligible: 1
  bundleMatchRequired: 1
operatorActionSummary:
  directCommitEligible: 0
  directRevertEligible: 0
  bundleMatchRequired: 0
recordedRevision:
  desiredStateDigest: sha256:...
  localRepoRevision: sha256:...
  state: Orphan
  observedSummary:
    orphan: 1
    directCommitEligible: 0
    directRevertEligible: 1
    bundleMatchRequired: 1
policyEligibleOrphanObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: kube-system
    name: cilium-config
    operatorAction: promoteToLocalPatch
    operatorActionMetadata:
      allowsDirectCommit: false
      allowsDirectRevert: false
      requiresBundleMatch: false
    paths:
      - data.enable-hubble
    remediation:
      action: reviewDistributionBaselineForAppliedObject
      changeOwner: globalBaseline
      policyName: defaultLocalPatchPolicy
      policyEligiblePaths:
        - data.enable-hubble
currentCompare:
  summary:
    total: 2
    matched: 1
    drifted: 1
    clean: 1
    orphan: 1
  objects:
    - tracked:
        apiVersion: apps/v1
        kind: DaemonSet
        namespace: kube-system
        name: cilium
      state: Orphan
      comparison: drifted
      mismatches:
        - path: spec.template.spec.containers[name=cilium-agent].image
          reason: valueMismatch
          ownership: global
          state: Orphan
      remediation:
        action: reviewDistributionBaselineForAppliedObject
        changeOwner: globalBaseline
        allowedCommands:
          - sync diff
          - sync status
          - sync revert
          - sync render
          - sync apply
          - sync package build
          - sync package push
        commandGuidance:
          - command: sync revert
            preconditions:
              - bundleMatchesRecordedDesiredStateDigest
            availability: available
```

How to read this:

- `headline` is the shortest reusable operator summary for this compare run. It
  is intended to stay stable enough for alert titles, ticket subjects, or
  dashboard labels.
- `localPatchPolicy` is the effective ownership policy provenance for the
  inspected rendered bundle. In legacy bundles that never recorded this
  metadata, CLI output still shows the effective built-in default policy name,
  while `path` and `digest` remain empty.
- `currentCompare` is the raw compare result for this specific rendered bundle.
- `policyEligibleOrphanObjects` is a top-level shortcut for the subset of
  `currentCompare` that is still `Orphan`, but already falls within the
  default `LocalPatchPolicy`.
- `operatorAction` is the compact operator-facing action name. For this subset,
  `promoteToLocalPatch` means the drift is still globally owned today, but the
  allowed long-term fix is to capture it as a local repo patch.
- `persistedObservedSummary` is the snapshot Sealos was willing to write back
  because the inspected bundle still matched the recorded desired-state digest.
- `recordedRevision` is the cluster's recorded state object, including the last
  persisted `observedSummary`.
- those recorded summaries now include the same direct-action counts too, so
  the recorded snapshot can answer both "how much drift existed" and "how much
  of that drift was directly commit- or revert-eligible" at observation time.
- the object-level `remediation` explains both ownership routing
  (`globalBaseline`) and currently safe commands.

### Example: `sync status`

```yaml
clusterName: demo
bomName: minimal-single-node
revision: rev-poc-001
channel: alpha
bundlePath: /var/lib/sealos/demo/distribution/current
localPatchPolicy:
  source: localRepo
  scope: clusterLocal
  name: custom-local-patch-policy
  path: policy/local-patch-policy.yaml
  digest: sha256:...
desiredStateDigest: sha256:...
localRepoRevision: sha256:...
localPatchRevision: patch-rev-1
recordedState: Orphan
recordedObservedSummary:
  total: 3
  clean: 1
  dirty: 1
  orphan: 1
  mixedOwnershipObject: 1
  directCommitEligible: 1
  directRevertEligible: 2
  bundleMatchRequired: 2
currentState: Orphan
headline: state=Orphan; dirtyObjects=1; orphanObjects=2; dirtyHostPaths=1; orphanHostPaths=0; directCommitEligible=2; directRevertEligible=2; bundleMatchRequired=2; policyEligibleOrphanObjects=1
summary:
  total: 3
  clean: 1
  dirty: 1
  orphan: 1
operatorActionSummary:
  directCommitEligible: 2
  directRevertEligible: 2
  bundleMatchRequired: 2
mixedOwnershipObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: default
    name: grafana-settings
    ownerships:
      - global
      - local
dirtyObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: default
    name: grafana-settings
    operatorAction: commitOrReapplyLocalOverlay
    paths:
      - data.adminUser
    remediation:
      action: reviewLocalObjectOverlayAndCommitOrReapply
      changeOwner: localOverlay
      commandGuidance:
        - command: sync commit
          preconditions:
            - bundleMatchesRecordedDesiredStateDigest
          availability: available
orphanObjects:
  - apiVersion: apps/v1
    kind: DaemonSet
    namespace: kube-system
    name: cilium
    operatorAction: revertOrUpdateGlobalBaseline
    paths:
      - spec.template.spec.containers[name=cilium-agent].image
    remediation:
      action: reviewDistributionBaselineForAppliedObject
      changeOwner: globalBaseline
policyEligibleOrphanObjects:
  - apiVersion: v1
    kind: ConfigMap
    namespace: kube-system
    name: cilium-config
    operatorAction: promoteToLocalPatch
    paths:
      - data.enable-hubble
    remediation:
      action: reviewDistributionBaselineForAppliedObject
      changeOwner: globalBaseline
      policyName: defaultLocalPatchPolicy
      policyEligiblePaths:
        - data.enable-hubble
dirtyHostPaths:
  - path: /etc/kubernetes/kubeadm.yaml
    operatorAction: commitOrReapplyLocalInput
    operatorActionMetadata:
      allowsDirectCommit: true
      allowsDirectRevert: true
      requiresBundleMatch: true
    reasons:
      - contentMismatch
    remediation:
      action: reviewLocalHostInputAndCommitOrReapply
      changeOwner: localInput
```

How to read this:

- `summary` is the current live summary for the inspected bundle.
- `headline` is the most compressed operator-facing summary. It is stable and
  machine-friendly enough to reuse in alerts, tickets, or dashboards without
  re-parsing the full object and host-path lists.
- `recordedObservedSummary` is the last persisted summary attached to
  `AppliedRevision`.
- `localPatchPolicy` tells the operator which ownership policy artifact this
  rendered bundle actually carried. This matters because local patch
  validation, compare-side `policyEligible` annotation, and `sync commit`
  overlay extraction now all consume the same bundle-carried policy.
- that recorded snapshot now includes the same direct-action counters too, so
  it can answer both "how much drift existed" and "how much of that drift was
  directly commit- or revert-eligible" at observation time.
- `mixedOwnershipObjects` calls out objects that contain both `global` and
  `local` fragments, even if only one side drifted this time.
- `policyEligibleOrphanObjects` is a narrower subset of `orphanObjects`: it
  highlights package-owned object drift that is still currently `Orphan`, but
  whose changed paths already fall within the default `LocalPatchPolicy`.
- `operatorAction` turns that routing into a stable summary-level action name,
  such as `commitOrReapplyLocalOverlay`, `revertOrUpdateGlobalBaseline`, or
  `promoteToLocalPatch`. The same pattern also applies to host-path summaries,
  for example `commitOrReapplyLocalInput` or
  `rerenderOrUpdateGlobalBaseline`.
- `operatorActionMetadata` adds a narrow structured view on top of that action
  name: whether this action supports direct `sync commit`, whether it supports
  direct `sync revert`, and whether those direct paths depend on
  `bundleMatchesRecordedDesiredStateDigest`.
- `operatorActionSummary` is the top-level count view over the current drift
  set. It intentionally counts only the main dirty/orphan object and host-path
  lists, not the narrower `policyEligibleOrphanObjects` subset.
- the `Observed` condition message now carries the same direct-action counts in
  compact sentence form, so operators can see the commit/revert posture without
  first expanding the full structured summary.
- `dirtyObjects`, `orphanObjects`, and `dirtyHostPaths` are already grouped by
  ownership state, so the remediation block can point directly to
  `localOverlay`, `localInput`, or `globalBaseline`.

One practical operator rule follows:

- use `sync diff` when you need the full raw compare payload
- use `sync status` when you need the summarized ownership view and the current
  cluster-level recorded state side by side

## Current `operatorAction` Matrix

This sub-design only needs one stable fact here: the current single-node MVP
already compresses ownership routing into a small fixed set of
`operatorAction` values, and those values are now part of the CLI output
contract.

Treat these names as the stable surface emitted by `sync diff` / `sync status`:

- `commitOrReapplyLocalOverlay`
- `promoteToLocalPatch`
- `revertOrUpdateGlobalBaseline`
- `commitOrReapplyLocalInput`
- `updateLocalInputAndRerender`
- `rerenderOrUpdateGlobalBaseline`
- `manualReview`

The canonical matrix for meaning, direct command capability, and
bundle-match guardrails now lives in:
[Sync operator actions](../reference/sync-operator-actions.md)

For actions that modify live state or persist observed state, the current CLI
still depends on the same digest guardrails described earlier:
`bundleMatchesRecordedDesiredStateDigest` is what decides whether command
guidance becomes `available` or `blocked`.

## Open Questions

- What is the smallest ownership selector language that can express field-level
  rules for generated files and Kubernetes objects?
- Which generated outputs should get direct automated repair paths beyond the
  current kubeadm control-plane subset?
- When should the bundle-local inventory be promoted into a dedicated
  controller-facing state object?
