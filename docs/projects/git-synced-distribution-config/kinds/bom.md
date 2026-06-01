# Kind: BOM

## Status

Implemented file schema, with planned source-first extensions.

## Class

Release source document.

## Owner

The distribution release owner maintains `BOM` documents.

## Normal Locations

- `boms/<distribution>/<revision>/bom.yaml`
- `releases/<distribution>/<revision>/bom.yaml`

## Purpose

`BOM` is the release bill of materials. It selects the exact package set for a
distribution revision and binds package identity, source provenance, build
metadata, artifact metadata, dependencies, and local patch policy.

It is the main document that lets source-first local build mode and non-local
build mode coexist:

- Source-first mode uses `source` and `build` to produce artifacts locally.
- Non-local mode uses `artifact` to consume already published outputs.

## Required Envelope

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: sealos-v5.0.0
spec: {}
```

## Spec Contract

| Field | Required | Description |
| --- | --- | --- |
| `revision` | Yes | Immutable distribution revision represented by this BOM. |
| `localPatchPolicy` | No | Relative path to the default local patch policy for this release. |
| `baseArtifacts` | No | Shared base artifacts used by package builds or runtime assembly. |
| `packages` | Yes | Ordered package entries selected by this revision. |

## Package Contract

| Field | Required | Description |
| --- | --- | --- |
| `category` | Recommended | Package category used to avoid name collisions and clarify package type. |
| `name` | Yes | Package name within the category. |
| `version` | Yes | Package version. |
| `source.path` | Required for source-first mode | Repository-relative source path. |
| `source.digest` | Required for reproducible source-first mode | Digest of the source facts used to build or render the package. |
| `build.class` | Required for source-first mode | Build class used to build this source. |
| `build.profile` | No | Build profile, such as `debug`, `release`, or `fips`. |
| `artifact.name` | Required by current implementation | Logical artifact name. |
| `artifact.image` | Required by current implementation | Published OCI image or artifact reference. |
| `artifact.digest` | Required by current implementation | Digest of the published artifact. |
| `artifact.platform` | No | Target platform for platform-specific artifacts. |
| `dependencies` | No | Other package names required by this package. |
| `required` | No | Whether the package is required for the release. |

The current implementation validates that every package has artifact metadata.
The source-first proposal keeps `artifact` as the non-local path and adds
stronger `source` plus `build` requirements for local builds. Implementations
that enforce source-first local builds must define when `artifact` may be
omitted or generated.

## Package Identity

Package identity should be resolved as:

```text
category + name + version + source digest
```

`name` alone is not enough. Different package categories may legally reuse the
same short name, and the same version can be rebuilt from different source
facts.

## Validation Rules

- `apiVersion`, `kind`, and `metadata.name` must be set.
- `spec.revision` must be set.
- At least one package entry is required.
- Package names must be unique where the implementation still resolves
  dependencies by name.
- Package identity must not collide after category, name, version, and source
  provenance are resolved.
- Source paths must be relative and must not escape the repository root.
- Digests, when set, must use a supported digest format.
- Dependencies must reference packages in the same BOM.

## Lifecycle

1. Package owners publish or update `ComponentPackage` documents.
2. Release owners select package versions into a `BOM`.
3. Source-first mode builds missing artifacts from `source` plus `build`.
4. Non-local mode verifies and consumes `artifact`.
5. `ReleaseChannel` promotes a BOM revision after health evidence is accepted.
6. Runtime reconciliation records the applied revision.

## Boundaries

- `BOM` does not select clusters.
- `BOM` does not contain cluster-specific input values.
- `BOM` does not contain secrets.
- `BOM` does not represent runtime success by itself.

## Example

```yaml
apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: sealos-v5.0.0
spec:
  revision: v5.0.0
  localPatchPolicy: ownership/local-patch-policy.yaml
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
      required: true
```

## Related Kinds

- `ComponentPackage` defines package source metadata.
- `BuildClass` defines source build behavior.
- `ReleaseChannel` points a channel to a target BOM revision.
- `HydratedBundle` records the rendered output of a BOM.
- `AppliedRevision` records the revision applied to a cluster.
