# Kind: ComponentPackage

## Status

Implemented file schema, with proposed package-local build extensions for
source-first materialization.

## Class

Source package document.

## Owner

The component package owner maintains this document in the distribution source
repository.

## Normal Locations

- `packages/<category>/<name>/package.yaml`
- `packages/<category>/<name>/<version>/package.yaml`

## Purpose

`ComponentPackage` describes the buildable and installable unit for one
component version. It is the source-side contract that turns repository files
into package content, hooks, inputs, dependencies, and local patch ownership.

The document must be stable enough for source-first local builds, but it must
also support non-local builds where the same package has already been published
as an external artifact.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: kubernetes
spec: {}
```

`metadata.name` is the repository-local package name. Collision avoidance is
handled by the package identity in `BOM` entries, where `category`, `name`,
`version`, and source provenance are resolved together.

## Source And Materialized Forms

`ComponentPackage` appears in two forms:

- Source form: `package.yaml` stored under the distribution source repository.
- Materialized form: `package.yaml` stored at the root of a built package
  payload or artifact.

Both forms use the same kind and must validate with the same schema. The source
form declares the package source contract. The materialized form declares the
payload that downstream loaders, hydration, and apply workflows consume.

Build workflows must not emit a different final document kind. They must produce
a package root that contains a valid `ComponentPackage` manifest and the files
referenced by that manifest.

When selected by a `BOM`, the package must satisfy:

```text
BOM package.name == ComponentPackage.spec.component
BOM package.version == ComponentPackage.spec.version
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `component` | Yes | Logical component name. Usually matches `metadata.name`. |
| `version` | Yes | Component version represented by this package. |
| `class` | Yes | Package class. Current values are `rootfs`, `patch`, and `application`. |
| `dependencies` | No | Package names that must be available before this package is installed or rendered. |
| `compatibility` | No | Compatibility rules for supported Kubernetes, OS, architecture, or distribution lines. |
| `build` | No | Package-local build contract for source-first materialization. |
| `inputs` | No | Declared non-secret inputs accepted by the package. |
| `contents` | Yes | Package content entries, such as rootfs files, manifests, charts, values, patches, or hooks. |
| `hooks` | No | Lifecycle hooks run by the package workflow. |
| `localPatchPolicy` | No | Relative path to the local patch ownership policy for this package. |

## Build

`spec.build` describes package-specific build requirements that belong next to
the package source. It is optional for packages that can be materialized by the
selected `BuildClass` defaults.

Supported fields are:

| Field | Required | Description |
| --- | --- | --- |
| `class` | No | Expected default build class for this package source. |
| `inputs` | No | Non-secret build assets required before materialization. |
| `staging` | No | Mapping from build inputs to paths in the materialized package root. |
| `script.path` | No | Package-local adapter script, usually under `build/`. |

Build inputs are package build assets, not cluster-specific inputs. They may
reference local mirror entries, artifact cache entries, upstream asset
references, or small files already stored in the package source. Any external
asset that can change package bytes must be digest-pinned.

`staging.path` values must be relative to the materialized package root and must
not escape it. `script.path` must be relative to the package source directory.
Repository-level scripts may invoke this contract, but package-specific staging
rules should not live only in repository-level scripts.

## Contents

Supported content types are:

- `rootfs`
- `manifest`
- `chart`
- `patch`
- `file`
- `values`
- `hook`

Each content entry must have a stable name and a repository-relative path. The
path must not escape the package or repository root.

## Inputs

Supported input types are:

- `configFile`
- `valuesFile`
- `env`

Inputs describe values that the package accepts. They do not provide
cluster-specific values by themselves. Cluster-specific values belong in
`ComponentInput` or in the cluster configuration repository.

## Hooks

Supported hook phases are:

- `bootstrap`
- `configure`
- `install`
- `upgrade`
- `remove`
- `healthcheck`

Supported hook targets are:

- `allNodes`
- `firstMaster`
- `cluster`

Hooks must be deterministic from declared inputs and package files. They must
not read undeclared host files or secrets.

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- `spec.component` and `spec.version` must be set.
- `spec.class` must be one of the supported package classes.
- `spec.build.class`, when set, must be compatible with the BOM-selected build class.
- `spec.build.inputs` must be non-secret and digest-pinned when they resolve outside the package source.
- `spec.build.staging` entries must reference declared build inputs and use relative paths.
- `spec.build.script.path`, when set, must point inside the package source directory.
- At least one content entry is required.
- Content names must be unique within the package.
- Input names must be unique within the package.
- Hook names must be unique within the package.
- `localPatchPolicy`, when set, must be a relative path.

## Lifecycle

1. The package owner declares package files, inputs, and hooks.
2. A `BOM` references the package by source path, digest, build class, and
   artifact output when available.
3. Source-first local build mode builds the package from repository facts.
4. Non-local build mode consumes the artifact referenced by the `BOM`.
5. Hydration records the package in `HydratedBundle` provenance.

## Boundaries

- `ComponentPackage` does not select clusters.
- `ComponentPackage` does not carry secrets.
- `ComponentPackage` does not represent runtime state.
- `ComponentPackage` does not approve ownership changes.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: kubernetes
spec:
  component: kubernetes
  version: v1.31.1
  class: rootfs
  build:
    class: rootfs/v1
    inputs:
      - name: kubeadm
        type: file
        sourceRef: kubernetes-release:v1.31.1/bin/linux/amd64/kubeadm
        digest: sha256:...
        required: true
    staging:
      - input: kubeadm
        path: rootfs/usr/bin/kubeadm
        mode: "0755"
    script:
      path: build/package-build.sh
  inputs:
    - name: cluster-network
      type: valuesFile
      path: inputs/network.values.yaml
  contents:
    - name: kube-binaries
      type: rootfs
      path: rootfs/
    - name: kubeadm-config
      type: manifest
      path: manifests/kubeadm.yaml
  hooks:
    - name: kubeadm-init
      phase: install
      target: firstMaster
      path: hooks/kubeadm-init.sh
  localPatchPolicy: ownership/local-patch-policy.yaml
```

## Related Kinds

- `BuildClass` defines how the package source is built.
- `BOM` selects package versions and artifacts.
- `ComponentInput` supplies cluster-specific non-secret values.
- `LocalPatchPolicy` defines local ownership boundaries.
- `HydratedBundle` records rendered package output.
