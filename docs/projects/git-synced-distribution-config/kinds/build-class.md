# Kind: BuildClass

## Status

Proposed contract. The current implementation records `build.class` in `BOM`
entries, but Sealos does not yet implement a class registry or standalone
repo-local `BuildClass` document loading.

## Class

Built-in build contract with optional repository descriptor.

## Owner

The Sealos distribution implementation owns standard build classes. The
distribution platform owner approves which classes can be used and may maintain
repo-local descriptors for custom or policy-pinned classes. Package owners
select a build class but should not redefine its semantics inside a package.

## Resolution Locations

Resolve build classes in this order:

1. Sealos built-in class registry.
2. Approved extension class implementations installed with Sealos.
3. Optional repo-local `classes/<name>/<version>.yaml` descriptors for custom,
   experimental, or policy-pinned classes backed by installed implementations.

Built-in classes such as `rootfs/v1`, `manifest-bundle/v1`, `helm-render/v1`,
and `patch-overlay/v1` do not need to be stored in every distribution
repository. A repo-local descriptor may document or constrain built-in class use,
but it does not replace the implementation shipped with Sealos.

## Purpose

`BuildClass` defines the reproducible build contract for package sources. It
lets source-first local build mode and non-local build mode share one package
model:

- Source-first mode uses the build class to build artifacts from repository
  facts.
- Non-local mode uses the same class name as provenance for artifacts that were
  already built elsewhere.

The class is the boundary between package metadata and build execution. A
package declares what it contains; a build class implementation declares how
that source shape is built.

Package-specific build details still belong to `ComponentPackage.spec.build`.
For example, the list of Kubernetes binaries that must be staged into
`rootfs/usr/bin/` is package source metadata, not reusable class semantics.

## Optional Descriptor Envelope

Built-in classes are referenced by identity and do not require this file. When a
repository carries a descriptor for a custom, experimental, or policy-pinned
class, it should use this envelope:

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs
spec:
  version: v1
```

The canonical build class identity is:

```text
<metadata.name>/<spec.version>
```

For example, the descriptor above is referenced as `rootfs/v1` from
`ComponentPackage.spec.build.class` or `BOM.spec.packages[*].build.class`.

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `version` | Yes | Build class contract version, such as `v1`. |
| `driver` | Yes | Logical build driver, such as `copy-rootfs`, `copy-manifest`, `helm`, or `patch`. |
| `output` | Yes | Artifact kind produced by the build, such as `ociImage`, `filesystem`, `chart`, or `manifestBundle`. |
| `packageClasses` | Yes | `ComponentPackage.spec.class` values accepted by this build class. |
| `platforms` | No | Supported target platforms. Empty means platform-independent. |
| `source` | No | Include and exclude rules for source files. |
| `parameters` | No | Declared non-secret build options and defaults. |
| `provenance` | No | Required provenance fields that must be written into the resulting artifact metadata. |

## Validation Rules

- The class identity `metadata.name/spec.version` must resolve to one approved
  implementation: a Sealos built-in class, an approved extension, or a
  repo-local descriptor that points to an installed custom extension.
- Unknown classes fail closed.
- `metadata.name` and `spec.version` must be set.
- `driver` and `output` must be set.
- Every value in `packageClasses` must be a supported `ComponentPackage` class.
- `parameters` must be non-secret. Secrets must be provided by the runtime or
  CI environment and referenced only by name.
- Build classes should be immutable after adoption. Changing class behavior
  requires a new class name or versioned class name.
- Repo-local descriptors must not enable arbitrary package-specific behavior
  unless that behavior is represented by an approved extension implementation.

## Build Inputs

The package build workflow resolves these values before invoking a build class:

- package identity: `category`, `name`, `version`
- source path and digest
- `build.class`
- target platform
- build profile
- declared build options
- package-local build inputs and staging rules from
  `ComponentPackage.spec.build`

The build class implementation must not read undeclared host paths, cluster
configuration, runtime state, or secret contents.

Before invoking the class, the build workflow should load the package-local
contract from the selected `ComponentPackage`. The class provides the driver
semantics; the package-local contract provides per-package build facts.

## Build Output

A build class selected by `BOM.spec.packages[*].build.class` must produce a
materialized package payload, not a new document kind. The payload may be stored
as an OCI image, filesystem directory, OCI layout, or another supported
transport, but its root must be loadable as a package root.

The materialized package root must contain:

- `package.yaml`
- every content path referenced by `package.yaml`
- every hook path referenced by `package.yaml`
- optional local patch policy files referenced by `package.yaml`

The `package.yaml` in the output must validate as `ComponentPackage`. The build
may normalize paths or add build provenance, but it must preserve the component
and version selected by the BOM:

```text
BOM package.name == output ComponentPackage.spec.component
BOM package.version == output ComponentPackage.spec.version
```

This requirement lets source-first local builds and non-local artifact
consumption share the same downstream loader and hydration workflow.

## Local And Non-local Coexistence

`BuildClass` is the shared abstraction that keeps the two build modes
maintainable:

- In source-first local build mode, the controller or local build tool executes
  the class against source files and records the produced artifact.
- In non-local build mode, the artifact is already available, but the class
  remains part of the `BOM` provenance so consumers can understand how the
  artifact was created.

This avoids maintaining separate package schemas for local and non-local
delivery.

Repository-level `scripts/` may wrap a build class for operator convenience,
but they should be generic dispatchers. They should not carry reusable class
implementations that every distribution repository must copy. A package-specific
script may be used only when it is referenced by the package-local build
contract and lives inside the package source directory.

## Descriptor Example

This descriptor shape documents the contract. For a built-in class such as
`rootfs/v1`, the executable behavior still comes from the Sealos class registry.

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs
spec:
  version: v1
  driver: copy-rootfs
  output: ociImage
  packageClasses:
    - rootfs
  platforms:
    - linux/amd64
    - linux/arm64
  source:
    include:
      - package.yaml
      - rootfs/**
      - files/**
      - hooks/**
    exclude:
      - "**/.git/**"
      - "**/*.tmp"
  provenance:
    required:
      - sourceDigest
      - buildClass
      - platform
```

