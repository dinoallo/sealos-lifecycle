# Kind: BuildClass

## Status

Proposed file schema. The current implementation records `build.class` in `BOM`
entries, but a standalone `BuildClass` document is not implemented yet.

## Class

Repository source document.

## Owner

The distribution platform owner maintains build classes. Package owners select a
build class but should not redefine its semantics inside a package.

## Normal Locations

- `build-classes/<name>.yaml`
- `build/classes/<name>.yaml`

## Purpose

`BuildClass` defines the reproducible build contract for package sources. It
lets source-first local build mode and non-local build mode share one package
model:

- Source-first mode uses the build class to build artifacts from repository
  facts.
- Non-local mode uses the same class name as provenance for artifacts that were
  already built elsewhere.

The class is the boundary between package metadata and build execution. A
package declares what it contains; a build class declares how that source shape
is built.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs-image
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `driver` | Yes | Logical build driver, such as `containerfile`, `script`, `helm`, or `copy-rootfs`. |
| `output` | Yes | Artifact kind produced by the build, such as `ociImage`, `filesystem`, `chart`, or `manifestBundle`. |
| `packageClasses` | Yes | `ComponentPackage.spec.class` values accepted by this build class. |
| `platforms` | No | Supported target platforms. Empty means platform-independent. |
| `source` | No | Include and exclude rules for source files. |
| `parameters` | No | Declared non-secret build options and defaults. |
| `provenance` | No | Required provenance fields that must be written into the resulting artifact metadata. |

## Validation Rules

- `metadata.name` must be globally unique within the distribution source
  repository.
- `driver` and `output` must be set.
- Every value in `packageClasses` must be a supported `ComponentPackage` class.
- `parameters` must be non-secret. Secrets must be provided by the runtime or
  CI environment and referenced only by name.
- Build classes should be immutable after adoption. Changing class behavior
  requires a new class name or versioned class name.

## Build Inputs

The package build workflow resolves these values before invoking a build class:

- package identity: `category`, `name`, `version`
- source path and digest
- `build.class`
- target platform
- build profile
- declared build options

The build class must not read undeclared host paths, cluster configuration,
runtime state, or secret contents.

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

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BuildClass
metadata:
  name: rootfs-image
spec:
  driver: containerfile
  output: ociImage
  packageClasses:
    - rootfs
  platforms:
    - linux/amd64
    - linux/arm64
  source:
    include:
      - rootfs/**
      - Containerfile
      - hooks/**
    exclude:
      - "**/.git/**"
      - "**/*.tmp"
  parameters:
    - name: baseImage
      required: true
      secret: false
    - name: compression
      default: zstd
      secret: false
  provenance:
    required:
      - sourceDigest
      - buildClass
      - buildProfile
      - platform
```

## Kubernetes Package Example

A Kubernetes `ComponentPackage` would usually select a rootfs-oriented build
class:

```yaml
packages:
  - category: core
    name: kubernetes
    version: v1.31.1
    source:
      path: packages/core/kubernetes/v1.31.1
      digest: sha256:...
    build:
      class: rootfs-image
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
- `BuildClass` does not approve local patches.
- `BuildClass` does not represent an applied runtime state.

## Related Kinds

- `ComponentPackage` supplies the source shape consumed by the build class.
- `BOM` selects the class for each package entry.
- `PackageAcceptanceReport` records whether built package output passed checks.
