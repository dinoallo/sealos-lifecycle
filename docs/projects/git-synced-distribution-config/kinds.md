# Distribution Document Kind Reference

## Status

Draft

## Summary

This document defines the catalog of `distribution.sealos.io/v1alpha1` document
kinds used by the package-based distribution model.

Not every kind in this document is a Kubernetes CRD. Most kinds are Git or local
filesystem documents. Only kinds that need Kubernetes API reconciliation should
be installed as CRDs.

## Kind Classes

| Class | Meaning |
| --- | --- |
| Kubernetes CRD | Installed into the Kubernetes API server and reconciled by a controller. |
| Repository source document | Reviewed source of truth stored in `distribution-config` or `cluster-config`. |
| Local source document | Cluster-local source of truth stored near a local repo or cluster workspace. |
| Generated document | Deterministic output from render, apply, smoke, or validation workflows. |
| Evidence document | Reviewable proof used by promotion or policy gates. |
| Proposed document | Planned schema that is not implemented as a first-class loader yet. |
| Illustrative document | Useful model in docs, but not a schema commitment yet. |

## Common Envelope

All distribution document kinds should use the same top-level shape unless a
kind-specific document says otherwise:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: <Kind>
metadata:
  name: <name>
spec: {}
status: {}
```

Common rules:

- `apiVersion` must be `distribution.sealos.io/v1alpha1`.
- `kind` must be one of the kinds listed in this reference.
- `metadata.name` is required for named documents.
- `metadata.labels` may be used for ownership, release, and automation hints.
- `spec` stores desired state, source facts, policy, or evidence inputs.
- `status` is only for Kubernetes CRDs or generated runtime state documents.
- Source documents must not contain plaintext secret values.
- Repository-relative paths must not be absolute and must not use `..` to escape
  the owning repository root.
- Generated documents must carry enough provenance to identify the source
  revision, BOM revision, local repo revision, and digest inputs that produced
  them.

## Catalog

| Kind | Class | Owner | Normal Location | Status |
| --- | --- | --- | --- | --- |
| `ComponentPackage` | Repository source document | Package owner | `packages/<category>/<name>/<version>/package.yaml`, materialized package roots | Implemented file schema |
| `BuildClass` | Repository source document | Platform team | `classes/<class>/<version>.yaml` | Proposed |
| `BOM` | Repository source document | Platform release owner | `releases/<distribution>/<revision>/bom.yaml` | Implemented file schema |
| `ReleaseChannel` | Repository source document | Release manager | `channels/<distribution>/<channel>.yaml` | Proposed preferred name, code accepts it |
| `DistributionChannel` | Repository source document | Release manager | Existing local channel files | Implemented compatibility name |
| `DistributionHealthProof` | Evidence document | Release automation | CI artifacts or promotion evidence paths | Implemented file schema |
| `ClusterTarget` | Repository source document | Cluster owner | `cluster-config/clusters/<scope>/<cluster>/target.yaml` | Proposed |
| `ComponentInput` | Repository source document | Cluster owner | `cluster-config/clusters/<scope>/<cluster>/inputs/*.yaml` | Proposed |
| `LocalPatchPolicy` | Local source document | Platform or cluster owner | package source, BOM reference, or local repo `policy/local-patch-policy.yaml` | Implemented file schema |
| `LocalPatchPolicyGateApproval` | Evidence document | Policy reviewer | local repo policy evidence or CI artifact | Implemented file schema |
| `HydratedBundle` | Generated document | Agent or CI | render workspace `bundle.yaml` | Implemented generated schema |
| `AppliedRevision` | Generated runtime document | Agent | cluster runtime state store `applied-revision.yaml` | Implemented runtime schema |
| `AppliedInventory` | Generated runtime document | Agent | rendered or applied inventory output | Proposed |
| `PackageAcceptanceReport` | Evidence document | Package test automation | smoke or acceptance artifact | Implemented file schema |
| `DistributionTarget` | Kubernetes CRD | Cluster operator | Kubernetes API, namespaced | Implemented CRD |
| `DistributionRolloutPolicy` | Kubernetes CRD | Cluster operator | Kubernetes API, namespaced | Implemented CRD |
| `LocalRepo` | Local source document | Cluster owner | local repo metadata | Illustrative, not implemented |
| `LocalRepoRevision` | Local source document | Cluster owner | local repo revision metadata | Illustrative, not implemented |

## Source Document Kinds

### `ComponentPackage`

Purpose: defines one package revision and its render/apply contract.

Normal location:

```text
packages/<category>/<name>/<version>/package.yaml
```

Minimum contract:

- declares package component, version, and package class
- declares package contents such as rootfs, files, manifests, charts, or hooks
- declares supported input surfaces
- declares package dependencies when required
- all referenced paths are relative to the package root

Must not contain:

- cluster-local input values
- plaintext secret values
- generated render output
- downloaded artifact cache data

### `BuildClass`

Purpose: defines the reusable build workflow contract used to turn package
source facts into a materialized package payload.

Normal location:

```text
classes/<class>/<version>.yaml
```

Minimum contract:

- output kind, such as package root, OCI artifact, OCI layout, or local registry
  image
- accepted package classes
- supported platforms
- source include and exclude rules used by `source.digest`
- builder implementation or command family
- required deterministic non-secret build options
- required provenance fields

Rules:

- build class versions are immutable
- a change that can affect package bytes, selected source files, or output
  metadata requires a new class version
- builders must not read `cluster-config`, live cluster state, undeclared host
  files, or secret values

### `BOM`

Purpose: defines one immutable distribution release revision.

Normal location:

```text
releases/<distribution>/<revision>/bom.yaml
```

Minimum contract:

- `spec.revision`
- selected packages by full identity: `category`, `name`, and `version`
- source facts and source digest for buildable packages
- build class and build contract for buildable packages
- artifact image and digest when a prebuilt artifact is required or available
- dependency references when a package depends on another package

Must not contain:

- channel membership
- cluster-local inputs
- cluster-local patches
- secret values
- generated render output

### `ReleaseChannel`

Purpose: points one channel to an approved immutable BOM revision.

Normal location:

```text
channels/<distribution>/<channel>.yaml
```

Minimum contract:

- distribution or line name
- channel name
- target BOM revision
- path to the target BOM
- optional promotion history and health proof reference

Rules:

- channel movement is release intent and should be a small reviewable change
- the referenced BOM revision must match `spec.targetRevision`
- new repository layouts should prefer `ReleaseChannel`

### `DistributionChannel`

Purpose: existing implemented channel document name used by current guides and
commands.

Rules:

- treated as a compatibility name for channel pointer documents
- new git-synced repository layouts should migrate toward `ReleaseChannel`
- resolvers may accept both names during the transition

### `DistributionHealthProof`

Purpose: records health evidence for promotion into a channel.

Minimum contract:

- distribution line
- target revision
- `passed`
- collected time when available
- signal list with names, pass/fail state, and messages

Rules:

- does not move a channel by itself
- may be referenced by channel promotion history
- must not copy secret payloads, kubeconfigs, tokens, or host-private files

## Cluster Source Kinds

### `ClusterTarget`

Purpose: stable cluster entrypoint in `cluster-config`.

Normal location:

```text
cluster-config/clusters/<scope>/<cluster>/target.yaml
```

Minimum contract:

- selected distribution
- selected channel or revision
- selected profile
- delivery mode
- configured distribution repository reference
- input, patch, and secret-reference paths relative to the cluster root

Rules:

- must not duplicate BOM package contents
- must not embed repository credentials
- local paths must not escape the cluster root
- delivery mode must not change package graph, input order, or patch order

### `ComponentInput`

Purpose: binds declared package input surfaces to cluster-local non-secret
values.

Normal location:

```text
cluster-config/clusters/<scope>/<cluster>/inputs/*.yaml
```

Minimum contract:

- target component or package identity
- values for declared input names

Rules:

- must bind only inputs declared by selected `ComponentPackage` documents
- must not contain obvious secret material
- secret-shaped inputs should be bound through secret references or runtime
  injection points instead

### `LocalPatchPolicy`

Purpose: defines what cluster-local patches may change.

Normal locations:

```text
local-repo/policy/local-patch-policy.yaml
packages/<category>/<name>/<version>/policy/local-patch-policy.yaml
```

Minimum contract:

- scope, currently `clusterLocal`
- forbidden exact paths
- forbidden metadata keys
- forbidden container fields
- kind rules with allowed prefixes

Rules:

- governs cluster-local override surfaces only
- does not make package-owned fields local
- package-side and BOM-side policy sources select a local patch policy; they do
  not introduce a separate package/BOM policy layer

### `LocalPatchPolicyGateApproval`

Purpose: records human approval for local patch policy changes that need a
review gate.

Minimum contract:

- owner
- approver
- change reference
- expiration
- old and new policy references
- expected impact when required by the gate

Rules:

- is evidence for a policy change, not a runtime policy by itself
- should be validated against the actual policy diff it approves

### `LocalRepo`

Purpose: future metadata document that identifies a cluster-local repo.

Status: illustrative only. Current docs use it to describe the model, but the
schema is not implemented.

### `LocalRepoRevision`

Purpose: future metadata document that records the current cluster-local input
and patch revision.

Status: illustrative only. Current docs use it to describe the model, but the
schema is not implemented.

## Generated And Runtime Kinds

### `HydratedBundle`

Purpose: generated desired-state bundle produced from BOM, package payloads,
profile defaults, cluster inputs, and local patches.

Normal location:

```text
<agent-or-ci-workspace>/bundle.yaml
```

Rules:

- generated output, not the primary source of truth
- must record render provenance
- should include BOM revision, channel, local repo revision, package source
  digests, local patch policy source, and tracked resources
- may be retained as audit evidence, but should not replace BOM or
  cluster-config as source input

### `AppliedRevision`

Purpose: records the cluster's current rendered or applied revision and observed
state.

Normal location:

```text
<cluster-runtime-root>/distribution/applied-revision.yaml
```

Rules:

- mutable cluster-local runtime state
- records BOM reference, desired state digest, local repo revision, local patch
  revision, observed state, and conditions
- should not be edited as release intent

### `AppliedInventory`

Purpose: future generated inventory document that explains the concrete objects
and host paths expected from one rendered revision.

Status: proposed. It extends the observability of `AppliedRevision`, but is not
implemented as a first-class persisted schema yet.

### `PackageAcceptanceReport`

Purpose: records package lifecycle test evidence, such as smoke, apply, or
revert results.

Rules:

- generated by package acceptance automation
- consumed by `DistributionHealthProof` generation
- must include enough BOM, package, local repo, and desired-state evidence to
  prove what was tested
- must not include secret payloads

## Kubernetes CRD Kinds

### `DistributionTarget`

Purpose: runtime Kubernetes API object that asks `sealos-agent --controller` to
reconcile one target.

Status: implemented CRD.

Normal location:

```text
deploy/distribution-controller/base/crd.yaml
```

Minimum contract:

- exactly one of `spec.bomPath` or `spec.distributionChannelPath`
- optional local repo path
- optional package source overrides
- optional cache, kubeconfig, and host-root paths visible to the controller pod
- optional rollout policy reference

Rules:

- this is a runtime controller object, not the shared distribution source of
  truth
- it may be generated from `ClusterTarget` later, but the two kinds have
  different ownership boundaries

### `DistributionRolloutPolicy`

Purpose: runtime Kubernetes API object that defines durable rollout behavior for
referencing `DistributionTarget` objects.

Status: implemented CRD.

Minimum contract:

- rollout strategy
- batch size
- optional canary, pause, health gate, and failure behavior

Rules:

- applies only to rollout behavior covered by the controller and rendered-bundle
  executor
- does not replace package-level safety design for every multi-node workflow

## Naming And Migration Rules

- Do not call every distribution document a CRD. Use "document kind" unless the
  kind has a Kubernetes `CustomResourceDefinition`.
- Use `ReleaseChannel` for new git-synced channel pointer documents.
- Keep accepting `DistributionChannel` while current commands and guides still
  use it.
- Keep `ClusterTarget` and `DistributionTarget` separate: `ClusterTarget` is
  cluster-owner Git intent; `DistributionTarget` is a Kubernetes runtime object.
- Keep generated kinds such as `HydratedBundle`, `AppliedRevision`, and
  `PackageAcceptanceReport` out of source repositories unless an explicit audit
  workflow requires them.

## Recommended Schema Work Order

1. Stabilize source kinds: `ComponentPackage`, `BuildClass`, `BOM`,
   `ReleaseChannel`.
2. Stabilize cluster-local source kinds: `ClusterTarget`, `ComponentInput`,
   `LocalPatchPolicy`.
3. Stabilize evidence kinds: `LocalPatchPolicyGateApproval`,
   `DistributionHealthProof`, `PackageAcceptanceReport`.
4. Keep the implemented Kubernetes CRDs focused on runtime reconciliation:
   `DistributionTarget` and `DistributionRolloutPolicy`.
5. Treat `LocalRepo`, `LocalRepoRevision`, and `AppliedInventory` as future
   schema work until implementation depends on them.