## Initial Built-in Build Classes

The initial built-in class set should stay small. Add a new class only when the
source shape or output semantics differ enough that validation and provenance
need a separate contract.

| Class | Driver | Output | Package classes | Use case |
| --- | --- | --- | --- | --- |
| `rootfs/v1` | `copy-rootfs` | `ociImage` | `rootfs` | Packages that materialize files under `rootfs/`, optionally after staging declared build inputs. Kubernetes and containerd rootfs packages fit here. |
| `manifest-bundle/v1` | `copy-manifest` | `ociImage` | `application`, `policy` | Packages that copy checked-in manifests, values, and hooks into a package artifact without rendering a chart. The current Cilium package shape fits here. |
| `helm-render/v1` | `helm` | `ociImage` | `application` | Packages whose source includes a Helm chart or chart reference and values, and whose build output is a rendered manifest bundle. |
| `patch-overlay/v1` | `patch` | `ociImage` | `patch` | Packages that apply declared overlays or patches to an upstream package or manifest bundle and publish the resulting package payload. |

Avoid a generic `script/v1` class as an initial default. Package-local scripts
are allowed as adapters when declared in `ComponentPackage.spec.build`, but a
catch-all script class would make build behavior harder to validate and
reproduce. Custom script-like behavior should be introduced through an approved
extension class, not by placing arbitrary reusable scripts in each distribution
repository.

### `rootfs/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs
spec:
  version: v1
  driver: copy-rootfs
  output: ociImage
  packageClasses:
    - rootfs
  source:
    include:
      - package.yaml
      - rootfs/**
      - files/**
      - manifests/**
      - hooks/**
      - build/**
    exclude:
      - "**/.git/**"
      - "**/tmp/**"
  provenance:
    required:
      - sourceDigest
      - buildClass
      - platform
```

### `manifest-bundle/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: manifest-bundle
spec:
  version: v1
  driver: copy-manifest
  output: ociImage
  packageClasses:
    - application
    - policy
  source:
    include:
      - package.yaml
      - manifests/**
      - files/**
      - hooks/**
    exclude:
      - "**/.git/**"
      - "**/tmp/**"
  provenance:
    required:
      - sourceDigest
      - buildClass
```

### `helm-render/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: helm-render
spec:
  version: v1
  driver: helm
  output: ociImage
  packageClasses:
    - application
  parameters:
    - name: chart
      required: true
      secret: false
    - name: values
      required: false
      secret: false
  provenance:
    required:
      - sourceDigest
      - buildClass
      - chartDigest
```

### `patch-overlay/v1`

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: patch-overlay
spec:
  version: v1
  driver: patch
  output: ociImage
  packageClasses:
    - patch
  source:
    include:
      - package.yaml
      - patches/**
      - files/**
      - hooks/**
  provenance:
    required:
      - sourceDigest
      - buildClass
      - baseArtifactDigest
```

## Kubernetes Package Example

A Kubernetes `ComponentPackage` would usually select a rootfs-oriented build
class. The package-specific binary inputs and staging rules belong in
`ComponentPackage.spec.build`, while the BOM pins the class and artifact for a
release:

```yaml
packages:
  - category: core
    name: kubernetes
    version: v1.31.1
    source:
      path: packages/core/kubernetes/v1.31.1
      digest: sha256:...
    build:
      class: rootfs/v1
      profile: release
      platform: linux/amd64
    artifact:
      name: kubernetes-rootfs
      image: registry.example.com/dist/kubernetes:v1.31.1
      digest: sha256:...
```

## Boundaries

- `BuildClass` does not select package versions.
- `BuildClass` does not define cluster-specific inputs.
- `BuildClass` does not list package-specific external assets.
- `BuildClass` does not hide package-specific staging rules in repository-level scripts.
- `BuildClass` does not require every distribution repository to vendor
  standard class definitions.
- `BuildClass` does not approve local patches.
- `BuildClass` does not represent an applied runtime state.

## Related Kinds

- `ComponentPackage` supplies the source shape consumed by the build class.
- `BOM` selects the class for each package entry.
- `PackageAcceptanceReport` records whether built package output passed checks.
